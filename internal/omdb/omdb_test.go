package omdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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

// Answers as OMDb gives them, keyed by the query past the key.
var answers = map[string]string{
	"i=tt0073195&plot=full": `{"Title":"Jaws","Year":"1975","Rated":"PG","Released":"20 Jun 1975",
		"Runtime":"124 min","Genre":"Adventure, Thriller","Director":"Steven Spielberg",
		"Plot":"When a killer shark unleashes chaos on a beach community off Cape Cod.",
		"Poster":"https://m.media-amazon.com/images/M/jaws.jpg",
		"Ratings":[{"Source":"Internet Movie Database","Value":"8.1/10"},{"Source":"Rotten Tomatoes","Value":"97%"},
		{"Source":"Metacritic","Value":"87/100"}],
		"Metascore":"87","imdbRating":"8.1","imdbVotes":"673,852","imdbID":"tt0073195","Type":"movie","Response":"True"}`,
	"i=tt0903747&plot=full": `{"Title":"Breaking Bad","Year":"2008–2013","Rated":"TV-MA","Released":"20 Jan 2008",
		"Genre":"Crime, Drama, Thriller","Plot":"A chemistry teacher turns to making meth.","Poster":"N/A",
		"Ratings":[{"Source":"Internet Movie Database","Value":"9.5/10"}],"imdbRating":"9.5","imdbVotes":"2,312,014",
		"imdbID":"tt0903747","Type":"series","totalSeasons":"5","Response":"True"}`,
	"Season=1&i=tt0903747": `{"Title":"Breaking Bad","Season":"1","totalSeasons":"5","Episodes":[
		{"Title":"Pilot","Released":"2008-01-20","Episode":"1","imdbRating":"9.0","imdbID":"tt0959621"},
		{"Title":"Cat's in the Bag...","Released":"2008-01-27","Episode":"2","imdbRating":"8.6","imdbID":"N/A"}],"Response":"True"}`,
	"Season=9&i=tt0903747":  `{"Response":"False","Error":"Series or season not found!"}`,
	"i=tt0959621&plot=full": `{"Title":"Pilot","Year":"2008","Released":"20 Jan 2008","Plot":"Walter White is diagnosed.","Response":"True"}`,
	"i=tt9999999&plot=full": `{"Response":"False","Error":"Incorrect IMDb ID."}`,
}

func client(t *testing.T, key string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("apikey") {
		case "secret":
		case "spent":
			http.Error(w, `{"Response":"False","Error":"Request limit reached!"}`, http.StatusUnauthorized)
			return
		default:
			http.Error(w, `{"Response":"False","Error":"Invalid API key!"}`, http.StatusUnauthorized)
			return
		}
		q.Del("apikey")
		a, ok := answers[q.Encode()]
		if !ok {
			t.Errorf("asked %s", q.Encode())
			a = `{"Response":"False","Error":"Incorrect IMDb ID."}`
		}
		_, _ = w.Write([]byte(a))
	}))
	t.Cleanup(srv.Close)
	c := New(func(context.Context) (map[string]string, error) { return map[string]string{"api_key": key}, nil }, unlimited{})
	c.base = srv.URL + "/"
	return c
}

