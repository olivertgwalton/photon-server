package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeLibraries keeps libraries in memory, and the scans asked for.
type fakeLibraries struct {
	libs    []domain.Library
	scanned []uuid.UUID
}

func (f *fakeLibraries) Libraries(context.Context) ([]domain.Library, error) { return f.libs, nil }

func (f *fakeLibraries) Library(_ context.Context, id uuid.UUID) (domain.Library, error) {
	for _, l := range f.libs {
		if l.ID == id {
			return l, nil
		}
	}
	return domain.Library{}, store.ErrNotFound
}

func (f *fakeLibraries) AddLibrary(_ context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error) {
	if slices.ContainsFunc(f.libs, func(l domain.Library) bool { return l.Name == name || l.Root == root }) {
		return domain.Library{}, store.ErrLibraryExists
	}
	l := domain.Library{ID: uuid.NewV7(), Name: name, Kind: kind, Root: root, Sources: domain.DefaultSources(), Monitor: domain.MonitorRealtime}
	f.libs = append(f.libs, l)
	return l, nil
}

func (f *fakeLibraries) SetLibrary(_ context.Context, id uuid.UUID, c store.LibraryChange) error {
	i := slices.IndexFunc(f.libs, func(l domain.Library) bool { return l.ID == id })
	if i < 0 {
		return store.ErrNotFound
	}
	if c.Name != "" {
		f.libs[i].Name = c.Name
	}
	if c.Sources != nil {
		f.libs[i].Sources = c.Sources
	}
	return nil
}

func (f *fakeLibraries) RemoveLibrary(_ context.Context, id uuid.UUID) error {
	n := len(f.libs)
	f.libs = slices.DeleteFunc(f.libs, func(l domain.Library) bool { return l.ID == id })
	if len(f.libs) == n {
		return store.ErrNotFound
	}
	return nil
}

func (f *fakeLibraries) ScanLibrary(_ context.Context, lib uuid.UUID, _ time.Duration) error {
	f.scanned = append(f.scanned, lib)
	return nil
}

func TestAnAdminKeepsTheLibraries(t *testing.T) {
	libs := &fakeLibraries{}
	told := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, Libraries: libs, Events: told})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	root := t.TempDir()
	if rec := do(memberToken, http.MethodGet, "/api/v1/admin/libraries", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member listing: %d, want 403", rec.Code)
	}
	rec := do(goodToken, http.MethodPost, "/api/v1/admin/libraries", `{"name": "Films", "kind": "movies", "root": "`+root+`"}`)
	var added adminLibraryJSON
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil || rec.Code != http.StatusCreated || added.Root != root {
		t.Fatalf("adding: %d %+v %v", rec.Code, added, err)
	}
	if !slices.Equal(libs.scanned, []uuid.UUID{added.ID}) {
		t.Errorf("scanned %v, want the new library at once", libs.scanned)
	}
	for _, tc := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Again", "kind": "movies", "root": "` + root + `"}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Gone", "kind": "movies", "root": "` + root + `/missing"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Relative", "kind": "movies", "root": "films"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Music", "kind": "music", "root": "/"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"sources": ["tmdb", "tmdb"]}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"name": "Movies", "sources": ["tmdb", "nfo"]}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"previews": "sometimes"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"previews": "chapters"}`, http.StatusOK},
		{http.MethodPost, "/api/v1/admin/libraries/" + added.ID.String() + "/scan", "", http.StatusAccepted},
		{http.MethodPost, "/api/v1/admin/libraries/" + uuid.NewV7().String() + "/scan", "", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/admin/libraries/" + added.ID.String(), "", http.StatusNoContent},
		{http.MethodDelete, "/api/v1/admin/libraries/" + added.ID.String(), "", http.StatusNotFound},
	} {
		if rec := do(goodToken, tc.method, tc.target, tc.body); rec.Code != tc.want {
			t.Errorf("%s %s %s: %d, want %d: %s", tc.method, tc.target, tc.body, rec.Code, tc.want, rec.Body)
		}
	}
	if got, want := told.kinds(), []domain.EventKind{domain.EventLibraryAdded, domain.EventLibraryRemoved}; !slices.Equal(got, want) {
		t.Errorf("told %v, want %v", got, want)
	}
}
