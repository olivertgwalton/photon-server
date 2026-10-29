package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeHistory answers one play of Heat for whichever profile is asked about, and that there are
// more from the first page.
type fakeHistory struct {
	asked  []uuid.UUID
	before []store.Before
}

var heatPlayed = time.Date(2026, 10, 1, 20, 0, 0, 123456000, time.UTC)

func (f *fakeHistory) History(_ context.Context, profile uuid.UUID, before store.Before, _ int) ([]store.Play, bool, error) {
	f.asked, f.before = append(f.asked, profile), append(f.before, before)
	play := store.Play{ID: films, Profile: profile, Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Heat"}, Method: domain.PlayDirect, StoppedAt: heatPlayed}
	return []store.Play{play}, before == store.Newest, nil
}

// PlayRecords answers a film and an episode on the first page and the film again on the second, as
// though the history were longer than a batch.
func (f *fakeHistory) PlayRecords(_ context.Context, profile uuid.UUID, before store.Before, _ int) ([]store.PlayRecord, bool, error) {
	f.asked, f.before = append(f.asked, profile), append(f.before, before)
	year, season, episode := 1995, 1, 1
	heat := store.PlayRecord{
		ID: films, ProfileName: "Oliver", Kind: domain.ItemMovie, Title: "Heat", Year: &year, Method: domain.PlayDirect,
		StartedAt: heatPlayed.Add(-time.Hour), StoppedAt: heatPlayed, PositionMS: 3_600_000, TMDB: "949", IMDb: "tt0113277",
	}
	if before != store.Newest {
		return []store.PlayRecord{heat}, false, nil
	}
	target := store.PlayRecord{
		ID: shows, ProfileName: "Oliver", Kind: domain.ItemEpisode, Title: "The Target", ShowTitle: "The Wire",
		SeasonNumber: &season, EpisodeNumber: &episode, Method: domain.PlayTranscode,
		StartedAt: heatPlayed.Add(time.Hour), StoppedAt: heatPlayed.Add(2 * time.Hour), PositionMS: 60_000, TMDB: "1438", TVDB: "79126",
	}
	return []store.PlayRecord{target, heat}, true, nil
}

func TestHistoryFileIsEveryPlayAsCSV(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, History: h})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/file", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="photon-history-`) {
		t.Fatalf("%d %v, want a CSV attachment", rec.Code, rec.Header())
	}
	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"started_at", "stopped_at", "profile", "kind", "title", "year", "show_title", "season", "episode", "position_ms", "method", "tmdb_id", "tvdb_id", "imdb_id"},
		{"2026-10-01T21:00:00Z", "2026-10-01T22:00:00Z", "Oliver", "episode", "The Target", "", "The Wire", "1", "1", "60000", "transcode", "1438", "79126", ""},
		{"2026-10-01T19:00:00Z", "2026-10-01T20:00:00Z", "Oliver", "movie", "Heat", "1995", "", "", "", "3600000", "direct", "949", "", "tt0113277"},
		{"2026-10-01T19:00:00Z", "2026-10-01T20:00:00Z", "Oliver", "movie", "Heat", "1995", "", "", "", "3600000", "direct", "949", "", "tt0113277"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	for n := range want {
		if strings.Join(rows[n], ",") != strings.Join(want[n], ",") {
			t.Errorf("row %d = %q, want %q", n, rows[n], want[n])
		}
	}
	if len(h.before) != 2 || h.before[1].ID != films {
		t.Errorf("read from %v, want the newest, then on from the first batch's last play", h.before)
	}
}

func TestHistoryFileIsAProfilesOwnAndAnAdminsWhole(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, History: h})
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{memberToken, "/api/v1/history/file", http.StatusOK},
		{memberToken, "/api/v1/history/file?profile=" + oliver.ID.String(), http.StatusBadRequest},
		{memberToken, "/api/v1/admin/history/file", http.StatusForbidden},
		{memberToken, "/api/v1/admin/history/file?profile=" + oliver.ID.String(), http.StatusForbidden},
		{goodToken, "/api/v1/admin/history/file", http.StatusOK},
		{goodToken, "/api/v1/admin/history/file?profile=" + oliver.ID.String(), http.StatusOK},
		{goodToken, "/api/v1/admin/history/file?profile=oliver", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s as %s: %d %s, want %d", tc.target, tc.token, rec.Code, rec.Body, tc.want)
		}
	}
	// Each file read in two batches: the member's own, everyone's, then Oliver's.
	if len(h.asked) != 6 || h.asked[0] == oliver.ID || h.asked[0] == (uuid.UUID{}) || h.asked[2] != (uuid.UUID{}) || h.asked[4] != oliver.ID {
		t.Errorf("asked about %v, want the member's own, everyone's, then Oliver's", h.asked)
	}
}

func TestHistoryIsAProfilesOwnAndAnAdminsWhole(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, History: h})
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{goodToken, "/api/v1/history", http.StatusOK},
		{goodToken, "/api/v1/admin/history", http.StatusOK},
		{goodToken, "/api/v1/admin/history?profile=" + oliver.ID.String(), http.StatusOK},
		{memberToken, "/api/v1/admin/history", http.StatusForbidden},
		{goodToken, "/api/v1/history?limit=0", http.StatusBadRequest},
		{goodToken, "/api/v1/admin/history?profile=oliver", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || (tc.want == http.StatusOK && !strings.Contains(rec.Body.String(), `"title":{"id":"`+films.String())) {
			t.Errorf("%s: %d %s, want %d", tc.target, rec.Code, rec.Body, tc.want)
		}
	}
	if len(h.asked) != 3 || h.asked[0] != oliver.ID || h.asked[1] != (uuid.UUID{}) || h.asked[2] != oliver.ID {
		t.Errorf("asked about %v, want Oliver's own, everyone's, then Oliver's", h.asked)
	}
}

func TestHistoryIsPagedOnFromThePageBefore(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, History: h})
	get := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	var first struct{ Next string }
	if rec := get("/api/v1/history"); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &first) != nil || first.Next == "" {
		t.Fatalf("first page: %d %s, want a next", rec.Code, rec.Body)
	}
	var second map[string]any
	if rec := get("/api/v1/history?after=" + first.Next); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &second) != nil || second["next"] != nil {
		t.Fatalf("second page: %d %s, want the last, with no next", rec.Code, rec.Body)
	}
	if want := (store.Before{At: heatPlayed, ID: films}); len(h.before) != 2 || h.before[0] != store.Newest || !h.before[1].At.Equal(want.At) || h.before[1].ID != want.ID {
		t.Errorf("asked from %v, want the newest, then from the first page's last play", h.before)
	}
	for _, target := range []string{"/api/v1/history?after=nonsense", "/api/v1/history?offset=50"} {
		if rec := get(target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", target, rec.Code)
		}
	}
}
