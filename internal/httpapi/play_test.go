package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	partOne = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b1")
	partTwo = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b2")
)

// fakePlaying holds films in two parts under root: H.264 in Matroska, with stereo AAC.
type fakePlaying struct{ root string }

func (fakePlaying) Playable(_ context.Context, _, item, _ uuid.UUID) (store.PlayCopy, error) {
	if item != films {
		return store.PlayCopy{}, store.ErrNotFound
	}
	return store.PlayCopy{
		Version: films, Container: "matroska,webm", BitrateKbps: 8000, DurationMS: 6_600_000,
		Parts: []store.PlayPart{
			{ID: partOne, DurationMS: 3_600_000}, {ID: partTwo, OffsetMS: 3_600_000, DurationMS: 3_000_000},
		},
		Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "h264"},
			{Index: 1, Kind: domain.StreamAudio, Codec: "aac", Channels: 2},
		},
		Subtitles: []store.PlaySubtitle{{ID: subtitleID, Codec: "subrip", Language: language.English}},
	}, nil
}

func (fakePlaying) PlaybackTitle(_ context.Context, id uuid.UUID) (domain.PlaybackTitle, error) {
	return domain.PlaybackTitle{ID: id, Kind: domain.ItemMovie, Title: "Lawrence of Arabia", Year: 1962, Poster: posterID}, nil
}

var posterID = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e1")

var (
	subtitleID = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000d1")
	// pictureID is a VobSub beside the film.
	pictureID = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000d2")
)

func (fakePlaying) Subtitle(_ context.Context, id uuid.UUID) (store.PlaySubtitle, error) {
	switch id {
	case subtitleID:
		return store.PlaySubtitle{ID: id, Codec: "subrip", Language: language.English}, nil
	case pictureID:
		return store.PlaySubtitle{ID: id, Codec: "dvd_subtitle"}, nil
	}
	return store.PlaySubtitle{}, store.ErrNotFound
}

func (f fakePlaying) SubtitleFile(_ context.Context, id uuid.UUID) (string, string, error) {
	if id != subtitleID {
		return "", "", store.ErrNotFound
	}
	return f.root, "Lawrence/Lawrence.en.srt", nil
}

