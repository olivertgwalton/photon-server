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
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeEditing has one film, "heat" from 1995, and keeps what is done to it.
type fakeEditing struct {
	edited domain.Metadata
	reset  []domain.Field
	pinned string
	order  domain.EpisodeOrder
	marked []domain.Marker
}

func (f *fakeEditing) EditMetadata(_ context.Context, id uuid.UUID, m domain.Metadata) error {
	if id != films {
		return store.ErrNotFound
	}
	f.edited = m
	return nil
}

func (f *fakeEditing) ResetEdits(_ context.Context, _ uuid.UUID, fields []domain.Field) error {
	f.reset = fields
	return nil
}

func (f *fakeEditing) PinMatch(_ context.Context, id uuid.UUID, p domain.Provider, v string) error {
	if id != films {
		return store.ErrNotFound
	}
	f.pinned = string(p) + "/" + v
	return nil
}

func (f *fakeEditing) SetEpisodeOrder(_ context.Context, id uuid.UUID, order domain.EpisodeOrder) error {
	if id != films {
		return store.ErrNotFound
	}
	f.order = order
	return nil
}

func (f *fakeEditing) SetMarkers(_ context.Context, id uuid.UUID, markers []domain.Marker) error {
	if id != films {
		return store.ErrNotFound
	}
	if len(markers) > 1 && markers[0].Kind == markers[1].Kind {
		return store.ErrMarkerRepeated
	}
	f.marked = markers
	return nil
}

func (f *fakeEditing) IdentifySubject(_ context.Context, id uuid.UUID) (store.Subject, bool, error) {
	return store.Subject{Kind: domain.ItemMovie, Title: "heat", Year: 1995}, id == films, nil
}

// films searches by name and year.
type filmSearch struct{}

func (filmSearch) Info() provider.Info {
	return provider.Info{ID: domain.SourceTMDB, Name: "TMDB", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (filmSearch) Candidates(_ context.Context, _ domain.ItemKind, title string, year int) ([]domain.Candidate, error) {
	return []domain.Candidate{{ID: "949", Title: title + " asked", Year: year, Poster: "https://image.tmdb.org/t/p/original/heat.jpg"}}, nil
}

func TestAnAdminFixesATitle(t *testing.T) {
	e := &fakeEditing{}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, Editing: e, Providers: provider.NewRegistry(nil, filmSearch{})})
	base := "/api/v1/admin/titles/" + films.String()
	copyBase := "/api/v1/admin/versions/" + films.String() + "/markers"
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
		has                         string
	}{
		{memberToken, http.MethodPatch, base, `{"title": "Mine"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPatch, base, `{"title": "Heat", "release_date": "1995-12-15", "locked": ["overview"]}`, http.StatusNoContent, ""},
		{goodToken, http.MethodPatch, base, `{"release_date": "15/12/1995"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPatch, base, `{"locked": ["colour"]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodDelete, base + "/edits?field=title&field=overview", "", http.StatusAccepted, ""},
		{goodToken, http.MethodGet, base + "/candidates?provider=tmdb", "", http.StatusOK, `"id":"949","title":"heat asked","year":1995,"poster"`},
		{goodToken, http.MethodGet, base + "/candidates?provider=tmdb&title=Heat%201986", "", http.StatusOK, `"title":"Heat 1986 asked"`},
		{goodToken, http.MethodGet, base + "/candidates?provider=mdblist", "", http.StatusBadRequest, ""},
		{goodToken, http.MethodGet, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/candidates?provider=tmdb", "", http.StatusNotFound, ""},
		{goodToken, http.MethodPut, base + "/match", `{"provider": "tmdb", "id": "949"}`, http.StatusAccepted, ""},
		{goodToken, http.MethodPut, base + "/match", `{"provider": "netflix", "id": "1"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, base + "/episode-order", `{"order": "dvd"}`, http.StatusAccepted, ""},
		{goodToken, http.MethodPut, base + "/episode-order", `{"order": "production"}`, http.StatusBadRequest, ""},
		{memberToken, http.MethodPut, copyBase, `{"markers": []}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "commercial", "start_ms": 0, "end_ms": 1000}]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "intro", "start_ms": 5000, "end_ms": 5000}]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "intro", "start_ms": 0, "end_ms": 1}, {"kind": "intro", "start_ms": 2, "end_ms": 3}]}`, http.StatusBadRequest, "one marker of each kind"},
		{goodToken, http.MethodPut, "/api/v1/admin/versions/" + uuid.NewV7().String() + "/markers", `{"markers": []}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "intro", "start_ms": 62000, "end_ms": 121500}]}`, http.StatusNoContent, ""},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.has) {
			t.Errorf("%s %s %s: %d %s, want %d with %s", tc.method, tc.target, tc.body, rec.Code, rec.Body, tc.want, tc.has)
		}
	}
	if e.edited.Title != "Heat" || e.edited.ReleaseDate.Year() != 1995 || len(e.edited.Locked) != 1 || len(e.reset) != 2 || e.pinned != "tmdb/949" || e.order != domain.OrderDVD || len(e.marked) != 1 || e.marked[0].EndMS != 121500 {
		t.Errorf("done: %+v %v %q", e.edited, e.reset, e.pinned)
	}
}
