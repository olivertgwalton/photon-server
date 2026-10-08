package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// sharedValkey is what several nodes share in Valkey: their playbacks, and what each last said of
// itself, which may be out of date.
type sharedValkey struct {
	*livePlaybacks
	mu      sync.Mutex
	adverts map[uuid.UUID]domain.Node
}

func newSharedValkey() *sharedValkey {
	return &sharedValkey{livePlaybacks: &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}, adverts: map[uuid.UUID]domain.Node{}}
}

func (c *sharedValkey) tell(n domain.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adverts[n.ID] = n
}

func (c *sharedValkey) Nodes(context.Context) ([]domain.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Collect(maps.Values(c.adverts)), nil
}

func (c *sharedValkey) NodeAddress(_ context.Context, id uuid.UUID) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.adverts[id]
	return n.Address, ok, nil
}

// peerNode is a server node of a sharedValkey, served over HTTP, encoding in software.
type peerNode struct {
	id      uuid.UUID
	role    atomic.Value
	remuxer *hls.Remuxer
	placer  *playback.Placer
	srv     *httptest.Server
	// started counts the playback.started events this node raised.
	started atomic.Int32
}

func (n *peerNode) self() domain.Node {
	d := domain.Node{
		ID: n.id, Address: n.srv.URL, Name: n.srv.URL, Role: n.role.Load().(domain.NodeRole), Availability: domain.NodeActive,
		Encoder: domain.Encoder{Acceleration: domain.AccelSoftware, HEVC: domain.HEVCDeny},
	}
	d.Transcodes, d.Conversions, d.Limit = n.remuxer.Transcodes()
	return d
}

// join starts a node of c that encodes at most limit videos at once, telling the others of itself.
func join(t *testing.T, c *sharedValkey, limit int) *peerNode {
	t.Helper()
	remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: "ffmpeg"}}, t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, limit, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	// Every node derives the same key, so each takes the others' calls.
	clusterKey, err := nodecall.NewKey([]byte("sharedValkey signing key"))
	if err != nil {
		t.Fatal(err)
	}
	n := &peerNode{id: uuid.NewV7(), remuxer: remuxer}
	n.role.Store(domain.NodeAll)
	n.placer = playback.NewPlacer(c, n.self, remuxOpener{remuxer}, clusterKey)
	n.srv = httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Sent:    playback.NewSent(),
		Network: fakeNetwork{}, Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{},
		Playbacks: playback.NewSessions(c.livePlaybacks, c.livePlaybacks, remuxer, func(_ context.Context, e domain.Event) {
			if e.Kind == domain.EventPlaybackStarted {
				n.started.Add(1)
			}
		}, n.id),
		Placer: n.placer, NodeKey: clusterKey,
		HLS: remuxer, Owners: playback.NewRouter(c, n.id), Signer: playback.NewSigner([]byte("key")),
	}))
	t.Cleanup(n.srv.Close)
	c.tell(n.self())
	return n
}

// encode fills one of n's transcode slots, as a playback encoding there does.
func (n *peerNode) encode(t *testing.T) {
	t.Helper()
	video := domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 2000}}
	err := remuxOpener{n.remuxer}.Open(t.Context(), uuid.NewV7(), store.PlayCopy{Parts: []store.PlayPart{{DurationMS: 60_000}}}, video, nil, domain.SegmentsFMP4, 0)
	if err != nil {
		t.Fatal(err)
	}
}

// transcodeOn asks n to play the film so its video is encoded, answering its playback and playlist.
func transcodeOn(t *testing.T, n *peerNode) (uuid.UUID, string) {
	t.Helper()
	body := `{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, n.srv.URL+"/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+goodToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer struct {
		PlaybackID uuid.UUID `json:"playback_id"`
		Method     string    `json:"method"`
		Playlist   string    `json:"playlist"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil || resp.StatusCode != http.StatusOK || answer.Method != "transcode" {
		t.Fatalf("a transcode: %s, %+v, %v", resp.Status, answer, err)
	}
	return answer.PlaybackID, answer.Playlist
}

// A transcode asked of a node with no slot free is made by one with a slot, and the player is
// served its HLS by the node it asked, which hands each request on.
func TestATranscodeIsMadeWhereASlotIsFree(t *testing.T) {
	c := newSharedValkey()
	front, gpu := join(t, c, 1), join(t, c, 2)
	front.encode(t)
	c.tell(front.self())
	id, playlist := transcodeOn(t, front)
	p, _, err := c.Playback(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Node != gpu.id {
		t.Fatalf("the playback is kept as on %s, want the node with a slot, %s", p.Node, gpu.id)
	}
	if !gpu.remuxer.Has(id) || front.remuxer.Has(id) {
		t.Errorf("remuxed on the GPU node %v, on the front %v; want it on the GPU node alone", gpu.remuxer.Has(id), front.remuxer.Has(id))
	}
	resp, err := http.Get(front.srv.URL + playlist)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Errorf("its playlist through the front: %s %s, want it from the GPU node", resp.Status, resp.Header.Get("Content-Type"))
	}
}

