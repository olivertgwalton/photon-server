package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type nodeSettings interface {
	KnownNodes(ctx context.Context) ([]domain.NodeRecord, error)
	Node(ctx context.Context, id uuid.UUID) (domain.NodeRecord, error)
	SetNodeSettings(ctx context.Context, id uuid.UUID, s domain.NodeSettings) error
}

// nodeJSON is a node as it tells the others of itself: what it encodes with, and transcodes, of
// which conversions are downloads', of at most transcode_limit at once, absent when unlimited.
type nodeJSON struct {
	ID          uuid.UUID          `json:"id"`
	Name        string             `json:"name,omitzero"`
	Address     string             `json:"address"`
	LastSeen    time.Time          `json:"last_seen"`
	Encoder     nodeEncoderJSON    `json:"encoder"`
	Transcodes  int                `json:"transcodes"`
	Conversions int                `json:"conversions"`
	Limit       int                `json:"transcode_limit,omitzero"`
	LimitSource domain.LimitSource `json:"transcode_limit_source"`
}

// nodeEncoderJSON is what a node encodes video with, and whether it draws styled subtitles in.
type nodeEncoderJSON struct {
	Acceleration domain.Acceleration `json:"acceleration"`
	HEVC         domain.HEVCEncoding `json:"hevc"`
	Libass       bool                `json:"libass"`
}

func showNode(n domain.Node) nodeJSON {
	return nodeJSON{
		ID: n.ID, Name: n.Name, Address: n.Address, LastSeen: n.Seen.UTC(),
		Encoder:    nodeEncoderJSON{n.Encoder.Acceleration, n.Encoder.HEVC, n.Encoder.Libass},
		Transcodes: n.Transcodes, Conversions: n.Conversions, Limit: n.Limit, LimitSource: n.LimitSource,
	}
}

// knownNodeJSON is a node there is or has been, as an admin sets it: role all serves clients and
// encodes video, serve never encodes, transcode encodes before any of all; transcode_limit, with
// transcode_limit_source set, is how many videos it encodes at once, 0 for no limit, and is worked
// out from its encoder where automatic. online is it as it tells the others of itself now, absent
// while it says nothing.
type knownNodeJSON struct {
	ID          uuid.UUID          `json:"id"`
	Name        string             `json:"name"`
	FirstSeen   time.Time          `json:"first_seen"`
	Role        domain.NodeRole    `json:"role"`
	LimitSource domain.LimitSource `json:"transcode_limit_source"`
	Limit       int                `json:"transcode_limit"`
	Online      *nodeJSON          `json:"online,omitempty"`
}

// nodeChangeJSON is what to change of a node: its role, its limit on transcodes at once, or both;
// transcode_limit is given with transcode_limit_source set, 0 for no limit.
type nodeChangeJSON struct {
	Role        domain.NodeRole    `json:"role,omitzero"`
	LimitSource domain.LimitSource `json:"transcode_limit_source,omitzero"`
	Limit       *int               `json:"transcode_limit,omitzero"`
}

func showKnownNode(n domain.NodeRecord, online map[uuid.UUID]domain.Node) knownNodeJSON {
	out := knownNodeJSON{
		ID: n.ID, Name: n.Name, FirstSeen: n.FirstSeen.UTC(), Role: n.Role, LimitSource: n.LimitSource, Limit: n.Limit,
	}
	if advert, ok := online[n.ID]; ok {
		shown := showNode(advert)
		out.Online = &shown
	}
	return out
}

// online are the nodes telling the others of themselves now, this one among them whether or not
// it tells the others: it is answering.
func (a *API) online(ctx context.Context) (map[uuid.UUID]domain.Node, error) {
	out := map[uuid.UUID]domain.Node{}
	if a.svc.Valkey != nil {
		adverts, err := a.svc.Valkey.Nodes(ctx)
		if err != nil {
			return nil, err
		}
		for _, n := range adverts {
			out[n.ID] = n
		}
	}
	self := a.svc.Placer.Self()
	out[self.ID] = self
	return out, nil
}

// adminNodes lists every node there is or has been, the first to start first, and how busy each
// is that is up.
func (a *API) adminNodes(w http.ResponseWriter, r *http.Request) {
	known, err := a.svc.Nodes.KnownNodes(r.Context())
	if a.answered(w, r, err) {
		return
	}
	online, err := a.online(r.Context())
	if a.answered(w, r, err) {
		return
	}
	out := make([]knownNodeJSON, len(known))
	for i, n := range known {
		out[i] = showKnownNode(n, online)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[knownNodeJSON]{Items: out})
}

// setNode changes what an admin sets of a node, and tells every node, which takes it up at once.
func (a *API) setNode(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req nodeChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	n, err := a.svc.Nodes.Node(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	set := n.NodeSettings
	if req.Role != "" {
		set.Role = req.Role
	}
	switch req.LimitSource {
	case domain.LimitSet:
		if req.Limit == nil || *req.Limit < 0 {
			writeProblem(w, a.logger, codeInvalidBody, "a limit set is how many videos at once, 0 for no limit")
			return
		}
		set.LimitSource, set.Limit = domain.LimitSet, *req.Limit
	case domain.LimitAutomatic:
		set.LimitSource, set.Limit = domain.LimitAutomatic, 0
	}
	if err := a.svc.Nodes.SetNodeSettings(r.Context(), id, set); a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNodesChanged})
	online, err := a.online(r.Context())
	if a.answered(w, r, err) {
		return
	}
	n.NodeSettings = set
	writeJSON(w, a.logger, "application/json", http.StatusOK, showKnownNode(n, online))
}
