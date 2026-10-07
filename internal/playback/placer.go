package playback

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// openWithin is how long a node has to open a remux another node asks of it.
const openWithin = 10 * time.Second

type adverts interface {
	Nodes(ctx context.Context) ([]domain.Node, error)
}

type opener interface {
	Open(ctx context.Context, playback uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan, segments domain.SegmentFormat, start time.Duration) error
}

// Placer chooses the node that encodes a playback's video, and opens its remux there. It keeps no
// count of the cluster's transcodes: the node asked admits or refuses under its own lock, and
// the next is asked, so the nodes' own counts are the only ones.
type Placer struct {
	adverts adverts
	self    func() domain.Node
	local   opener
	key     nodecall.Key
	client  *http.Client
}

// NewPlacer places on the nodes adverts tells of and on this one, as self says it is now.
func NewPlacer(a adverts, self func() domain.Node, local opener, key nodecall.Key) *Placer {
	return &Placer{adverts: a, self: self, local: local, key: key, client: &http.Client{Timeout: openWithin}}
}

// Need is what encoding a video asks of a node's encoder.
type Need struct{ HEVC, Libass bool }

// NeedOf is what encoding video asks; nothing where it is copied.
func NeedOf(video domain.VideoPlan) Need {
	e := video.Encode
	if e == nil {
		return Need{}
	}
	return Need{HEVC: e.Codec == "hevc", Libass: e.Burn != nil || e.BurnFile != nil}
}

// EncodingOf is what a node encodes, for deciding a playback it would encode.
func EncodingOf(n domain.Node) Encoding {
	return Encoding{HEVC: n.Encoder.HEVC, Libass: n.Encoder.Libass}
}

// Self is this node as it is now.
func (p *Placer) Self() domain.Node { return p.self() }

// Candidates are the nodes that could encode what need asks, this one among them: those with a
// slot free first, of them those set to transcode, then those with no limit, then those with
// more of their slots free; ties in no set order. A full one is still listed, as it may have a
// slot by the time it is asked; a node that never encodes is not.
func (p *Placer) Candidates(ctx context.Context, need Need) ([]domain.Node, error) {
	self := p.self()
	nodes, err := p.adverts.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	nodes = slices.DeleteFunc(nodes, func(n domain.Node) bool { return n.ID == self.ID || n.Address == "" })
	nodes = append(nodes, self)
	nodes = slices.DeleteFunc(nodes, func(n domain.Node) bool {
		return !n.Role.Encodes() || !n.Availability.Takes() || need.HEVC && n.Encoder.HEVC != domain.HEVCAllow || need.Libass && !n.Encoder.Libass
	})
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] }) //nolint:gosec // breaks ties between nodes equally free; nothing secret
	slices.SortStableFunc(nodes, func(a, b domain.Node) int {
		return cmp.Or(
			cmp.Compare(rank(b, roomy), rank(a, roomy)),
			cmp.Compare(rank(b, transcoding), rank(a, transcoding)),
			cmp.Compare(rank(b, unlimited), rank(a, unlimited)),
			cmp.Compare(free(b), free(a)),
		)
	})
	return nodes, nil
}

// rank is one for a node that is, and zero for one that is not.
func rank(n domain.Node, is func(domain.Node) bool) int {
	if is(n) {
		return 1
	}
	return 0
}

// roomy is a node with a transcode slot free, as it last said.
func roomy(n domain.Node) bool { return unlimited(n) || n.Transcodes < n.Limit }

// transcoding is a node set to transcode before any other, where it has a slot.
func transcoding(n domain.Node) bool {
	switch n.Role {
	case domain.NodeTranscode:
		return true
	case domain.NodeAll, domain.NodeServe:
	}
	return false
}

func unlimited(n domain.Node) bool { return n.Limit == hls.Unlimited }

// free is the share of a node's transcode slots not held, one for a node with no limit.
func free(n domain.Node) float64 {
	if n.Limit == hls.Unlimited {
		return 1
	}
	return float64(n.Limit-n.Transcodes) / float64(n.Limit)
}

// Opening is a playback's remux as one node asks another to open it: the profile's copy, by ids
// the node it is asked of reads again, and how it is played.
type Opening struct {
	Profile  uuid.UUID            `json:"profile"`
	Item     uuid.UUID            `json:"item"`
	Version  uuid.UUID            `json:"version"`
	Video    domain.VideoPlan     `json:"video"`
	Audio    *domain.AudioPlan    `json:"audio,omitempty"`
	Segments domain.SegmentFormat `json:"segments"`
	StartMS  int64                `json:"start_ms"`
}

// Open opens a playback's remux of c, as o says, on node: on this one, or by asking it. A node
// that has every slot held answers hls.ErrTranscodeLimit.
func (p *Placer) Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayCopy, o Opening) error {
	if node.ID == p.self().ID {
		return p.local.Open(ctx, playback, c, o.Video, o.Audio, o.Segments, time.Duration(o.StartMS)*time.Millisecond)
	}
	body, err := json.Marshal(o)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, node.Address+RemotePath(playback), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	p.key.Sign(req, body)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("node %s: %w", node.Name, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusServiceUnavailable:
		return hls.ErrTranscodeLimit
	}
	return fmt.Errorf("node %s answered %s", node.Name, resp.Status)
}

// RemotePath is where a node is asked to open a playback's remux.
func RemotePath(playback uuid.UUID) string {
	return "/api/v1/internal/playbacks/" + playback.String() + "/remux"
}

// Full is the refusal of a playback every one of candidates refused.
func Full(candidates []domain.Node) error {
	total := 0
	for _, n := range candidates {
		total += n.Limit
	}
	return fmt.Errorf("every node is already transcoding as many videos at once as it may: %d", total)
}
