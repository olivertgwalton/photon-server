//go:build integration

package jellyfin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// fakePlaybacks is the dashboard's playbacks: those started, by id, and what each was told.
type fakePlaybacks struct {
	started map[uuid.UUID]domain.PlaybackCard
	told    []time.Duration
	stopped map[uuid.UUID]time.Duration
}

func newFakePlaybacks() *fakePlaybacks {
	return &fakePlaybacks{started: map[uuid.UUID]domain.PlaybackCard{}, stopped: map[uuid.UUID]time.Duration{}}
}

func (f *fakePlaybacks) Start(_ context.Context, id uuid.UUID, _ domain.PlayMethod, card domain.PlaybackCard, _ uuid.UUID) (domain.Playback, error) {
	f.started[id] = card
	return domain.Playback{ID: id}, nil
}

func (f *fakePlaybacks) Progress(_ context.Context, _, id uuid.UUID, position time.Duration, _ domain.PlayState, _ domain.ChosenTracks) (domain.Reach, error) {
	if _, ok := f.started[id]; !ok {
		return "", playback.ErrNoPlayback
	}
	f.told = append(f.told, position)
	return domain.ReachResumable, nil
}

func (f *fakePlaybacks) Stop(_ context.Context, _, id uuid.UUID, position time.Duration) (domain.Reach, error) {
	if _, ok := f.started[id]; !ok {
		return "", playback.ErrNoPlayback
	}
	f.stopped[id] = position
	return domain.ReachResumable, nil
}

func (f *fakePlaybacks) Finish(_ context.Context, _, id uuid.UUID) (domain.Reach, error) {
	if _, ok := f.started[id]; !ok {
		return "", playback.ErrNoPlayback
	}
	f.stopped[id] = -1
	return domain.ReachResumable, nil
}

func (f *fakePlaybacks) Opened(context.Context, domain.Playback) {}

func (f *fakePlaybacks) Abandon(_ context.Context, id uuid.UUID) error {
	delete(f.started, id)
	return nil
}

// aFilm is a household's film, Heat: one copy, an MKV of H.264 and AAC with an intro marked and a
// subtitle file beside it; and Ada, who may play it.
func aFilm(t *testing.T) (*store.Store, domain.Profile, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Heat"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"Heat/Heat.mkv": "0123456789", "Heat/Heat.en.srt": "1\n00:00:01,000 --> 00:00:02,000\nHello\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	// Its size is an hour at 8 Mbps, as the scan read it; its bytes here are fewer.
	part := store.Part{RelPath: "Heat/Heat.mkv", Size: 3_600_000_000, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Duration: time.Hour, Container: "matroska,webm",
		Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR},
			{Index: 1, Kind: domain.StreamAudio, Codec: "aac", Channels: 2, Language: language.English},
		},
	}}
	film := store.Film{Title: "Heat", Folder: "Heat", Copies: []store.Copy{{ContentKey: []byte("heat"), Parts: []store.Part{part}, Subtitles: []store.Subtitle{
		{RelPath: "Heat/Heat.en.srt", Size: 1, ModTime: time.Unix(0, 0), Codec: "subrip", Language: language.English},
	}}}}
	if _, err := st.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []store.Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{films.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	heat := cards[0].ID
	versions, err := st.Versions(ctx, []uuid.UUID{heat})
	if err != nil {
		t.Fatal(err)
	}
	copyID := versions[heat][0].ID
	if err := st.SetMarkers(ctx, copyID, []domain.Marker{{Kind: domain.MarkerIntro, StartMS: 1000, EndMS: 61000}}, nil); err != nil {
		t.Fatal(err)
	}

	return st, ada, heat, copyID
}