// playRequest asks to play films on a client that opens these containers, plays H.264 and AAC, and
// plays a copy's files each in turn.
func playRequest(containers string) *http.Request {
	body := `{"profile": {"containers": [` + containers + `], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "parts": "each"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	return req
}

func (f fakePlaying) PartFile(_ context.Context, part uuid.UUID) (string, string, error) {
	if part != partOne {
		return "", "", store.ErrNotFound
	}
	return f.root, "Lawrence/Lawrence cd1.mkv", nil
}

// VisiblePartFile hides the first part from everyone but Oliver.
func (f fakePlaying) VisiblePartFile(ctx context.Context, profile, part uuid.UUID) (string, string, error) {
	if profile != oliver.ID {
		return "", "", store.ErrNotFound
	}
	return f.PartFile(ctx, part)
}

var playbackID = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000c1")

// fakePlaybacks knows one playback, Oliver's.
type fakePlaybacks struct{}

func (fakePlaybacks) Start(_ context.Context, method domain.PlayMethod, card domain.PlaybackCard) (domain.Playback, error) {
	return domain.Playback{ID: playbackID, Profile: card.Profile.ID, Item: card.Title.ID, Version: card.Version.ID, Method: method, Card: card}, nil
}

func (fakePlaybacks) Progress(_ context.Context, profile, id uuid.UUID, _ time.Duration, _ domain.PlayState, _ domain.ChosenTracks) (domain.Reach, error) {
	if id != playbackID || profile != oliver.ID {
		return "", playback.ErrNoPlayback
	}
	return domain.ReachResumable, nil
}

func (f fakePlaybacks) Stop(ctx context.Context, profile, id uuid.UUID, at time.Duration) (domain.Reach, error) {
	return f.Progress(ctx, profile, id, at, domain.StatePlaying, domain.ChosenTracks{})
}

func (fakePlaybacks) End(_ context.Context, id uuid.UUID) error {
	if id != playbackID {
		return playback.ErrNoPlayback
	}
	return nil
}

func (fakePlaybacks) Abandon(context.Context, uuid.UUID) error { return nil }

func (fakePlaybacks) Serve(context.Context, uuid.UUID, func()) (func(), error) { return func() {}, nil }

func TestAPlaybackReportsWhereItIs(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Playbacks: fakePlaybacks{}})
	for _, tc := range []struct {
		target, body string
		want         int
	}{
		{"/api/v1/playbacks/" + playbackID.String() + "/progress", `{"position_ms": 60000, "state": "paused"}`, http.StatusOK},
		{"/api/v1/playbacks/" + playbackID.String() + "/progress", `{"position_ms": 60000, "state": "rewinding"}`, http.StatusBadRequest},
		{"/api/v1/playbacks/" + playbackID.String() + "/stop", `{"position_ms": 61000}`, http.StatusOK},
		{"/api/v1/playbacks/" + uuid.NewV7().String() + "/stop", `{"position_ms": 1}`, http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s: %d, want %d", tc.target, tc.body, rec.Code, tc.want)
		}
	}
}

func TestAFilmPlaysFromSignedAddresses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Lawrence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Lawrence", "Lawrence cd1.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Lawrence", "Lawrence.en.srt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{root: root}, Playbacks: fakePlaybacks{}, HLS: fakeHLS{},
		Signer: playback.NewSigner([]byte("key")),
	})
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	rec := do(playRequest(`"matroska"`))
	var got struct {
		PlaybackID uuid.UUID `json:"playback_id"`
		Parts      []struct {
			URL      string `json:"url"`
			OffsetMS int64  `json:"offset_ms"`
		} `json:"parts"`
		Subtitles []struct {
			Language string `json:"language"`
			URL      string `json:"url"`
		} `json:"subtitles"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.PlaybackID != playbackID || len(got.Parts) != 2 || got.Parts[1].OffsetMS != 3_600_000 || time.Until(got.ExpiresAt) < 23*time.Hour {
		t.Fatalf("play = %+v, want both parts, the second an hour in, good for a day", got)
	}

	if len(got.Subtitles) != 1 || got.Subtitles[0].Language != "en" {
		t.Fatalf("subtitles = %+v, want the English file", got.Subtitles)
	}
	if rec := do(httptest.NewRequest(http.MethodGet, got.Subtitles[0].URL, nil)); rec.Code != http.StatusOK ||
		rec.Body.String() != "1\n" || rec.Header().Get("Content-Type") != "application/x-subrip" {
		t.Errorf("the subtitle file: %d %q %q, want it as SubRip", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if rec := do(httptest.NewRequest(http.MethodGet, got.Subtitles[0].URL+"&format=webvtt", nil)); rec.Code != http.StatusOK ||
		rec.Body.String() != "WEBVTT en\n\n1\n" || rec.Header().Get("Content-Type") != "text/vtt; charset=utf-8" {
		t.Errorf("the subtitle file as WebVTT: %d %q %q, want it converted as English", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if rec := do(httptest.NewRequest(http.MethodGet, got.Subtitles[0].URL+"&format=ass", nil)); rec.Code != http.StatusBadRequest {
		t.Errorf("as a format there is not: %d, want 400", rec.Code)
	}
	picture := playback.NewSigner([]byte("key")).Sign("/api/v1/subtitles/"+pictureID.String()+"/file", time.Now().Add(time.Hour))
	if rec := do(httptest.NewRequest(http.MethodGet, picture+"&format=webvtt", nil)); rec.Code != http.StatusBadRequest {
		t.Errorf("pictures as WebVTT: %d, want 400", rec.Code)
	}

	ranged := httptest.NewRequest(http.MethodGet, got.Parts[0].URL, nil)
	ranged.Header.Set("Range", "bytes=2-5")
	ranged.Header.Set("Accept-Encoding", "gzip")
	if rec := do(ranged); rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" ||
		rec.Header().Get("Content-Type") != "video/x-matroska" || rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("a range of the first part: %d %q %q, want 206 \"2345\" as Matroska, as it is", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if rec := do(httptest.NewRequest(http.MethodGet, "/api/v1/parts/"+partOne.String()+"/stream", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("an unsigned address: %d, want 401", rec.Code)
	}
	forged := httptest.NewRequest(http.MethodGet, got.Parts[1].URL, nil)
	forged.URL.Path = strings.Replace(forged.URL.Path, partTwo.String(), partOne.String(), 1)
	if rec := do(forged); rec.Code != http.StatusUnauthorized {
		t.Errorf("one part's signature on another's address: %d, want 401", rec.Code)
	}
}

// fakeHLS remuxes into a playlist and one segment, of the playback it was opened for.
type fakeHLS struct{ dir string }

func (fakeHLS) Open(context.Context, uuid.UUID, store.PlayCopy, domain.VideoPlan, *domain.AudioPlan, domain.SegmentFormat, time.Duration) error {
	return nil
}

func (fakeHLS) Transcodes() (active, conversions, limit int) { return 1, 0, 4 }

func (fakeHLS) Encoder(domain.VideoPlan) domain.Acceleration { return domain.AccelSoftware }

func (fakeHLS) Has(playback uuid.UUID) bool { return playback == playbackID }

func (fakeHLS) Playlist(playback uuid.UUID, name string) (string, error) {
	if playback != playbackID || name != "main.m3u8" {
		return "", hls.ErrNoRemux
	}
	return "#EXTM3U\n", nil
}

func (fakeHLS) SubtitleSegment(_ context.Context, playback uuid.UUID, track, n int) (string, error) {
	if playback != playbackID || track != 0 || n != 2 {
		return "", hls.ErrNoRemux
	}
	return "WEBVTT\n", nil
}

// WebVTT answers the file's text under a WebVTT header naming its language.
func (fakeHLS) WebVTT(_ context.Context, open func() (*os.File, error), language string) (string, error) {
	f, err := open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	return "WEBVTT " + language + "\n\n" + string(b), err
}

func (f fakeHLS) Init(context.Context, uuid.UUID, int) (*os.File, error) {
	return os.Open(filepath.Join(f.dir, "segment"))
}

func (f fakeHLS) Segment(_ context.Context, playback uuid.UUID, n int) (*os.File, error) {
	if playback != playbackID || n != 0 {
		return nil, hls.ErrNoRemux
	}
	return os.Open(filepath.Join(f.dir, "segment"))
}

func TestARemuxPlaysFromOneSignedPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "segment"), []byte("m4s"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := fakeHLS{dir: dir}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{root: dir}, Playbacks: fakePlaybacks{}, Remuxing: h, HLS: h,
		Signer: playback.NewSigner([]byte("key")),
	})
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	var got struct {
		Method   string   `json:"method"`
		Reasons  []string `json:"reasons"`
		Playlist string   `json:"playlist"`
	}
	if err := json.NewDecoder(do(playRequest(`"mp4"`)).Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "remux" || !strings.HasSuffix(got.Playlist, "/main.m3u8") || len(got.Reasons) != 1 || got.Reasons[0] != "container_not_supported" {
		t.Fatalf("play = %+v, want a remux's playlist, for the container", got)
	}
	joined := playRequest(`"matroska"`)
	joined.Body = io.NopCloser(strings.NewReader(`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}]}}`))
	var whole struct {
		Method  string   `json:"method"`
		Reasons []string `json:"reasons"`
	}
	if err := json.NewDecoder(do(joined).Body).Decode(&whole); err != nil {
		t.Fatal(err)
	}
	if whole.Method != "remux" || !slices.Equal(whole.Reasons, []string{"parts_not_supported"}) {
		t.Errorf("a film in two files on a client that plays one: %+v, want them joined in one remux", whole)
	}
	base := strings.TrimSuffix(got.Playlist, "main.m3u8")
	for file, want := range map[string]string{"main.m3u8": "#EXTM3U\n", "0.m4s": "m4s", "sub0-2.vtt": "WEBVTT\n"} {
		if rec := do(httptest.NewRequest(http.MethodGet, base+file, nil)); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s: %d %q, want %q", file, rec.Code, rec.Body.String(), want)
		}
	}
	for file, want := range map[string]string{"0.m4s": "video/iso.segment", "0.ts": "video/mp2t"} {
		if got := do(httptest.NewRequest(http.MethodGet, base+file, nil)).Header().Get("Content-Type"); got != want {
			t.Errorf("%s served as %q, want %q", file, got, want)
		}
	}
	other := strings.Replace(base, playbackID.String(), uuid.NewV7().String(), 1)
	if rec := do(httptest.NewRequest(http.MethodGet, other+"0.m4s", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("one playback's signature on another's path: %d, want 401", rec.Code)
	}
	if rec := do(httptest.NewRequest(http.MethodGet, base+"7.m4s", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("a segment there is not: %d, want 404", rec.Code)
	}
}

func TestAClientIsToldWhyNothingPlays(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{}, Playbacks: fakePlaybacks{}})
	for _, tc := range []struct {
		body        string
		wantStatus  int
		wantReasons []string
	}{
		{`{}`, http.StatusBadRequest, nil},
		{
			`{"profile": {"containers": ["mp4"], "video": [{"codec": "hevc"}], "audio": [{"codec": "aac"}]}}`, http.StatusUnprocessableEntity,
			[]string{"container_not_supported", "parts_not_supported", "video_codec_not_supported"},
		},
		{`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "parts": "some"}}`, http.StatusBadRequest, nil},
		{`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "segments": "webm"}}`, http.StatusBadRequest, nil},
		{`{"audio_stream": 0, "profile": {"containers": ["matroska"], "video": [{"codec": "h264"}]}}`, http.StatusBadRequest, nil},
		{`{"start_ms": -1, "profile": {"containers": ["matroska"], "video": [{"codec": "h264"}]}}`, http.StatusBadRequest, nil},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var got struct {
			Reasons []string `json:"reasons"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&got)
		if rec.Code != tc.wantStatus || !slices.Equal(got.Reasons, tc.wantReasons) {
			t.Errorf("%s: %d %v, want %d %v", tc.body, rec.Code, got.Reasons, tc.wantStatus, tc.wantReasons)
		}
	}
}

// noHLS runs no remux: the playback is another node's.
type noHLS struct{ fakeHLS }

func (noHLS) Has(uuid.UUID) bool { return false }

func (noHLS) Playbacks() []uuid.UUID { return nil }

func (noHLS) Close(uuid.UUID) {}

type owner string

func (o owner) Owner(_ context.Context, playback uuid.UUID) (string, bool, error) {
	return string(o), playback == playbackID, nil
}

func TestHLSIsServedByTheNodeRunningIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "segment"), []byte("m4s"), 0o644); err != nil {
		t.Fatal(err)
	}
	signer := playback.NewSigner([]byte("key"))
	running := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, HLS: fakeHLS{dir: dir}, Signer: signer, Playbacks: fakePlaybacks{},
	}))
	defer running.Close()
	// The front node keeps no playbacks, so one it ended itself would answer 404.
	none := &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}
	front := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, HLS: noHLS{}, Owners: owner(running.URL), Signer: signer,
		Playbacks: playback.NewSessions(none, none, noHLS{}, func(context.Context, domain.Event) {}, uuid.NewV7()),
	})
	subject := hlsSubject(playbackID)
	exp, sig := signer.Token(subject, time.Now().Add(time.Hour))
	rec := httptest.NewRecorder()
	front.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, subject+"/"+exp+"/"+sig+"/0.m4s", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "m4s" {
		t.Errorf("a segment another node makes: %d %q, want it from that node", rec.Code, rec.Body.String())
	}
	stop := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/playbacks/"+playbackID.String(), nil)
	stop.Header.Set("Authorization", "Bearer "+goodToken)
	rec = httptest.NewRecorder()
	front.ServeHTTP(rec, stop)
	if rec.Code != http.StatusNoContent {
		t.Errorf("an admin stopping a playback another node runs: %d %s, want it stopped there", rec.Code, rec.Body)
	}
	// Stopped where it runs, so its remux ends and its transcode slot is free at once.
	stop = httptest.NewRequest(http.MethodPost, "/api/v1/playbacks/"+playbackID.String()+"/stop", strings.NewReader(`{"position_ms": 1000}`))
	stop.Header.Set("Authorization", "Bearer "+goodToken)
	rec = httptest.NewRecorder()
	front.ServeHTTP(rec, stop)
	if rec.Code != http.StatusOK {
		t.Errorf("its player stopping a playback another node runs: %d %s, want it stopped there", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	front.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, subject+"/"+exp+"/forged/0.m4s", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a forged signature: %d, want 401 before any node is asked", rec.Code)
	}
}

// livePlaybacks keeps playbacks as Valkey does, and the places in them nowhere.
type livePlaybacks struct {
	mu sync.Mutex
	m  map[uuid.UUID]domain.Playback
}

func (l *livePlaybacks) SavePlayback(_ context.Context, p domain.Playback, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[p.ID] = p
	return nil
}

func (l *livePlaybacks) Playback(_ context.Context, id uuid.UUID) (domain.Playback, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.m[id]
	return p, ok, nil
}

func (l *livePlaybacks) EndPlayback(_ context.Context, id uuid.UUID) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.m[id]
	delete(l.m, id)
	return ok, nil
}

func (l *livePlaybacks) Playbacks(context.Context) ([]domain.Playback, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Collect(maps.Values(l.m)), nil
}

func (*livePlaybacks) Length(context.Context, uuid.UUID) (time.Duration, error) {
	return 2 * time.Hour, nil
}

func (*livePlaybacks) SaveProgress(context.Context, uuid.UUID, uuid.UUID, time.Duration, time.Duration, domain.Reach, *time.Time) (domain.Reach, error) {
	return domain.ReachResumable, nil
}

func (*livePlaybacks) ChooseTracks(context.Context, uuid.UUID, uuid.UUID, domain.ChosenTracks) error {
	return nil
}

func (*livePlaybacks) RecordPlay(context.Context, domain.Playback, time.Time, time.Duration) error {
	return nil
}

// remuxOpener opens a copy's first part, as decided, on a real remuxer.
type remuxOpener struct{ *hls.Remuxer }

func (r remuxOpener) Open(ctx context.Context, id uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan, segments domain.SegmentFormat, start time.Duration) error {
	d := time.Duration(c.Parts[0].DurationMS) * time.Millisecond
	return r.Remuxer.Open(ctx, id, hls.Copy{Parts: []hls.Source{{Open: func() (*os.File, error) { return nil, os.ErrNotExist }, Part: hls.Part{Duration: d, Keyframes: hls.Forced(d)}, Video: video, Audio: audio}}, Start: start, Segments: segments})
}

// A player that takes MPEG-TS alone is given a playlist of it; one that does not say, of
// fragmented MP4.
func TestAPlayerIsGivenTheSegmentsItAsksFor(t *testing.T) {
	remuxer, err := hls.NewRemuxer("ffmpeg", t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, hls.Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	live := &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{}, Playbacks: playback.NewSessions(live, live, remuxer, func(context.Context, domain.Event) {}, uuid.NewV7()),
		Remuxing: remuxOpener{remuxer}, HLS: remuxer, NowPlaying: live, Signer: playback.NewSigner([]byte("key")),
	})
	for segments, want := range map[string]string{``: "\n0.m4s\n", `, "segments": "fmp4"`: "\n0.m4s\n", `, "segments": "mpegts"`: "\n0.ts\n"} {
		body := `{"profile": {"containers": ["mp4"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "parts": "each"` + segments + `}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var got struct {
			Playlist string `json:"playlist"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got.Playlist == "" {
			t.Fatalf("play asking %q: %d, want a playlist", segments, rec.Code)
		}
		rec = httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, strings.TrimSuffix(got.Playlist, "main.m3u8")+"video.m3u8", nil))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("play asking %q: playlist\n%s\nwant %q in it", segments, rec.Body, want)
		}
	}
}

func TestAServerTranscodesNoMoreThanItsLimit(t *testing.T) {
	remuxer, err := hls.NewRemuxer("ffmpeg", t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, 1, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	live := &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{}, Playbacks: playback.NewSessions(live, live, remuxer, func(context.Context, domain.Event) {}, uuid.NewV7()),
		Remuxing: remuxOpener{remuxer}, HLS: remuxer, NowPlaying: live, Signer: playback.NewSigner([]byte("key")),
	})
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	// Two megabits is less than the film's eight, so its video is encoded.
	transcode := func() *httptest.ResponseRecorder {
		body := `{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		return do(req)
	}
	admin := func() (transcodes struct{ Active, Conversions, Limit int }) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/playbacks", nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		var playing struct {
			Transcodes struct{ Active, Conversions, Limit int }
		}
		if err := json.NewDecoder(do(req).Body).Decode(&playing); err != nil {
			t.Fatal(err)
		}
		return playing.Transcodes
	}
	converting, release, ok := remuxer.HoldConversion(t.Context())
	if !ok {
		t.Fatal("a conversion on an idle node had no slot")
	}
	defer release()
	if got := admin(); got.Active != 1 || got.Conversions != 1 {
		t.Errorf("transcodes while converting = %+v, want the conversion counted", got)
	}
	var first struct {
		PlaybackID uuid.UUID `json:"playback_id"`
		Method     string    `json:"method"`
	}
	if err := json.NewDecoder(transcode().Body).Decode(&first); err != nil || first.Method != "transcode" {
		t.Fatalf("the first play = %+v, %v; want a transcode, the conversion stopped for it", first, err)
	}
	if !errors.Is(context.Cause(converting), hls.ErrPreempted) {
		t.Errorf("the conversion after a play took its slot: %v, want it stopped", context.Cause(converting))
	}
	var refused problem
	if rec := transcode(); rec.Code != http.StatusServiceUnavailable || json.NewDecoder(rec.Body).Decode(&refused) != nil ||
		refused.Code != codeTranscodeLimit || !strings.HasSuffix(refused.Detail, ": 1") {
		t.Errorf("a second transcode: %d %+v, want 503 transcode_limit naming the limit", rec.Code, refused)
	}
	if rec := do(playRequest(`"mp4"`)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"method":"remux"`) {
		t.Errorf("a remux at the limit: %d %s, want it played", rec.Code, rec.Body)
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/admin/playbacks", nil)
	list.Header.Set("Authorization", "Bearer "+goodToken)
	var playing struct {
		Items      []json.RawMessage `json:"items"`
		Transcodes struct{ Active, Conversions, Limit int }
	}
	if err := json.NewDecoder(do(list).Body).Decode(&playing); err != nil || len(playing.Items) != 2 ||
		playing.Transcodes.Active != 1 || playing.Transcodes.Conversions != 0 || playing.Transcodes.Limit != 1 {
		t.Errorf("admin playbacks = %+v, %v; want the transcode and the remux, one of one transcoding", playing, err)
	}

	stop := httptest.NewRequest(http.MethodPost, "/api/v1/playbacks/"+first.PlaybackID.String()+"/stop", strings.NewReader(`{"position_ms": 1000}`))
	stop.Header.Set("Authorization", "Bearer "+goodToken)
	if rec := do(stop); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if rec := transcode(); rec.Code != http.StatusOK {
		t.Errorf("a transcode once the first stopped: %d %s, want it played", rec.Code, rec.Body)
	}
}

func TestTheDashboardShowsAPlaybackAndStopsIt(t *testing.T) {
	remuxer, err := hls.NewRemuxer("ffmpeg", t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, hls.Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	live := &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}
	var told []domain.Event
	raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{}, Playbacks: playback.NewSessions(live, live, remuxer, raise, uuid.NewV7()),
		Remuxing: remuxOpener{remuxer}, HLS: remuxer, NowPlaying: live, Signer: playback.NewSigner([]byte("key")),
	})
	do := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		req.RemoteAddr = "192.0.2.7:51000"
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	// Two megabits is less than the film's eight, so its video is encoded.
	var started struct {
		PlaybackID uuid.UUID `json:"playback_id"`
		Playlist   string    `json:"playlist"`
	}
	body := `{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`
	if err := json.NewDecoder(do(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", body).Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	list := func() (items []json.RawMessage) {
		var playing struct{ Items []json.RawMessage }
		if err := json.NewDecoder(do(http.MethodGet, "/api/v1/admin/playbacks", "").Body).Decode(&playing); err != nil {
			t.Fatal(err)
		}
		return playing.Items
	}
	items := list()
	var shown playback.NowPlaying
	if len(items) != 1 || json.Unmarshal(items[0], &shown) != nil {
		t.Fatalf("admin playbacks = %s, want the one playing", items)
	}
	want := domain.PlaybackCard{
		Profile: domain.PlaybackProfile{ID: oliver.ID, Name: "Oliver"},
		Device:  domain.PlaybackDevice{ID: shown.Device.ID, Name: "Living room", Client: "Photon Web 1.0", Address: "192.0.2.7"},
		Title:   domain.PlaybackTitle{ID: films, Kind: domain.ItemMovie, Title: "Lawrence of Arabia", Year: 1962, Poster: posterID},
		Version: domain.PlaybackVersion{ID: films, Container: "matroska,webm", BitrateKbps: 8000, DurationMS: 6_600_000},
		Reasons: []domain.TranscodeReason{domain.BitrateExceedsLimit},
		Video: &domain.PlaybackVideo{
			Codec: "h264", Encode: &domain.PlaybackEncode{Codec: "h264", Range: domain.RangeSDR, BitrateKbps: shown.Video.Encode.BitrateKbps},
		},
		Audio: &domain.PlaybackAudio{
			Stream: 1, Codec: "aac", Channels: 2, Encode: &domain.PlaybackEncode{Codec: "aac", Channels: 2, BitrateKbps: 256},
		},
		Acceleration: domain.AccelSoftware,
	}
	if shown.ID != started.PlaybackID || shown.Method != domain.PlayTranscode || !cmp.Equal(shown.PlaybackCard, want) {
		t.Errorf("the playback shown: %s; want %+v", items[0], want)
	}
	if got, _ := json.Marshal(told[0].Details["playback"]); told[0].Kind != domain.EventPlaybackStarted || string(got) != string(items[0]) {
		t.Errorf("told %v %s; want it started, shown as the list shows it", told[0].Kind, got)
	}

	target := "/api/v1/admin/playbacks/" + started.PlaybackID.String()
	if rec := do(http.MethodDelete, target, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("stopping it: %d %s", rec.Code, rec.Body)
	}
	if remuxer.Has(started.PlaybackID) || len(list()) != 0 {
		t.Errorf("after stopping: remux running %v, listed %d; want neither", remuxer.Has(started.PlaybackID), len(list()))
	}
	if last := told[len(told)-1]; last.Kind != domain.EventPlaybackStopped {
		t.Errorf("told %v, want it stopped", last.Kind)
	}
	if rec := do(http.MethodPost, "/api/v1/playbacks/"+started.PlaybackID.String()+"/progress", `{"position_ms": 5000, "state": "playing"}`); rec.Code != http.StatusNotFound {
		t.Errorf("its player reporting after: %d, want 404", rec.Code)
	}
	if rec := do(http.MethodGet, started.Playlist, ""); rec.Code != http.StatusNotFound {
		t.Errorf("its player asking for its playlist after: %d, want 404", rec.Code)
	}
	if rec := do(http.MethodDelete, target, ""); rec.Code != http.StatusNotFound {
		t.Errorf("stopping it again: %d, want 404", rec.Code)
	}
}

func TestAConnectionIsTimedOnAPartsFirstBytesWithoutPlaying(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Lawrence"), 0o755); err != nil {
		t.Fatal(err)
	}
	film := make([]byte, sampleBytes+1)
	film[sampleBytes-1] = 'x'
	if err := os.WriteFile(filepath.Join(root, "Lawrence", "Lawrence cd1.mkv"), film, 0o644); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{root: root}})
	sample := func(token, ranges string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/parts/"+partOne.String()+"/sample", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if ranges != "" {
			req.Header.Set("Range", ranges)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := sample(goodToken, "bytes=0-4194303"); rec.Code != http.StatusPartialContent || rec.Body.Len() != 4<<20 {
		t.Errorf("4 MiB from the start: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := sample(goodToken, ""); rec.Code != http.StatusOK || rec.Body.Len() != sampleBytes || rec.Body.Bytes()[sampleBytes-1] != 'x' {
		t.Errorf("the whole sample: %d, %d bytes, want the file's first %d", rec.Code, rec.Body.Len(), sampleBytes)
	}
	if rec := sample(goodToken, "bytes="+strconv.Itoa(sampleBytes)+"-"); rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("past the sample: %d, want 416", rec.Code)
	}
	if rec := sample(memberToken, ""); rec.Code != http.StatusNotFound {
		t.Errorf("a profile that may not see the title: %d, want 404", rec.Code)
	}
	if rec := sample("", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out: %d, want 401", rec.Code)
	}
}
