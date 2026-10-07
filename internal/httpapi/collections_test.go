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

var (
	alienSet = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e1")
	mySet    = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e2")
)

// fakeCollections has TMDB's Alien set in the films library, and one an admin made.
type fakeCollections struct {
	set    []uuid.UUID
	placed domain.CollectionPlacement
	rule   store.SmartRule
	list   store.ListRef
	listed []domain.Listed
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

func (f *fakeCollections) AddSmartCollection(_ context.Context, lib uuid.UUID, _ string, rule store.SmartRule) (uuid.UUID, error) {
	if lib != films {
		return uuid.UUID{}, store.ErrNotFound
	}
	f.rule = rule
	return mySet, nil
}

func (f *fakeCollections) SetRule(_ context.Context, id uuid.UUID, rule store.SmartRule) error {
	if id != mySet {
		return store.ErrNotUserCollection
	}
	f.rule = rule
	return nil
}

// myList is an admin's list collection of a TMDB list.
var myList = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e3")

func (f *fakeCollections) AddListCollection(_ context.Context, lib uuid.UUID, _ string, list store.ListRef, listed []domain.Listed) (uuid.UUID, error) {
	if lib != films {
		return uuid.UUID{}, store.ErrNotFound
	}
	f.list, f.listed = list, listed
	return myList, nil
}

func (f *fakeCollections) SetListMembers(_ context.Context, id uuid.UUID, listed []domain.Listed) error {
	if id != myList {
		return store.ErrNotUserCollection
	}
	f.listed = listed
	return nil
}

func (fakeCollections) CollectionList(_ context.Context, id uuid.UUID) (store.ListRef, error) {
	switch id {
	case myList:
		return store.ListRef{Source: domain.SourceTMDB, ID: "8136"}, nil
	case alienSet, mySet:
		return store.ListRef{}, store.ErrNotUserCollection
	}
	return store.ListRef{}, store.ErrNotFound
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

// listing keeps TMDB's list 8136, of Alien; MDBList cannot be reached, and OMDb keeps no lists.
type listing struct{}

func (listing) All(context.Context) ([]provider.Provider, error) { return nil, nil }

func (listing) Get(context.Context, domain.FieldSource) (provider.Provider, bool, error) {
	return nil, false, nil
}

func (listing) List(_ context.Context, source domain.FieldSource, id string) ([]domain.Listed, error) {
	switch {
	case source == domain.SourceTMDB && id == "8136":
		return []domain.Listed{{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderTMDB: "348"}}}, nil
	case source == domain.SourceTMDB:
		return nil, provider.ErrNotFound
	case source == domain.SourceMDBList:
		return nil, provider.ErrUnreached
	}
	return nil, provider.ErrNoLister
}

func (listing) Forget() {}

func TestCollectionsAreBrowsedAndAnAdminsAreKept(t *testing.T) {
	c := &fakeCollections{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Collections: c, Providers: listing{}})
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
		// A smart collection is a wall's filter and order, the same for everyone.
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Recent comedies",
			"rule": {"filter": {"genres": ["Comedy"], "years": [2024]}, "sort": "added", "limit": 20}}`, http.StatusCreated, mySet.String()},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Unwatched",
			"rule": {"filter": {"marks": ["unwatched"]}}}`, http.StatusBadRequest, "no one's marks"},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Played",
			"rule": {"filter": {}, "sort": "played"}}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Best",
			"rule": {"filter": {"min_rating": 120}}}`, http.StatusBadRequest, "min_rating"},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + mySet.String() + "/rule", `{"filter": {"resolutions": ["4k"]}, "sort": "rating"}`, http.StatusNoContent, ""},
		// A list collection holds a list kept on a provider, read as it is made.
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Alien films",
			"list": {"source": "tmdb", "id": "8136"}}`, http.StatusCreated, myList.String()},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Gone",
			"list": {"source": "tmdb", "id": "1"}}`, http.StatusBadRequest, "cannot be read"},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Out",
			"list": {"source": "tmdb", "id": "../8136"}}`, http.StatusBadRequest, "user/list"},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "None",
			"list": {"source": "omdb", "id": "8136"}}`, http.StatusBadRequest, "keeps no lists"},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Down",
			"list": {"source": "mdblist", "id": "12"}}`, http.StatusBadGateway, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/collections", `{"library_id": "` + films.String() + `", "title": "Both",
			"rule": {"filter": {}}, "list": {"source": "tmdb", "id": "8136"}}`, http.StatusBadRequest, "not both"},
		{goodToken, http.MethodPost, "/api/v1/admin/collections/" + myList.String() + "/sync", "", http.StatusNoContent, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/collections/" + mySet.String() + "/sync", "", http.StatusConflict, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + mySet.String() + "/rule", `{"filter": {}, "sort": "loudest"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/collections/" + alienSet.String() + "/rule", `{"filter": {}}`, http.StatusConflict, ""},
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
	if c.list.ID != "8136" || len(c.listed) != 1 || c.listed[0].IDs[domain.ProviderTMDB] != "348" {
		t.Errorf("list kept: %+v of %+v, want TMDB's 8136 read", c.list, c.listed)
	}
	if c.rule.Sort != domain.SortRating || len(c.rule.Filter.Resolutions) != 1 {
		t.Errorf("rule kept: %+v, want 4K by rating", c.rule)
	}
	if c.placed != domain.PlacementHome {
		t.Errorf("TMDB's set is placed %q, want home", c.placed)
	}
}
