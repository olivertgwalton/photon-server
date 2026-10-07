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
	srv     *httptest.Server
}

func (n *peerNode) self() domain.Node {
	d := domain.Node{
		ID: n.id, Address: n.srv.URL, Name: n.srv.URL, Role: n.role.Load().(domain.NodeRole),
		Encoder: domain.Encoder{Acceleration: domain.AccelSoftware, HEVC: domain.HEVCDeny},
	}
	d.Transcodes, d.Conversions, d.Limit = n.remuxer.Transcodes()
	return d
}

var clusterKey, _ = nodecall.NewKey([]byte("sharedValkey signing key"))

// join starts a node of c that encodes at most limit videos at once, telling the others of itself.
func join(t *testing.T, c *sharedValkey, limit int) *peerNode {
	t.Helper()
	remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: "ffmpeg"}}, t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, limit, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	n := &peerNode{id: uuid.NewV7(), remuxer: remuxer}
	n.role.Store(domain.NodeAll)
	n.srv = httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Network: fakeNetwork{}, Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{},
		Playbacks: playback.NewSessions(c.livePlaybacks, c.livePlaybacks, remuxer, func(context.Context, domain.Event) {}, n.id),
		Placer:    playback.NewPlacer(c, n.self, remuxOpener{remuxer}, clusterKey), NodeKey: clusterKey,
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
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, n.srv.URL+"/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
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
	if p, _, _ := c.Playback(t.Context(), id); p.Node != gpu.id {
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
	if p, _, _ := c.Playback(t.Context(), id); p.Node != front.id || !front.remuxer.Has(id) {
		t.Errorf("the playback is kept as on %s, remuxed on the front %v; want the front, the node with a slot", p.Node, front.remuxer.Has(id))
	}
	if plays, _ := c.Playbacks(t.Context()); len(plays) != 1 {
		t.Errorf("%d playbacks kept, want only the one made, not the one refused", len(plays))
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
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, front.srv.URL+"/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
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
