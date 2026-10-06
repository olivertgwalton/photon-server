package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	nightIn = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000f1")
	entry   = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000f2")
)

// fakePlaylists keeps Oliver's one playlist, of Heat.
type fakePlaylists struct{ moved int }

func own(profile, playlist uuid.UUID) error {
	if profile != oliver.ID || playlist != nightIn {
		return store.ErrNotFound
	}
	return nil
}

func (fakePlaylists) Playlists(_ context.Context, profile uuid.UUID) ([]store.PlaylistSummary, error) {
	if profile != oliver.ID {
		return nil, nil
	}
	return []store.PlaylistSummary{{ID: nightIn, Name: "Night in", Entries: 1, DurationMS: 10_200_000}}, nil
}

func (fakePlaylists) AddPlaylist(context.Context, uuid.UUID, string, []uuid.UUID) (uuid.UUID, error) {
	return nightIn, nil
}

func (fakePlaylists) PlaylistEntries(_ context.Context, profile, playlist uuid.UUID, offset, _ int) ([]store.PlaylistEntry, int64, error) {
	if err := own(profile, playlist); err != nil {
		return nil, 0, err
	}
	return []store.PlaylistEntry{{ID: entry, Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Heat"}}}, int64(offset + 1), nil
}

func (fakePlaylists) AddToPlaylist(_ context.Context, profile, playlist uuid.UUID, _ []uuid.UUID) error {
	return own(profile, playlist)
}

func (fakePlaylists) RemoveFromPlaylist(_ context.Context, profile, playlist, _ uuid.UUID) error {
	return own(profile, playlist)
}

func (f *fakePlaylists) MovePlaylistEntry(_ context.Context, profile, playlist, _ uuid.UUID, position int) error {
	f.moved = position
	return own(profile, playlist)
}

func (fakePlaylists) RenamePlaylist(_ context.Context, profile, playlist uuid.UUID, _ string) error {
	return own(profile, playlist)
}

func (fakePlaylists) RemovePlaylist(_ context.Context, profile, playlist uuid.UUID) error {
	return own(profile, playlist)
}

func TestAProfileKeepsItsPlaylists(t *testing.T) {
	p := &fakePlaylists{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Playlists: p, Events: &fakeEvents{}})
	base := "/api/v1/playlists/" + nightIn.String()
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
		has                         string
	}{
		{goodToken, http.MethodGet, "/api/v1/playlists", "", http.StatusOK, `"name":"Night in","entries":1,"duration_ms":10200000`},
		{memberToken, http.MethodGet, "/api/v1/playlists", "", http.StatusOK, `"items":[]`},
		{goodToken, http.MethodPost, "/api/v1/playlists", `{"name": "Night in", "item_ids": []}`, http.StatusCreated, nightIn.String()},
		{goodToken, http.MethodPost, "/api/v1/playlists", `{}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodGet, base + "/entries", "", http.StatusOK, `"entry_id":"` + entry.String() + `","id":"` + films.String()},
		{memberToken, http.MethodGet, base + "/entries", "", http.StatusNotFound, ""},
		{goodToken, http.MethodPost, base + "/entries", `{"item_ids": ["` + films.String() + `"]}`, http.StatusNoContent, ""},
		{goodToken, http.MethodPut, base + "/entries/" + entry.String() + "/position", `{"position": 3}`, http.StatusNoContent, ""},
		{goodToken, http.MethodDelete, base + "/entries/" + entry.String(), "", http.StatusNoContent, ""},
		{goodToken, http.MethodPatch, base, `{"name": "Late"}`, http.StatusNoContent, ""},
		{memberToken, http.MethodDelete, base, "", http.StatusNotFound, ""},
		{goodToken, http.MethodDelete, base, "", http.StatusNoContent, ""},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.has) {
			t.Errorf("%s %s: %d %s, want %d with %s", tc.method, tc.target, rec.Code, rec.Body, tc.want, tc.has)
		}
	}
	if p.moved != 3 {
		t.Errorf("moved to %d, want 3", p.moved)
	}
}
