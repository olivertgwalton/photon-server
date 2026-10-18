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
	nightIn   = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000f1")
	entry     = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000f2")
	unwatched = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000f3")
)

// unseenRule finds the episodes Oliver has not watched, as its smart playlist is of.
var unseenRule = store.SmartRule{Kind: domain.WallEpisodes, Filter: store.WallFilter{Marks: []domain.Mark{domain.MarkUnwatched}}}

// fakePlaylists keeps Oliver's two playlists: Night in, of Heat, and a smart one of what it has
// not watched.
type fakePlaylists struct{ moved int }

func own(profile, playlist uuid.UUID) error {
	if profile != oliver.ID || (playlist != nightIn && playlist != unwatched) {
		return store.ErrNotFound
	}
	return nil
}

// byHand is own, refusing a change to the entries of the smart playlist.
func byHand(profile, playlist uuid.UUID) error {
	if err := own(profile, playlist); err != nil {
		return err
	}
	if playlist == unwatched {
		return store.ErrSmartPlaylist
	}
	return nil
}

func (fakePlaylists) Playlists(_ context.Context, profile uuid.UUID) ([]store.PlaylistSummary, error) {
	if profile != oliver.ID {
		return nil, nil
	}
	return []store.PlaylistSummary{
		{ID: nightIn, Name: "Night in", Entries: 1, DurationMS: 10_200_000},
		{ID: unwatched, Name: "Not yet seen", Library: films, Rule: &unseenRule, Entries: 2, DurationMS: 2_400_000},
	}, nil
}

func (fakePlaylists) AddPlaylist(context.Context, uuid.UUID, string, []uuid.UUID) (uuid.UUID, error) {
	return nightIn, nil
}

func (fakePlaylists) AddSmartPlaylist(_ context.Context, profile, lib uuid.UUID, _ string, _ store.SmartRule) (uuid.UUID, error) {
	if profile != oliver.ID || lib != films {
		return uuid.UUID{}, store.ErrNotFound
	}
	return unwatched, nil
}

func (fakePlaylists) SetPlaylistRule(_ context.Context, profile, playlist uuid.UUID, _ store.SmartRule) error {
	if err := own(profile, playlist); err != nil {
		return err
	}
	if playlist == nightIn {
		return store.ErrNotSmartPlaylist
	}
	return nil
}

func (fakePlaylists) PlaylistEntries(_ context.Context, profile, playlist uuid.UUID, offset, _ int) ([]store.PlaylistEntry, int64, error) {
	if err := own(profile, playlist); err != nil {
		return nil, 0, err
	}
	return []store.PlaylistEntry{{ID: entry, Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Heat"}}}, int64(offset + 1), nil
}

func (fakePlaylists) AddToPlaylist(_ context.Context, profile, playlist uuid.UUID, _ []uuid.UUID) error {
	return byHand(profile, playlist)
}

func (fakePlaylists) RemoveFromPlaylist(_ context.Context, profile, playlist, _ uuid.UUID) error {
	return byHand(profile, playlist)
}

func (f *fakePlaylists) MovePlaylistEntry(_ context.Context, profile, playlist, _ uuid.UUID, position int) error {
	f.moved = position
	return byHand(profile, playlist)
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
	smart := "/api/v1/playlists/" + unwatched.String()
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
		has                         string
	}{
		{goodToken, http.MethodGet, "/api/v1/playlists", "", http.StatusOK, `"name":"Night in","entries":1,"duration_ms":10200000`},
		{memberToken, http.MethodGet, "/api/v1/playlists", "", http.StatusOK, `"items":[]`},
		{goodToken, http.MethodPost, "/api/v1/playlists", `{"name": "Night in", "item_ids": []}`, http.StatusCreated, nightIn.String()},
		{goodToken, http.MethodPost, "/api/v1/playlists", `{}`, http.StatusBadRequest, ""},
		// A smart playlist is made of a rule over one library, and plays what the rule finds.
		{
			goodToken, http.MethodPost, "/api/v1/playlists",
			`{"name": "Not yet seen", "library_id": "` + films.String() + `", "rule": {"kind": "episodes", "filter": {"marks": ["unwatched"]}}}`,
			http.StatusCreated, unwatched.String(),
		},
		{
			goodToken, http.MethodPost, "/api/v1/playlists",
			`{"name": "Shows", "library_id": "` + films.String() + `", "rule": {"kind": "shows", "filter": {}}}`,
			http.StatusBadRequest, "films or episodes",
		},
		{
			goodToken, http.MethodPost, "/api/v1/playlists",
			`{"name": "Nowhere", "rule": {"kind": "films", "filter": {}}}`, http.StatusBadRequest, "library_id",
		},
		{
			goodToken, http.MethodPost, "/api/v1/playlists",
			`{"name": "Both", "library_id": "` + films.String() + `", "rule": {"kind": "films", "filter": {}},
				"item_ids": ["` + films.String() + `"]}`,
			http.StatusBadRequest, "item_ids is left out",
		},
		{goodToken, http.MethodGet, "/api/v1/playlists", "", http.StatusOK, `"rule":{"kind":"episodes"`},
		{goodToken, http.MethodPut, smart + "/rule", `{"kind": "films", "filter": {}}`, http.StatusNoContent, ""},
		{goodToken, http.MethodPut, smart + "/rule", `{"kind": "titles", "filter": {}}`, http.StatusBadRequest, "films or episodes"},
		{goodToken, http.MethodPut, base + "/rule", `{"kind": "films", "filter": {}}`, http.StatusConflict, "put in it by hand"},
		// A smart playlist's entries are the rule's, so no client sets them.
		{goodToken, http.MethodPost, smart + "/entries", `{"item_ids": ["` + films.String() + `"]}`, http.StatusConflict, "change the rule"},
		{goodToken, http.MethodDelete, smart + "/entries/" + entry.String(), "", http.StatusConflict, "change the rule"},
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
