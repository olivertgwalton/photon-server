package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

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

func TestAFilmPlaysFromSignedAddresses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Lawrence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Lawrence", "Lawrence cd1.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{
		Auth: fakeAuth{}, Playing: fakePlaying{root: root}, Signer: playback.NewSigner([]byte("key")),
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
		Parts []struct {
			URL      string `json:"url"`
			OffsetMS int64  `json:"offset_ms"`
		} `json:"parts"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Parts) != 2 || got.Parts[1].OffsetMS != 3_600_000 || time.Until(got.ExpiresAt) < 23*time.Hour {
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
