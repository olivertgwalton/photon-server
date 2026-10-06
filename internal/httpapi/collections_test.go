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
	alienSet = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e1")
	mySet    = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e2")
)

// fakeCollections has TMDB's Alien set in the films library, and one an admin made.
type fakeCollections struct {
	set    []uuid.UUID
	placed domain.CollectionPlacement
}

func (fakeCollections) Collections(_ context.Context, lib, _ uuid.UUID, offset, _ int) ([]store.Card, int64, error) {
	if lib != films {
		return nil, 0, store.ErrNotFound
	}
	return []store.Card{{ID: alienSet, Kind: domain.ItemCollection, Title: "Alien Collection"}}, int64(offset + 1), nil
}

func (fakeCollections) Members(_ context.Context, _, id uuid.UUID) ([]store.Card, error) {
	if id != alienSet {
		return nil, store.ErrNotFound
	}
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: "Alien"}}, nil
}

func (fakeCollections) AddCollection(_ context.Context, lib uuid.UUID, _ string) (uuid.UUID, error) {
	if lib != films {
		return uuid.UUID{}, store.ErrNotFound
	}
	return mySet, nil
}

func (f *fakeCollections) SetMembers(_ context.Context, id uuid.UUID, items []uuid.UUID) error {
	switch id {
	case alienSet:
		return store.ErrNotUserCollection
	case mySet:
		f.set = items
		return nil
	}
	return store.ErrNotFound
}

func (f *fakeCollections) SetPlacement(_ context.Context, id uuid.UUID, placement domain.CollectionPlacement) error {
	if id != alienSet && id != mySet {
		return store.ErrNotFound
	}
	f.placed = placement
	return nil
}

func (fakeCollections) RemoveCollection(_ context.Context, id uuid.UUID) error {
	switch id {
	case alienSet:
		return store.ErrNotUserCollection
	case mySet:
		return nil
	}
	return store.ErrNotFound
}

func TestCollectionsAreBrowsedAndAnAdminsAreKept(t *testing.T) {
	c := &fakeCollections{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Collections: c})
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
		has                         string
	}{
		{memberToken, http.MethodGet, "/api/v1/libraries/" + films.String() + "/collections?offset=20", "", http.StatusOK, `"title":"Alien Collection"`},
		{memberToken, http.MethodGet, "/api/v1/libraries/" + uuid.NewV7().String() + "/collections", "", http.StatusNotFound, ""},
		{memberToken, http.MethodGet, "/api/v1/titles/" + alienSet.String() + "/members", "", http.StatusOK, `"title":"Alien"`},
		{memberToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Mine"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Mine"}`, http.StatusCreated, mySet.String()},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + mySet.String() + "/members", `{"item_ids": ["` + films.String() + `"]}`, http.StatusNoContent, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + alienSet.String() + "/members", `{"item_ids": []}`, http.StatusConflict, ""},
		{memberToken, http.MethodPut, "/api/v1/admin/collections/" + alienSet.String() + "/placement", `{"placement": "home"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + alienSet.String() + "/placement", `{"placement": "pinned"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + alienSet.String() + "/placement", `{}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + uuid.NewV7().String() + "/placement", `{"placement": "home"}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + alienSet.String() + "/placement", `{"placement": "home"}`, http.StatusNoContent, ""},
		{goodToken, http.MethodDelete, "/api/v1/admin/collections/" + alienSet.String(), "", http.StatusConflict, ""},
		{goodToken, http.MethodDelete, "/api/v1/admin/collections/" + mySet.String(), "", http.StatusNoContent, ""},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.has) {
			t.Errorf("%s %s: %d %s, want %d with %s", tc.method, tc.target, rec.Code, rec.Body, tc.want, tc.has)
		}
	}
	if len(c.set) != 1 || c.set[0] != films {
		t.Errorf("members set: %v", c.set)
	}
	if c.placed != domain.PlacementHome {
		t.Errorf("TMDB's set is placed %q, want home", c.placed)
	}
}
