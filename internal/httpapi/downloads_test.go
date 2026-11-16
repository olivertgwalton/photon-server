package httpapi

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var downloadID = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e1")

// fakeDownloads answers every download asked for as one: the part's file, ready and asked for
// before, or a conversion queued, new.
type fakeDownloads struct{}

func (fakeDownloads) AddDownload(_ context.Context, _, device, item, part uuid.UUID, q *domain.Quality) (store.Download, bool, error) {
	d := store.Download{ID: downloadID, Device: device, Item: item, Part: part, Quality: q, State: domain.DownloadReady}
	if q != nil {
		d.State = domain.DownloadQueued
	}
	return d, q != nil, nil
}

// Downloads holds one download on the device asking and one on another.
func (fakeDownloads) Downloads(_ context.Context, _ uuid.UUID, device *uuid.UUID) ([]store.Download, error) {
	if device != nil {
		return []store.Download{{ID: downloadID, Device: *device}}, nil
	}
	return []store.Download{{ID: downloadID}, {ID: uuid.NewV7(), Device: uuid.NewV7()}}, nil
}

func (fakeDownloads) Download(context.Context, uuid.UUID, uuid.UUID) (store.Download, error) {
	return store.Download{}, store.ErrNotFound
}

// fakeConversions holds one ready conversion's file.
type fakeConversions struct{ path string }

func (fakeConversions) Remove(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f fakeConversions) File(_ context.Context, id uuid.UUID) (*os.File, string, error) {
	if id != downloadID {
		return nil, "", store.ErrNotFound
	}
	file, err := os.Open(f.path)
	return file, "", err
}

func TestADownloadIsTheFileOrAConversionServedInRanges(t *testing.T) {
	converted := filepath.Join(t.TempDir(), "c.mp4")
	if err := os.WriteFile(converted, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	signer := playback.NewSigner([]byte("key"))
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{},
		Sent: playback.NewSent(),
		Auth: fakeAuth{}, Playing: fakePlaying{}, Downloads: fakeDownloads{},
		Conversions: fakeConversions{path: converted}, Signer: signer, HLS: fakeHLS{hw: hls.Hardware{HEVC: domain.HEVCAllow}},
	})
	ask := func(body string) (int, downloadJSON) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var d downloadJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return rec.Code, d
	}
	film := `"title_id": "` + films.String() + `"`
	if code, _ := ask(`{` + film + `, "max_bitrate_kbps": 10000}`); code != http.StatusBadRequest {
		t.Errorf("a two-file version without part_id: %d, want 400", code)
	}
	part := `, "part_id": "` + partOne.String() + `"`
	// The film is 8 Mbps H.264 1080p or smaller.
	code, d := ask(`{` + film + part + `, "max_bitrate_kbps": 10000}`)
	if code != http.StatusOK || d.Method != domain.PlayDirect || d.State != domain.DownloadReady ||
		!strings.HasPrefix(string(d.URL), "/api/v1/parts/"+partOne.String()+"/stream?") {
		t.Errorf("within the quality, asked before: %d %+v; want 200 and the part's own file, ready", code, d)
	}
	code, d = ask(`{` + film + part + `}`)
	if code != http.StatusOK || d.Method != domain.PlayDirect || d.State != domain.DownloadReady {
		t.Errorf("at no bitrate: %d %+v; want 200 and the part's own file, ready", code, d)
	}
	if code, _ := ask(`{` + film + part + `, "max_bitrate_kbps": -1}`); code != http.StatusBadRequest {
		t.Errorf("a bitrate below nothing: %d, want 400", code)
	}
	code, d = ask(`{` + film + part + `, "max_bitrate_kbps": 2000, "max_width": 1280}`)
	if code != http.StatusCreated || d.Method != domain.PlayTranscode || d.State != domain.DownloadQueued || d.URL != "" ||
		d.MaxBitrateKbps != 2000 || d.MaxWidth != 1280 {
		t.Errorf("over it: %d %+v; want 201 and a conversion queued, with no address yet", code, d)
	}
	if d.VideoCodec != domain.VideoH264 || d.VideoRange != domain.RangeSDR {
		t.Errorf("for a device that says nothing of its video: %s in %s, want H.264 in SDR", d.VideoCodec, d.VideoRange)
	}
	code, d = ask(`{` + film + part + `, "max_bitrate_kbps": 2000, "video_codecs": ["h264", "hevc"], "video_ranges": ["sdr", "hdr10"]}`)
	if code != http.StatusCreated || d.VideoCodec != domain.VideoHEVC || d.VideoRange != domain.RangeSDR {
		t.Errorf("for a device of HEVC: %d %+v; want the SDR film converted to HEVC", code, d)
	}
	if code, _ := ask(`{` + film + part + `, "max_bitrate_kbps": 2000, "video_codecs": ["hevc"], "video_ranges": ["hdr"]}`); code != http.StatusBadRequest {
		t.Errorf("a range there is none of: %d, want 400", code)
	}

	file := signer.Sign("/api/v1/downloads/"+downloadID.String()+"/file", time.Now().Add(time.Hour))
	for _, tc := range []struct {
		target string
		want   int
		body   string
	}{
		{file, http.StatusPartialContent, "2345"},
		{"/api/v1/downloads/" + downloadID.String() + "/file", http.StatusUnauthorized, ""},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Range", "bytes=2-5")
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || (tc.body != "" && rec.Body.String() != tc.body) {
			t.Errorf("%s: %d %q, want %d %q", tc.target, rec.Code, rec.Body, tc.want, tc.body)
		}
	}
}