func TestAFilmIsDescribedByItsIMDbID(t *testing.T) {
	c := client(t, "secret")
	id, err := c.Match(t.Context(), domain.Locale{}, domain.ItemMovie, provider.Hints{Title: "Jaws", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0073195"}})
	if err != nil || id != "tt0073195" {
		t.Fatalf("match = %q, %v", id, err)
	}
	m, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemMovie, id, domain.SeasonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Metadata{
		Title: "Jaws", Overview: "When a killer shark unleashes chaos on a beach community off Cape Cod.", Certificate: "PG",
		ReleaseDate: time.Date(1975, 6, 20, 0, 0, 0, 0, time.UTC), Year: 1975, Genres: []string{"Adventure", "Thriller"},
		IDs:     map[domain.Provider]string{domain.ProviderIMDb: "tt0073195"},
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://m.media-amazon.com/images/M/jaws.jpg"}},
		// Metacritic's is left out.
		Ratings: []domain.Rating{{Site: domain.SiteIMDb, Score: 81, Votes: 673852}, {Site: domain.SiteRottenTomatoes, Score: 97}},
	}
	if diff := cmp.Diff(want, m); diff != "" {
		t.Errorf("metadata (-want +got):\n%s", diff)
	}
}

func TestATitleWithoutAnIMDbIDIsNotMatched(t *testing.T) {
	id, err := client(t, "secret").Match(t.Context(), domain.Locale{}, domain.ItemMovie, provider.Hints{Title: "Jaws", Year: 1975, IDs: map[domain.Provider]string{domain.ProviderTMDB: "578"}})
	if err != nil || id != "" {
		t.Errorf("match = %q, %v; want none", id, err)
	}
}

func TestAShowsEpisodesAreDescribedBySeason(t *testing.T) {
	m, seasons, err := client(t, "secret").Describe(t.Context(), domain.Locale{}, domain.ItemShow, "tt0903747",
		domain.SeasonRequest{Numbers: []int{1, 9}, Order: domain.OrderAired})
	if err != nil {
		t.Fatal(err)
	}
	if m.Year != 2008 || m.Certificate != "TV-MA" || m.Artwork != nil {
		t.Errorf("show = year %d, certificate %q, artwork %v; want 2008, TV-MA and no poster for N/A", m.Year, m.Certificate, m.Artwork)
	}
	want := map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{
		1: {Title: "Pilot", Overview: "Walter White is diagnosed.", ReleaseDate: time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC), Year: 2008},
		2: {Title: "Cat's in the Bag...", ReleaseDate: time.Date(2008, 1, 27, 0, 0, 0, 0, time.UTC), Year: 2008},
	}}}
	if diff := cmp.Diff(want, seasons); diff != "" {
		t.Errorf("seasons (-want +got):\n%s", diff)
	}
	// Numbered otherwise, OMDb's episodes are not the files'.
	_, seasons, err = client(t, "secret").Describe(t.Context(), domain.Locale{}, domain.ItemShow, "tt0903747",
		domain.SeasonRequest{Numbers: []int{1}, Order: domain.OrderDVD})
	if err != nil || len(seasons) != 0 {
		t.Errorf("in DVD order: %v, %v; want no seasons", seasons, err)
	}
}

func TestATitleOMDbDoesNotKnowIsDescribedWithNothing(t *testing.T) {
	m, _, err := client(t, "secret").Describe(t.Context(), domain.Locale{}, domain.ItemMovie, "tt9999999", domain.SeasonRequest{})
	if err != nil || m.Title != "" {
		t.Errorf("unknown title: %+v, %v; want nothing", m, err)
	}
}

func TestAKeyIsNeededAndARefusedOneIsPassedOver(t *testing.T) {
	if _, _, err := client(t, "").Describe(t.Context(), domain.Locale{}, domain.ItemMovie, "tt0073195", domain.SeasonRequest{}); !errors.Is(err, provider.ErrNotConfigured) {
		t.Errorf("with no key: %v, want ErrNotConfigured", err)
	}
	for _, key := range []string{"wrong", "spent"} {
		_, _, err := client(t, key).Describe(t.Context(), domain.Locale{}, domain.ItemMovie, "tt0073195", domain.SeasonRequest{})
		if !errors.Is(err, provider.ErrUnavailable) || strings.Contains(err.Error(), key) {
			t.Errorf("with key %q: %v, want ErrUnavailable not carrying the key", key, err)
		}
	}
}

// A provider is found able to do what it does only while its methods are the capabilities' own.
func TestItHasItsCapabilities(t *testing.T) {
	got := provider.Capabilities(New(func(context.Context) (map[string]string, error) { return nil, nil }, unlimited{}))
	if want := []domain.Capability{domain.CapabilityDescribe}; !slices.Equal(got, want) {
		t.Errorf("capabilities %v, want %v", got, want)
	}
}