// A node that said it had slots free but has none by the time it is asked refuses, and the next
// is asked: the play is made, and the refused attempt leaves nothing behind.
func TestANodeFullByTheTimeItIsAskedPassesThePlayOn(t *testing.T) {
	c := newSharedValkey()
	front, gpu := join(t, c, 2), join(t, c, 1)
	front.encode(t)
	gpu.encode(t)
	// The GPU node last said it had its one slot free.
	stale := gpu.self()
	stale.Transcodes = 0
	c.tell(stale)
	id, _ := transcodeOn(t, front)
	p, _, err := c.Playback(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Node != front.id || !front.remuxer.Has(id) {
		t.Errorf("the playback is kept as on %s, remuxed on the front %v; want the front, the node with a slot", p.Node, front.remuxer.Has(id))
	}
	plays, err := c.Playbacks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 1 {
		t.Errorf("%d playbacks kept, want only the one made, not the one refused", len(plays))
	}
	if got := front.started.Load(); got != 1 {
		t.Errorf("%d playbacks said to start, want only the one made, not the one refused", got)
	}
}

// A play refused because every node that could encode it is full is not said to start.
func TestAPlayRefusedForEveryNodeFullNeverStarts(t *testing.T) {
	c := newSharedValkey()
	front, gpu := join(t, c, 1), join(t, c, 1)
	front.encode(t)
	gpu.encode(t)
	// Both last said they had their one slot free.
	for _, n := range []*peerNode{front, gpu} {
		stale := n.self()
		stale.Transcodes = 0
		c.tell(stale)
	}
	body := `{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, front.srv.URL+"/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+goodToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a transcode with every node full: %s, want 503", resp.Status)
	}
	if got := front.started.Load() + gpu.started.Load(); got != 0 {
		t.Errorf("%d playbacks said to start, want none", got)
	}
}

// A node set to serve only takes no transcode: not when placed, and not when another, told of it
// before the change, asks it anyway.
func TestANodeThatServesOnlyTranscodesNothing(t *testing.T) {
	c := newSharedValkey()
	front, gpu := join(t, c, 1), join(t, c, 2)
	front.encode(t)
	gpu.role.Store(domain.NodeServe)
	// The others were last told it transcodes, and that it has room.
	stale := gpu.self()
	stale.Role = domain.NodeAll
	c.tell(stale)
	body := `{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, front.srv.URL+"/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+goodToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a transcode with the only node with room serving only: %s, want 503", resp.Status)
	}
	if active, _, _ := gpu.remuxer.Transcodes(); active != 0 {
		t.Errorf("the node serving only transcodes %d, want none", active)
	}
}

// With no node taking video to encode, a copy that needs encoding is refused, and one played as it
// is still plays, from the node asked.
func TestWithNoNodeTranscodingACopyThatNeedsItIsRefused(t *testing.T) {
	c := newSharedValkey()
	only := join(t, c, 2)
	only.role.Store(domain.NodeServe)
	play := func(body string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, only.srv.URL+"/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+goodToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var answer struct {
			PlaybackID uuid.UUID `json:"playback_id"`
			Method     string    `json:"method"`
			Code       string    `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusOK && answer.PlaybackID == (uuid.UUID{}) {
			t.Errorf("answered a play with no playback")
		}
		return resp.StatusCode, answer.Method + answer.Code
	}
	if status, said := play(`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`); status != http.StatusServiceUnavailable || said != "transcode_limit" {
		t.Errorf("a copy to encode: %d %s, want 503 transcode_limit", status, said)
	}
	refusals := `# HELP photon_transcode_refusals_total The playbacks this node refused for want of a node to encode them, by why.
# TYPE photon_transcode_refusals_total counter
photon_transcode_refusals_total{reason="full"} 0
photon_transcode_refusals_total{reason="no_encoder"} 1
`
	if err := testutil.CollectAndCompare(only.placer, strings.NewReader(refusals)); err != nil {
		t.Error(err)
	}
	if status, said := play(`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "parts": "each"}}`); status != http.StatusOK || said == "transcode" {
		t.Errorf("a copy played as it is: %d %s, want it played", status, said)
	}
}
