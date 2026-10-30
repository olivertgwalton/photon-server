package mdblist

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type unlimited struct{}

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

// Jaws as MDBList answers it, IMDb's score among others MDBList scores out of 100, one site it
// has nothing for, sites the server leaves out, and the age Common Sense Media advises it for.
const jaws = `{"title": "Jaws", "commonsense": true, "age_rating": 13, "ratings": [
	{"source": "imdb", "value": 8.1, "score": 81, "votes": 673852},
	{"source": "tomatoes", "value": 97, "score": 97, "votes": 102},
	{"source": "popcorn", "value": 90, "score": 90, "votes": 250000},
	{"source": "metacriticuser", "value": null, "score": null, "votes": null},
	{"source": "rogerebert", "value": 4, "score": 100, "votes": null},
	{"source": "letterboxd", "value": 4, "score": 80, "votes": 876082}
]}`

func client(t *testing.T, key string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "secret" {
			http.Error(w, `{"error": "Invalid API key!"}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/tmdb/movie/578" {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, jaws); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(func(context.Context) (map[string]string, error) { return map[string]string{"api_key": key}, nil }, unlimited{})
	c.api.Base = srv.URL
	return c
}

func TestRatingsAreFoundByTheFirstIDMDBListKnows(t *testing.T) {
	got, err := client(t, "secret").Ratings(t.Context(), domain.ItemMovie, map[domain.Provider]string{
		domain.ProviderIMDb: "tt0000000", domain.ProviderTMDB: "578",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Rated{Ratings: []domain.Rating{
		{Site: domain.SiteIMDb, Score: 81, Votes: 673852},
		{Site: domain.SiteRottenTomatoes, Score: 97, Votes: 102},
		{Site: domain.SiteRottenTomatoesAudience, Score: 90, Votes: 250000},
	}, CommonSenseAge: 13}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ratings (-want +got):\n%s", diff)
	}
}

// MDBList answers an age_rating of its own, read off the title's certificate, where Common Sense
// Media advises none: it is no Common Sense age.
func TestOnlyCommonSenseMediasAgeIsKept(t *testing.T) {
	for _, answer := range []string{
		`{"commonsense": {}, "age_rating": 18}`,
		`{"commonsense": false, "age_rating": 18}`,
		`{"age_rating": 18}`,
		`{"commonsense": true, "age_rating": 0}`,
		`{"commonsense": true, "age_rating": 99}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := io.WriteString(w, answer); err != nil {
				t.Error(err)
			}
		}))
		c := client(t, "secret")
		c.api.Base = srv.URL
		got, err := c.Ratings(t.Context(), domain.ItemMovie, map[domain.Provider]string{domain.ProviderTMDB: "578"})
		srv.Close()
		if err != nil || got.CommonSenseAge != 0 {
			t.Errorf("%s: age %d, %v; want none", answer, got.CommonSenseAge, err)
		}
	}
}

func TestAKeyIsNeededAndNeverLogged(t *testing.T) {
	if _, err := client(t, "").Ratings(t.Context(), domain.ItemMovie, map[domain.Provider]string{domain.ProviderTMDB: "578"}); !errors.Is(err, provider.ErrNotConfigured) {
		t.Errorf("with no key: %v, want ErrNotConfigured", err)
	}
	_, err := client(t, "wrong").Ratings(t.Context(), domain.ItemMovie, map[domain.Provider]string{domain.ProviderTMDB: "578"})
	if err == nil || strings.Contains(err.Error(), "wrong") {
		t.Errorf("with a wrong key: %v, want an error that does not carry the key", err)
	}
	c := New(func(context.Context) (map[string]string, error) { return map[string]string{"api_key": "secret"}, nil }, unlimited{})
	c.api.Base = "http://127.0.0.1:1"
	_, err = c.Ratings(t.Context(), domain.ItemMovie, map[domain.Provider]string{domain.ProviderTMDB: "578"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("unreachable: %v, want an error that does not carry the key", err)
	}
}

// An MDBList list is its films and shows together in its own order, by their TMDB and IMDb ids,
// named by its id or as user/list.
func TestAListIsItsTitlesInRankOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lists/garycrawfordgc/top-horror/items" || r.URL.Query().Get("apikey") != "secret" {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, `{
			"movies": [{"rank": 1, "ids": {"tmdb": 694, "imdb": "tt0081505"}}, {"rank": 3, "ids": {"tmdb": 0, "imdb": "tt0078748"}}],
			"shows": [{"rank": 2, "ids": {"tmdb": 46648, "imdb": "tt2149175"}}]
		}`); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(func(context.Context) (map[string]string, error) { return map[string]string{"api_key": "secret"}, nil }, unlimited{})
	c.api.Base = srv.URL
	got, err := c.List(t.Context(), "garycrawfordgc/top-horror")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Listed{
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderTMDB: "694", domain.ProviderIMDb: "tt0081505"}},
		{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTMDB: "46648", domain.ProviderIMDb: "tt2149175"}},
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0078748"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("listed (-want +got):\n%s", diff)
	}
}

func TestListsAreTheBestLikedUnlessSearched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{
			"/lists/top":           `[{"user_name": "garycrawfordgc", "slug": "netflix-shows", "name": "Netflix Shows", "mediatype": "show", "items": 300}]`,
			"/lists/search horror": `[{"user_name": "hdlists", "slug": "horror", "name": "Horror", "description": "Scary", "mediatype": "movie", "items": 920}, {"user_name": "k0meta", "slug": "pride", "name": "Pride", "mediatype": ""}]`,
		}[strings.TrimSpace(r.URL.Path+" "+r.URL.Query().Get("query"))]
		if body == "" || r.URL.Query().Get("apikey") != "secret" {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(func(context.Context) (map[string]string, error) { return map[string]string{"api_key": "secret"}, nil }, unlimited{})
	c.api.Base = srv.URL
	top, err := c.Lists(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []domain.KeptList{{ID: "garycrawfordgc/netflix-shows", Title: "Netflix Shows", Holds: domain.ItemShow, Size: 300}}; !cmp.Equal(want, top) {
		t.Errorf("top lists %+v", top)
	}
	found, err := c.Lists(t.Context(), "horror")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.KeptList{
		{ID: "hdlists/horror", Title: "Horror", About: "Scary", Holds: domain.ItemMovie, Size: 920},
		{ID: "k0meta/pride", Title: "Pride"},
	}
	if diff := cmp.Diff(want, found); diff != "" {
		t.Errorf("found (-want +got):\n%s", diff)
	}
}
