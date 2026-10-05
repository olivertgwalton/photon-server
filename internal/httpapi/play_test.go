package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	partOne = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b1")
	partTwo = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b2")
)

// fakePlaying holds films in two parts under root.
type fakePlaying struct{ root string }

func (fakePlaying) Playable(_ context.Context, item, _ uuid.UUID) (uuid.UUID, []store.PlayPart, error) {
	if item != films {
		return uuid.UUID{}, nil, store.ErrNotFound
	}
	return films, []store.PlayPart{
		{ID: partOne, DurationMS: 3_600_000}, {ID: partTwo, OffsetMS: 3_600_000, DurationMS: 3_000_000},
	}, nil
}

func (f fakePlaying) PartFile(_ context.Context, part uuid.UUID) (string, string, error) {
	if part != partOne {
		return "", "", store.ErrNotFound
	}
	return f.root, "Lawrence/Lawrence cd1.mkv", nil
}

var playbackID = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000c1")

// fakePlaybacks knows one playback, Oliver's.
type fakePlaybacks struct{}

func (fakePlaybacks) Start(_ context.Context, profile, item, version uuid.UUID, method domain.PlayMethod) (domain.Playback, error) {
	return domain.Playback{ID: playbackID, Profile: profile, Item: item, Version: version, Method: method}, nil
}

func (fakePlaybacks) Progress(_ context.Context, profile, id uuid.UUID, _ time.Duration, _ domain.PlayState) (domain.Reach, error) {
	if id != playbackID || profile != oliver.ID {
		return "", playback.ErrNoPlayback
	}
	return domain.ReachResumable, nil
}

func (f fakePlaybacks) Stop(ctx context.Context, profile, id uuid.UUID, at time.Duration) (domain.Reach, error) {
	return f.Progress(ctx, profile, id, at, domain.StatePlaying)
}

func TestAPlaybackReportsWhereItIs(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, Playbacks: fakePlaybacks{}})
	for _, tc := range []struct {
		target, body string
		want         int
	}{
		{"/api/v1/playback/" + playbackID.String() + "/progress", `{"position_ms": 60000, "state": "paused"}`, http.StatusOK},
		{"/api/v1/playback/" + playbackID.String() + "/progress", `{"position_ms": 60000, "state": "rewinding"}`, http.StatusBadRequest},
		{"/api/v1/playback/" + playbackID.String() + "/stop", `{"position_ms": 61000}`, http.StatusOK},
		{"/api/v1/playback/" + uuid.NewV7().String() + "/stop", `{"position_ms": 1}`, http.StatusNotFound},
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
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{
		Auth: fakeAuth{}, Playing: fakePlaying{root: root}, Playbacks: fakePlaybacks{}, Signer: playback.NewSigner([]byte("key")),
	})
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	rec := do(req)
	var got struct {
		PlaybackID uuid.UUID `json:"playback_id"`
		Parts      []struct {
			URL      string `json:"url"`
			OffsetMS int64  `json:"offset_ms"`
		} `json:"parts"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.PlaybackID != playbackID || len(got.Parts) != 2 || got.Parts[1].OffsetMS != 3_600_000 || time.Until(got.ExpiresAt) < 23*time.Hour {
		t.Fatalf("play = %+v, want both parts, the second an hour in, good for a day", got)
	}

	ranged := httptest.NewRequest(http.MethodGet, got.Parts[0].URL, nil)
	ranged.Header.Set("Range", "bytes=2-5")
	if rec := do(ranged); rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" ||
		rec.Header().Get("Content-Type") != "video/x-matroska" {
		t.Errorf("a range of the first part: %d %q %q, want 206 \"2345\" as Matroska", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if rec := do(httptest.NewRequest(http.MethodGet, "/api/v1/parts/"+partOne.String()+"/stream", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("an unsigned address: %d, want 401", rec.Code)
	}
	forged := httptest.NewRequest(http.MethodGet, got.Parts[1].URL, nil)
	forged.URL.Path = "/api/v1/parts/" + partOne.String() + "/stream"
	if rec := do(forged); rec.Code != http.StatusUnauthorized {
		t.Errorf("one part's signature on another's address: %d, want 401", rec.Code)
	}
}

// fakeHLS remuxes into a playlist and one segment, of the playback it was opened for.
type fakeHLS struct{ dir string }

func (fakeHLS) Open(context.Context, uuid.UUID, []store.PlayPart, *int) error { return nil }

func (fakeHLS) Playlist(playback uuid.UUID) (string, error) {
	if playback != playbackID {
		return "", hls.ErrNoRemux
	}
	return "#EXTM3U\n", nil
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
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{
		Auth: fakeAuth{}, Playing: fakePlaying{root: dir}, Playbacks: fakePlaybacks{}, Remuxing: h, HLS: h,
		Signer: playback.NewSigner([]byte("key")),
	})
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(`{"method":"remux"}`))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	var got struct {
		Method   string `json:"method"`
		Playlist string `json:"playlist"`
	}
	if err := json.NewDecoder(do(req).Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "remux" || !strings.HasSuffix(got.Playlist, "/main.m3u8") {
		t.Fatalf("play = %+v, want a remux's playlist", got)
	}
	base := strings.TrimSuffix(got.Playlist, "main.m3u8")
	for file, want := range map[string]string{"main.m3u8": "#EXTM3U\n", "0.m4s": "m4s"} {
		if rec := do(httptest.NewRequest(http.MethodGet, base+file, nil)); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s: %d %q, want %q", file, rec.Code, rec.Body.String(), want)
		}
	}
	other := strings.Replace(base, playbackID.String(), uuid.NewV7().String(), 1)
	if rec := do(httptest.NewRequest(http.MethodGet, other+"0.m4s", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("one playback's signature on another's path: %d, want 401", rec.Code)
	}
	if rec := do(httptest.NewRequest(http.MethodGet, base+"7.m4s", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("a segment there is not: %d, want 404", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(`{"method":"transcode"}`))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	if rec := do(req); rec.Code != http.StatusBadRequest {
		t.Errorf("method transcode: %d, want 400 until it is offered", rec.Code)
	}
}