// An app plays a film as Infuse does: asks for its copies, reports it playing, which starts the playback
// the dashboard shows, streams the file with ranges, skips its intro, reads its subtitles beside it, and says
// where it got to; and marks it watched and a favourite.
func TestAnAppPlaysAFilm(t *testing.T) {
	ctx := t.Context()
	st, ada, heat, copyID := aFilm(t)
	log := slog.New(slog.DiscardHandler)
	plays := newFakePlaybacks()
	var told []domain.Event
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Raise:   func(_ context.Context, e domain.Event) { told = append(told, e) },
		Sent:    playback.NewSent(),
		Network: st,
		Auth:    profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Playbacks: plays, Watching: st, Placer: alone(nil),
	})
	const infuse = `MediaBrowser Client="Infuse-Direct", Device="Apple TV", DeviceId="E0BE", Version="8.5.6", Token="pst_ada"`
	call := func(method, target, body string, want int) []byte {
		t.Helper()
		w := serve(api, method, target, infuse, body)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, target, w.Code, w.Body, want)
		}
		return w.Body.Bytes()
	}

	var info struct {
		MediaSources  []map[string]any
		PlaySessionID string `json:"PlaySessionId"`
	}
	if err := json.Unmarshal(call(http.MethodPost, "/Items/"+guid(heat)+"/PlaybackInfo",
		`{"UserId":"`+guid(ada.ID)+`","IsPlayback":true,"AutoOpenLiveStream":true,"EnableDirectPlay":true,"MaxStreamingBitrate":200000000,"DirectPlayProtocols":["Http"],"DeviceProfile":{"MaxStaticBitrate":200000000}}`,
		http.StatusOK), &info); err != nil {
		t.Fatal(err)
	}
	if len(plays.started) != 0 {
		t.Errorf("PlaybackInfo started %v: only playing starts a playback", plays.started)
	}
	// Infuse names nothing it plays as it is, so Jellyfin's rules leave the copy unplayable as it
	// is, as real Jellyfin answers it; Infuse plays the file all the same, at full quality.
	if len(info.MediaSources) != 1 || info.MediaSources[0]["Id"] != guid(copyID) || info.MediaSources[0]["SupportsDirectPlay"] != false ||
		info.MediaSources[0]["TranscodingUrl"] != nil {
		t.Errorf("sources: %v", info.MediaSources)
	}
	streams, _ := info.MediaSources[0]["MediaStreams"].([]any)
	external, _ := streams[len(streams)-1].(map[string]any)
	if external["IsExternal"] != true || external["Index"] != 2.0 || external["Language"] != "eng" {
		t.Errorf("the subtitle file beside it: %v", external)
	}

	w := serve(api, http.MethodGet, "/Videos/"+guid(heat)+"/stream?MediaSourceId="+guid(copyID)+"&Static=true", infuse, "")
	if w.Code != http.StatusOK || w.Body.String() != "0123456789" || w.Header().Get("Content-Type") != "video/x-matroska" {
		t.Errorf("the stream: %d %s %q", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	call(http.MethodGet, "/Videos/"+guid(heat)+"/stream?MediaSourceId=nonsense&Static=true", "", http.StatusBadRequest)
	r := httptest.NewRequest(http.MethodGet, "/videos/"+guid(heat)+"/stream.mkv?static=true", nil)
	r.Header.Set("Authorization", infuse)
	r.Header.Set("Range", "bytes=2-4")
	rw := httptest.NewRecorder()
	api.ServeHTTP(rw, r)
	if rw.Code != http.StatusPartialContent || rw.Body.String() != "234" {
		t.Errorf("a range of the stream: %d %q", rw.Code, rw.Body)
	}
	if sub := call(http.MethodGet, "/Videos/"+guid(heat)+"/"+guid(copyID)+"/Subtitles/2/0/Stream.srt", "", http.StatusOK); !strings.Contains(string(sub), "Hello") {
		t.Errorf("the subtitle file: %q", sub)
	}
	call(http.MethodGet, "/Videos/"+guid(heat)+"/"+guid(copyID)+"/Subtitles/9/0/Stream.srt", "", http.StatusNotFound)

	var segs struct{ Items []map[string]any }
	if err := json.Unmarshal(call(http.MethodGet, "/MediaSegments/"+guid(heat), "", http.StatusOK), &segs); err != nil {
		t.Fatal(err)
	}
	if len(segs.Items) != 1 || segs.Items[0]["Type"] != "Intro" || segs.Items[0]["StartTicks"] != 1e7 || segs.Items[0]["EndTicks"] != 61e7 {
		t.Errorf("segments: %v", segs.Items)
	}

	report := func(path string, ticks int64, session string) {
		call(http.MethodPost, path, `{"ItemId":"`+guid(heat)+`","PlaySessionId":"`+session+`","PositionTicks":`+
			strconv.FormatInt(ticks, 10)+`,"PlayMethod":"DirectStream","IsPaused":false,"AudioStreamIndex":1,"SubtitleStreamIndex":-1}`, http.StatusNoContent)
	}
	report("/Sessions/Playing", 0, info.PlaySessionID)
	report("/Sessions/Playing/Progress", 10*60*1e7, info.PlaySessionID)
	report("/Sessions/Playing/Stopped", 12*60*1e7, info.PlaySessionID)
	session := uuidOf(t, info.PlaySessionID)
	if card, ok := plays.started[session]; !ok || card.Profile.ID != ada.ID || card.Version.ID != copyID {
		t.Errorf("playing started %v, want the session PlaybackInfo named, of the copy", plays.started)
	}
	if len(plays.told) != 2 || plays.told[1] != 10*time.Minute || plays.stopped[session] != 12*time.Minute {
		t.Errorf("the playback was told %v and stopped at %v", plays.told, plays.stopped)
	}
	// An app's own session id, not photon's, is a playback the dashboard shows too.
	report("/Sessions/Playing", 0, "infuse-own-session")
	if _, ok := plays.started[playID("infuse-own-session")]; !ok {
		t.Errorf("an app's own session was not started: %v", plays.started)
	}
	// An app that played without asking first is still kept to where it got to.
	report("/Sessions/Playing/Stopped", 20*60*1e7, "aa11bb22cc33dd44ee55ff6677889900")
	page, err := st.Title(ctx, ada.ID, heat)
	if err != nil || page.State.PositionMS != (20*time.Minute).Milliseconds() {
		t.Errorf("progress kept without a playback: %+v, %v", page.State, err)
	}

	var data map[string]any
	if err := json.Unmarshal(call(http.MethodPost, "/UserPlayedItems/"+guid(heat), "", http.StatusOK), &data); err != nil || data["Played"] != true {
		t.Errorf("marked watched: %v, %v", data, err)
	}
	if err := json.Unmarshal(call(http.MethodPost, "/Users/"+guid(ada.ID)+"/FavoriteItems/"+guid(heat), "", http.StatusOK), &data); err != nil || data["IsFavorite"] != true {
		t.Errorf("a favourite: %v, %v", data, err)
	}
	if err := json.Unmarshal(call(http.MethodDelete, "/UserFavoriteItems/"+guid(heat), "", http.StatusOK), &data); err != nil || data["IsFavorite"] != false {
		t.Errorf("no longer a favourite: %v, %v", data, err)
	}
	// The profile's other devices hear of where it got to without a playback, and of each mark.
	want := domain.Event{Kind: domain.EventUserDataChanged, Profile: ada.ID, Item: heat}
	if len(told) != 4 || slices.ContainsFunc(told, func(e domain.Event) bool {
		return e.Kind != want.Kind || e.Profile != want.Profile || e.Item != want.Item
	}) {
		t.Errorf("told %+v, want four of %+v", told, want)
	}
}

// fakeRemuxes are a node's remuxes: those opened, by playback, and the playlists each answers.
type fakeRemuxes struct {
	opened   map[uuid.UUID]domain.VideoPlan
	segments map[uuid.UUID]domain.SegmentFormat
}

func newFakeRemuxes() *fakeRemuxes {
	return &fakeRemuxes{opened: map[uuid.UUID]domain.VideoPlan{}, segments: map[uuid.UUID]domain.SegmentFormat{}}
}

func (f *fakeRemuxes) Open(_ context.Context, id uuid.UUID, _ store.PlayCopy, video domain.VideoPlan, _ *domain.AudioPlan, segments domain.SegmentFormat, _ time.Duration) error {
	f.opened[id], f.segments[id] = video, segments
	return nil
}

func (f *fakeRemuxes) Has(id uuid.UUID) bool {
	_, ok := f.opened[id]
	return ok
}

func (f *fakeRemuxes) Resource(_ context.Context, id uuid.UUID, name string) (hls.Resource, error) {
	if _, ok := f.opened[id]; !ok {
		return hls.Resource{}, hls.ErrNoRemux
	}
	switch name {
	case hls.MasterName:
		return hls.Resource{Text: "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=8000000\nvideo.m3u8\n", Type: "application/vnd.apple.mpegurl"}, nil
	case "video.m3u8":
		return hls.Resource{Text: "#EXTM3U\n#EXT-X-MAP:URI=\"init0.mp4\"\n#EXTINF:6.0,\n0.m4s\n#EXT-X-ENDLIST\n", Type: "application/vnd.apple.mpegurl"}, nil
	}
	return hls.Resource{}, hls.ErrNoRemux
}

// noOwners is a cluster of one node, which runs every playback itself.
type noOwners struct{}

func (noOwners) Owner(context.Context, uuid.UUID) (string, bool, error) { return "", false, nil }

// An app whose player opens no MKV is given HLS of one, at the TranscodingUrl its PlaybackInfo
// answers: fetching it starts the playback and its remux, once, and every address in its playlists
// carries the app's token; a plan tampered with is refused, and the app ends it as it moves on.
func TestAnAppIsGivenHLSOfWhatItCannotPlayAsItIs(t *testing.T) {
	st, ada, heat, copyID := aFilm(t)
	plays, remuxes := newFakePlaybacks(), newFakeRemuxes()
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Sent:    playback.NewSent(),
		Network: st,
		Auth:    profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Playbacks: plays, Watching: st,
		HLS: remuxes, Placer: alone(remuxes), Owners: noOwners{}, Signer: playback.NewSigner([]byte("key")), Encoding: playback.Encoding{HEVC: domain.HEVCAllow, Libass: true},
	})
	const swiftfinHeader = `MediaBrowser DeviceId=iOS_1, Client=Swiftfin iOS, Version=1.6.1, Device=iPhone, Token=pst_ada`
	w := serve(api, http.MethodPost, "/Items/"+guid(heat)+"/PlaybackInfo", swiftfinHeader, `{"MaxStreamingBitrate":120000000,"DeviceProfile":`+swiftfin+`}`)
	var info struct {
		MediaSources  []map[string]any
		PlaySessionID string `json:"PlaySessionId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	src := info.MediaSources[0]
	transcoding, _ := src["TranscodingUrl"].(string)
	if src["SupportsDirectPlay"] != false || src["TranscodingSubProtocol"] != "hls" || !strings.HasPrefix(transcoding, "/videos/"+guid(heat)+"/master.m3u8?") ||
		!strings.Contains(transcoding, "ApiKey=pst_ada") || !strings.Contains(transcoding, "PlaySessionId="+info.PlaySessionID) {
		t.Fatalf("an MKV for a player that opens none: %v", src)
	}
	if len(remuxes.opened) != 0 || len(plays.started) != 0 {
		t.Error("PlaybackInfo started the remux; only fetching it does")
	}

	// The URL carries its own token: an app sends none with what it fetches of HLS.
	fetch := func(target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, r)
		return rec
	}
	master := fetch(transcoding)
	session := uuidOf(t, info.PlaySessionID)
	if master.Code != http.StatusOK || len(remuxes.opened) != 1 || remuxes.opened[session].Encode != nil {
		t.Fatalf("master: %d %s, opened %v: want H.264 copied out of the MKV", master.Code, master.Body, remuxes.opened)
	}
	if remuxes.segments[session] != domain.SegmentsFMP4 || src["TranscodingContainer"] != "mp4" {
		t.Errorf("Swiftfin's HLS is in %s (%v), want fragmented MP4", remuxes.segments[session], src["TranscodingContainer"])
	}
	if card, ok := plays.started[session]; !ok || card.Version.ID != copyID {
		t.Errorf("the playback the dashboard shows: %v", plays.started)
	}
	query := transcoding[strings.Index(transcoding, "?")+1:]
	if !strings.Contains(master.Body.String(), "video.m3u8?"+query) {
		t.Errorf("the master's variant does not carry the app's query:\n%s", master.Body)
	}
	variant := fetch("/videos/" + guid(heat) + "/video.m3u8?" + query)
	if !strings.Contains(variant.Body.String(), `URI="init0.mp4?`+query+`"`) || !strings.Contains(variant.Body.String(), "0.m4s?"+query) {
		t.Errorf("the variant's addresses do not carry the app's query:\n%s", variant.Body)
	}
	if again := fetch(transcoding); again.Code != http.StatusOK || len(remuxes.opened) != 1 {
		t.Errorf("fetching the master again: %d, %d remuxes", again.Code, len(remuxes.opened))
	}

	tampered := strings.Replace(transcoding, "PlaySessionId="+info.PlaySessionID, "PlaySessionId="+guid(uuid.NewV7()), 1)
	if w := fetch(tampered); w.Code != http.StatusUnauthorized {
		t.Errorf("a plan signed for another session: %d, want 401", w.Code)
	}
	if w := serve(api, http.MethodDelete, "/Videos/ActiveEncodings?deviceId=iOS_1&playSessionId="+info.PlaySessionID, swiftfinHeader, ""); w.Code != http.StatusNoContent || plays.stopped[session] != -1 {
		t.Errorf("ending the encoding: %d, stopped %v", w.Code, plays.stopped)
	}
}

// An app outside the server's networks is kept within the server's limit on a remote stream, the
// video encoded to fit it; on them, it is copied.
func TestARemoteAppIsKeptWithinTheServersLimit(t *testing.T) {
	st, ada, heat, _ := aFilm(t)
	n, err := st.Network(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	n.RemoteMaxBitrateKbps = 2000
	if err := st.SetNetwork(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	remuxes := newFakeRemuxes()
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Sent:    playback.NewSent(),
		Network: st,
		Auth:    profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Playbacks: newFakePlaybacks(), Watching: st,
		HLS: remuxes, Placer: alone(remuxes), Owners: noOwners{}, Signer: playback.NewSigner([]byte("key")), Encoding: playback.Encoding{HEVC: domain.HEVCAllow, Libass: true},
	})
	const swiftfinHeader = `MediaBrowser DeviceId=iOS_1, Client=Swiftfin iOS, Version=1.6.1, Device=iPhone, Token=pst_ada`
	for _, tc := range []struct {
		from    string
		encoded bool
	}{{"192.168.1.20:5000", false}, {"203.0.113.9:5000", true}} {
		r := httptest.NewRequest(http.MethodPost, "/Items/"+guid(heat)+"/PlaybackInfo", strings.NewReader(`{"MaxStreamingBitrate":120000000,"DeviceProfile":`+swiftfin+`}`))
		r.Header.Set("Authorization", swiftfinHeader)
		r.RemoteAddr = tc.from
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		var info struct {
			MediaSources  []map[string]any
			PlaySessionID string `json:"PlaySessionId"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		transcoding, _ := info.MediaSources[0]["TranscodingUrl"].(string)
		api.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, transcoding, nil))
		session := uuidOf(t, info.PlaySessionID)
		e := remuxes.opened[session].Encode
		if tc.encoded != (e != nil) || e != nil && e.BitrateKbps > 2000 {
			t.Errorf("from %s: encode %+v, want encoded %t within 2000 kbps", tc.from, e, tc.encoded)
		}
	}

	// Its network counted as local, the same app has the video copied.
	n.LocalNetworks = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	if err := st.SetNetwork(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/Items/"+guid(heat)+"/PlaybackInfo", strings.NewReader(`{"MaxStreamingBitrate":120000000,"DeviceProfile":`+swiftfin+`}`))
	r.Header.Set("Authorization", swiftfinHeader)
	r.RemoteAddr = "203.0.113.9:5000"
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	var info struct {
		MediaSources  []map[string]any
		PlaySessionID string `json:"PlaySessionId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	transcoding, _ := info.MediaSources[0]["TranscodingUrl"].(string)
	api.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, transcoding, nil))
	session := uuidOf(t, info.PlaySessionID)
	if e := remuxes.opened[session].Encode; e != nil {
		t.Errorf("on a network set as local: encode %+v, want the video copied", e)
	}
}

// Infuse takes HLS only in MPEG-TS, as its profile says, and is given it.
func TestInfuseIsGivenHLSInMPEGTS(t *testing.T) {
	st, ada, heat, _ := aFilm(t)
	remuxes := newFakeRemuxes()
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Sent:    playback.NewSent(),
		Network: st,
		Auth:    profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Playbacks: newFakePlaybacks(), Watching: st,
		HLS: remuxes, Placer: alone(remuxes), Owners: noOwners{}, Signer: playback.NewSigner([]byte("key")), Encoding: playback.Encoding{HEVC: domain.HEVCAllow, Libass: true},
	})
	const infuse = `MediaBrowser Client="Infuse-Direct", Device="Apple TV", DeviceId="E0BE", Version="8.5.6", Token="pst_ada"`
	w := serve(api, http.MethodPost, "/Items/"+guid(heat)+"/PlaybackInfo", infuse, `{"IsPlayback":true,"EnableDirectPlay":true,
		"MaxStreamingBitrate":8000000,"DirectPlayProtocols":["Http"],"DeviceProfile":{"MaxStreamingBitrate":8000000,"TranscodingProfiles":[
		{"Type":"Audio","Container":"aac","AudioCodec":"aac","Protocol":"hls"},
		{"Type":"Video","Container":"ts","VideoCodec":"hevc,h264,av1","AudioCodec":"aac","MaxAudioChannels":"2","Protocol":"hls"}]}}`)
	var info struct {
		MediaSources  []map[string]any
		PlaySessionID string `json:"PlaySessionId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	transcoding, _ := info.MediaSources[0]["TranscodingUrl"].(string)
	if info.MediaSources[0]["TranscodingContainer"] != "ts" || transcoding == "" {
		t.Fatalf("Infuse's source: %v", info.MediaSources[0])
	}
	r := httptest.NewRequest(http.MethodGet, transcoding, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, r)
	session := uuidOf(t, info.PlaySessionID)
	if rec.Code != http.StatusOK || remuxes.segments[session] != domain.SegmentsMPEGTS {
		t.Errorf("master: %d, segments %s, want MPEG-TS", rec.Code, remuxes.segments[session])
	}
}

// alone places every playback on this node, as a server of one does.
func alone(local interface {
	Open(ctx context.Context, playback uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan, segments domain.SegmentFormat, start time.Duration) error
},
) *playback.Placer {
	self := domain.Node{ID: uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e1"), Role: domain.NodeAll, Availability: domain.NodeActive, Limit: 4, Encoder: domain.Encoder{
		Acceleration: domain.AccelSoftware, HEVC: domain.HEVCAllow, Libass: true,
	}}
	return playback.NewPlacer(noNodes{}, func() domain.Node { return self }, local, nodecall.Key{})
}

// noNodes is a cluster of one: no other node tells of itself.
type noNodes struct{}

func (noNodes) Nodes(context.Context) ([]domain.Node, error) { return nil, nil }
