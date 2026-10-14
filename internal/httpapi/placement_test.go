package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
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

func (c *sharedValkey) Node(_ context.Context, id uuid.UUID) (domain.Node, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.adverts[id]
	return n, ok, nil
}

// peerNode is a server node of a sharedValkey, served over HTTP, encoding in software.
type peerNode struct {
	id       uuid.UUID
	role     atomic.Value
	remuxer  *hls.Remuxer
	placer   *playback.Placer
	sessions *playback.Sessions
	srv      *httptest.Server
	// public and local are where clients reach it themselves, and streams how many playbacks it
	// delivers at once, set before it is asked anything.
	public, local string
	streams       int
	// started counts the playback.started events this node raised.
	started atomic.Int32
}

func (n *peerNode) self() domain.Node {
	d := domain.Node{
		ID: n.id, Address: n.srv.URL, Name: n.srv.URL, PublicURL: n.public, LocalURL: n.local, StreamLimit: n.streams, Role: n.role.Load().(domain.NodeRole), Availability: domain.NodeActive,
		Encoder: domain.Encoder{Acceleration: domain.AccelSoftware, HEVC: domain.HEVCDeny},
	}
	d.Transcodes, d.Conversions, d.Limit = n.remuxer.Transcodes()
	if n.sessions != nil {
		d.Streams = n.sessions.Delivering()
	}
	return d
}

// join starts a node of c that encodes at most limit videos at once, telling the others of itself.
func join(t *testing.T, c *sharedValkey, limit int) *peerNode {
	t.Helper()
	remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: "ffmpeg"}}, t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, limit, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	closing(t, remuxer)
	// Every node derives the same key, so each takes the others' calls.
	clusterKey, err := nodecall.NewKey([]byte("sharedValkey signing key"))
	if err != nil {
		t.Fatal(err)
	}
	n := &peerNode{id: uuid.NewV7(), remuxer: remuxer}
	n.role.Store(domain.NodeAll)
	n.sessions = playback.NewSessions(c.livePlaybacks, c.livePlaybacks, remuxer, func(_ context.Context, e domain.Event) {
		if e.Kind == domain.EventPlaybackStarted {
			n.started.Add(1)
		}
	}, n.id)
	n.placer = playback.NewPlacer(c, n.self, remuxOpener{remuxer}, n.sessions, clusterKey)
	// Every node mounts the media, here a film's first file, at the same path.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Lawrence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Lawrence", "Lawrence cd1.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	n.srv = httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Sent:    playback.NewSent(),
		Network: fakeNetwork{}, Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{root: root},
		Parts:     library.Parts{Places: fakePlaying{root: root}},
		Playbacks: n.sessions,
		Placer:    n.placer, NodeKey: clusterKey,
		HLS: remuxer, Owners: playback.NewRouter(c, n.id), Signer: playback.NewSigner([]byte("key")),
	}))
	t.Cleanup(n.srv.Close)
	c.tell(n.self())
	return n
}

// closing ends each of r's remuxes as the test does, before its folders are removed: Windows
// removes no folder a remux still holds open.
func closing(t *testing.T, r *hls.Remuxer) {
	t.Helper()
	t.Cleanup(func() {
		for _, p := range r.Playbacks() {
			r.Close(p)
		}
	})
}

// encode fills one of n's transcode slots, as a playback encoding there does.
func (n *peerNode) encode(t *testing.T) {
	t.Helper()
	video := domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 2000}}
	err := remuxOpener{n.remuxer}.Open(t.Context(), uuid.NewV7(), store.PlayCopy{Parts: []store.PlayPart{{DurationMS: 60_000}}}, video, nil, domain.SegmentsFMP4, 0, playback.RenditionsAll)
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

