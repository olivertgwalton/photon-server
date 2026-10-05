package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

type unlimited struct{}

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

func serve(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.URL.Query().Get("language") != "en-GB" {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path+"?"+r.URL.Query().Encode()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New("token", "en-GB", unlimited{})
	c.base = srv.URL
	return c
}

func TestSearchAsksForTheYearByKind(t *testing.T) {
	c := serve(t, map[string]string{
		"/search/tv?first_air_date_year=2002&include_adult=false&language=en-GB&query=The+Wire": `{"results":[
			{"id":1438,"name":"The Wire","original_name":"The Wire","first_air_date":"2002-06-02"}]}`,
	})
	got, err := c.Search(t.Context(), Show, "The Wire", 2002)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]Match{{ID: 1438, Title: "The Wire", OriginalTitle: "The Wire", Year: 2002}}, got); diff != "" {
		t.Errorf("Search (-want +got):\n%s", diff)
	}
}

func TestDetailsTakeTheCountrysCertificate(t *testing.T) {
	c := serve(t, map[string]string{
		"/movie/348?append_to_response=release_dates%2Cexternal_ids&language=en-GB": `{
			"id":348,"title":"Alien","original_title":"Alien","overview":"In space.","tagline":"Scream.",
			"release_date":"1979-05-25","genres":[{"name":"Horror"}],"production_companies":[{"name":"Brandywine"}],
			"external_ids":{"imdb_id":"tt0078748"},
			"release_dates":{"results":[
				{"iso_3166_1":"US","release_dates":[{"certification":"R"}]},
				{"iso_3166_1":"GB","release_dates":[{"certification":""},{"certification":"18"}]}]}}`,
	})
	got, err := c.Details(t.Context(), Movie, 348)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Metadata{
		Title: "Alien", OriginalTitle: "Alien", Overview: "In space.", Tagline: "Scream.", Certificate: "18",
		ReleaseDate: time.Date(1979, 5, 25, 0, 0, 0, 0, time.UTC), Year: 1979,
		Genres: []string{"Horror"}, Studios: []string{"Brandywine"},
		IDs: map[domain.Provider]string{domain.ProviderTMDB: "348", domain.ProviderIMDb: "tt0078748"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Details (-want +got):\n%s", diff)
	}
}

func TestMissingSeason(t *testing.T) {
	c := serve(t, nil)
	if _, err := c.Season(t.Context(), 1438, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("Season of a season TMDB lacks: err = %v, want ErrNotFound", err)
	}
}