func TestADeviceListsItsOwnDownloadsUnlessItAsksForTheProfiles(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Downloads: fakeDownloads{}})
	for _, tc := range []struct {
		target string
		code   int
		want   int
	}{
		{"/api/v1/downloads", http.StatusOK, 1},
		{"/api/v1/downloads?scope=device", http.StatusOK, 1},
		{"/api/v1/downloads?scope=profile", http.StatusOK, 2},
		{"/api/v1/downloads?scope=server", http.StatusBadRequest, 0},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var got listJSON[downloadJSON]
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", tc.target, err)
		}
		if rec.Code != tc.code || len(got.Items) != tc.want {
			t.Errorf("%s: %d with %d downloads, want %d with %d", tc.target, rec.Code, len(got.Items), tc.code, tc.want)
		}
	}
}

// besideDownloads holds one ready download of a version with subtitle files beside it, and one
// whose conversion is still queued.
type besideDownloads struct {
	fakeDownloads
	ready, queued uuid.UUID
	subtitles     []store.SubtitleRef
}

func (b besideDownloads) Download(_ context.Context, _, id uuid.UUID) (store.Download, error) {
	switch id {
	case b.ready:
		return store.Download{ID: id, Part: partOne, State: domain.DownloadReady, Subtitles: b.subtitles}, nil
	case b.queued:
		q := domain.Quality{MaxBitrateKbps: 2000}
		return store.Download{ID: id, Part: partOne, Quality: &q, State: domain.DownloadQueued, Subtitles: b.subtitles}, nil
	}
	return store.Download{}, store.ErrNotFound
}

// A ready download answers the subtitle files beside its version at addresses signed as its own
// file is, which open each file; one still converting answers none yet, as it answers no file. The
// address is a signed one alone: a session's token does not open it.
func TestADownloadAnswersTheSubtitleFilesBesideIt(t *testing.T) {
	root := t.TempDir()
	const srt = "1\n00:00:01,000 --> 00:00:02,000\nBeside the film.\n"
	if err := os.WriteFile(filepath.Join(root, "Film.en.srt"), []byte(srt), 0o600); err != nil {
		t.Fatal(err)
	}
	sub, queued := uuid.NewV7(), uuid.NewV7()
	signer := playback.NewSigner([]byte("key"))
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Signer: signer,
		Playing: sidecarPlaying{fakePlaying{root: root}, map[uuid.UUID]sidecar{sub: {"Film.en.srt", "subrip", language.English}}},
		Downloads: besideDownloads{
			ready: downloadID, queued: queued,
			subtitles: []store.SubtitleRef{{ID: sub, Codec: "subrip", Language: language.English}},
		},
	})
	get := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	answer := func(id uuid.UUID) downloadJSON {
		rec := get("/api/v1/downloads/" + id.String())
		var d downloadJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &d); rec.Code != http.StatusOK || err != nil {
			t.Fatalf("download %s: %d %v", id, rec.Code, err)
		}
		return d
	}

	ready := answer(downloadID)
	if len(ready.Subtitles) != 1 || ready.Subtitles[0].ID != sub || ready.Subtitles[0].Codec != "subrip" ||
		ready.Subtitles[0].Language != domain.TagOf(language.English) {
		t.Fatalf("a ready download's subtitles: %+v, want the one file beside it", ready.Subtitles)
	}
	at := string(ready.Subtitles[0].URL)
	if !strings.HasPrefix(at, "/api/v1/subtitles/"+sub.String()+"/file?") {
		t.Errorf("its address: %q, want the file's own, signed", at)
	}
	if rec := get(at); rec.Code != http.StatusOK || rec.Body.String() != srt {
		t.Errorf("its signed address: %d %q, want the file as it is", rec.Code, rec.Body.String())
	}
	if rec := get("/api/v1/subtitles/" + sub.String() + "/file"); rec.Code != http.StatusUnauthorized {
		t.Errorf("the file unsigned, with a session's token: %d, want 401", rec.Code)
	}
	if d := answer(queued); d.URL != "" || len(d.Subtitles) != 0 {
		t.Errorf("a conversion still queued: %q %+v, want no address and no subtitles yet", d.URL, d.Subtitles)
	}
}