// A transcode made by a node with a public URL is fetched from there, not handed on through the
// node the client asked, by a web app served from any origin; one the node asked makes itself is
// fetched from where the client reached it.
func TestATranscodeElsewhereIsFetchedFromThatNodesPublicURL(t *testing.T) {
	c := newSharedValkey()
	front, gpu := join(t, c, 2), join(t, c, 1)
	front.public, gpu.public = "https://front.example.com", gpu.srv.URL
	front.encode(t)
	c.tell(front.self())
	c.tell(gpu.self())
	_, playlist := transcodeOn(t, front)
	if !strings.HasPrefix(playlist, gpu.srv.URL+"/api/v1/hls/") {
		t.Fatalf("made on the GPU node, its playlist is at %s, want it at the GPU node's public URL", playlist)
	}
	c.tell(gpu.self())
	if _, own := transcodeOn(t, front); !strings.HasPrefix(own, "/api/v1/hls/") {
		t.Errorf("made on the front itself, its playlist is at %s, want an address relative to it", own)
	}
	resp, err := http.Get(playlist)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("its playlist from the GPU node: %s, Access-Control-Allow-Origin %q; want it, to any page", resp.Status, resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

// limitStreams sets how many playbacks n delivers at once, and tells the others.
func (n *peerNode) limitStreams(c *sharedValkey, limit int) {
	n.streams = limit
	n.sessions.SetStreamLimit(limit)
	c.tell(n.self())
}

// played is a film played as it is, as play answers it, or why it was refused.
type played struct {
	status     int
	Code       string    `json:"code"`
	PlaybackID uuid.UUID `json:"playback_id"`
	Parts      []struct {
		URL string `json:"url"`
	} `json:"parts"`
}

// playAsItIsOn asks n to play the film as it is.
func playAsItIsOn(t *testing.T, n *peerNode) played {
	t.Helper()
	req := playRequest(`"matroska"`)
	req.RequestURI, req.URL.Scheme, req.URL.Host = "", "http", strings.TrimPrefix(n.srv.URL, "http://")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	p := played{status: resp.StatusCode}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A film played as it is is delivered by a node set to serve only, its files fetched from that
// node's public URL; its stop, asked of the node the client asked, ends it there.
func TestAFilmPlayedAsItIsIsDeliveredByANodeServingOnly(t *testing.T) {
	c := newSharedValkey()
	front, proxy := join(t, c, 1), join(t, c, 1)
	proxy.role.Store(domain.NodeServe)
	proxy.public = proxy.srv.URL
	c.tell(proxy.self())
	played := playAsItIsOn(t, front)
	if played.status != http.StatusOK || len(played.Parts) == 0 {
		t.Fatalf("play: %+v", played)
	}
	if !strings.HasPrefix(played.Parts[0].URL, proxy.srv.URL+"/") {
		t.Fatalf("its first file is at %s, want it at the proxy's public URL", played.Parts[0].URL)
	}
	if got := proxy.sessions.Delivering(); got != 1 {
		t.Errorf("the proxy delivers %d streams, want the one", got)
	}
	file, err := http.Get(played.Parts[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Body.Close()
	body, err := io.ReadAll(file.Body)
	if err != nil || file.StatusCode != http.StatusOK || string(body) != "0123456789" {
		t.Errorf("its first file from the proxy: %s %q, %v", file.Status, body, err)
	}
	stop, err := http.NewRequestWithContext(t.Context(), http.MethodPost, front.srv.URL+"/api/v1/playbacks/"+played.PlaybackID.String()+"/stop", strings.NewReader(`{"position_ms": 0}`))
	if err != nil {
		t.Fatal(err)
	}
	stop.Header.Set("Authorization", "Bearer "+goodToken)
	stopped, err := http.DefaultClient.Do(stop)
	if err != nil {
		t.Fatal(err)
	}
	stopped.Body.Close()
	if stopped.StatusCode != http.StatusOK || proxy.sessions.Delivering() != 0 {
		t.Errorf("stopped through the front: %s, the proxy delivering %d; want it ended there", stopped.Status, proxy.sessions.Delivering())
	}
}

// A client on the server's local networks, as this machine is, fetches a stream from where they
// reach the node delivering it.
func TestALocalClientFetchesAStreamAtTheNodesLocalURL(t *testing.T) {
	c := newSharedValkey()
	front, proxy := join(t, c, 1), join(t, c, 1)
	proxy.role.Store(domain.NodeServe)
	proxy.public, proxy.local = "https://proxy.example.com", proxy.srv.URL
	c.tell(proxy.self())
	if p := playAsItIsOn(t, front); p.status != http.StatusOK || !strings.HasPrefix(p.Parts[0].URL, proxy.srv.URL+"/") {
		t.Errorf("%+v, want it at the proxy's local URL", p)
	}
}

// A node delivering as many streams at once as an admin lets it is passed over, and refuses one
// asked of it all the same, for the next to deliver; with every node full, the play is refused.
func TestANodeAtItsStreamLimitIsPassedOver(t *testing.T) {
	c := newSharedValkey()
	front, proxy := join(t, c, 1), join(t, c, 1)
	proxy.role.Store(domain.NodeServe)
	proxy.public = proxy.srv.URL
	front.limitStreams(c, 1)
	proxy.limitStreams(c, 1)
	if p := playAsItIsOn(t, front); p.status != http.StatusOK || !strings.HasPrefix(p.Parts[0].URL, proxy.srv.URL+"/") {
		t.Fatalf("the first: %+v, want it delivered by the proxy", p)
	}
	// The proxy, full now, last said it had room.
	stale := proxy.self()
	stale.Streams = 0
	c.tell(stale)
	if p := playAsItIsOn(t, front); p.status != http.StatusOK || !strings.HasPrefix(p.Parts[0].URL, "/api/v1/") {
		t.Fatalf("the second: %+v, want it delivered by the front, the proxy refusing it", p)
	}
	c.tell(front.self())
	c.tell(proxy.self())
	if p := playAsItIsOn(t, front); p.status != http.StatusServiceUnavailable || p.Code != "stream_limit" {
		t.Errorf("the third: %+v, want 503 stream_limit, every node full", p)
	}
	plays, err := c.Playbacks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 2 {
		t.Errorf("%d playbacks kept, want only the two delivered", len(plays))
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
photon_transcode_refusals_total{reason="no_space"} 0
`
	if err := testutil.CollectAndCompare(only.placer, strings.NewReader(refusals)); err != nil {
		t.Error(err)
	}
	if status, said := play(`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "parts": "each"}}`); status != http.StatusOK || said == "transcode" {
		t.Errorf("a copy played as it is: %d %s, want it played", status, said)
	}
}
