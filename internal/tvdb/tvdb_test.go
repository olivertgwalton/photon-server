package tvdb

import (
	"context"
	"encoding/json"
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

// gb is what the tests ask in, as a server set to en-GB asks.
var gb = domain.LocaleOf("en-GB")

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

// fake serves a show, refuses its first token once, and pages its episodes two to a page.
func fake(t *testing.T) (*Client, *int) {
	t.Helper()
	logins := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			var body struct {
				Key string `json:"apikey"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Key != "key" {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			logins++
			_, _ = w.Write([]byte(`{"data":{"token":"t` + string(rune('0'+logins)) + `"}}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer t2" {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path + "?" + r.URL.RawQuery {
		case "/series/79126/extended?meta=translations":
			_, _ = w.Write([]byte(`{"data":{"name":"The Wire","firstAired":"2002-06-02","originalLanguage":"eng",
				"characters":[
					{"name":"Jimmy McNulty","personName":"Dominic West","peopleId":289,"peopleType":"Actor","sort":2,"personImgURL":"https://artworks.thetvdb.com/banners/person/289/a.jpg"},
					{"name":"Cedric Daniels","personName":"Lance Reddick","peopleId":290,"peopleType":"Actor","sort":3,"personImgURL":"https://artworks.thetvdb.com/banners/"},
					{"name":"Himself","personName":"A Host","peopleId":291,"peopleType":"Host","sort":4},
					{"name":"Kima Greggs","personName":" Sonja Sohn ","peopleId":292,"peopleType":"Actor","sort":1}],
				"image":"https://artworks.thetvdb.com/banners/posters/79126-2.jpg",
				"genres":[{"name":"Drama"}],"originalNetwork":{"name":"HBO"},"latestNetwork":{"name":"HBO"},
				"contentRatings":[{"name":"TV-MA","country":"usa"},{"name":"18","country":"gbr"}],
				"remoteIds":[{"id":"tt0306414","sourceName":"IMDB"},{"id":"1438-the-wire","sourceName":"TheMovieDB.com"}],
				"translations":{"nameTranslations":[{"language":"eng","name":"The Wire (2002)","isAlias":true},{"language":"eng","name":"The Wire"}],
				"overviewTranslations":[{"language":"fra","overview":"Baltimore, en français."},{"language":"eng","overview":"Baltimore."}]}}}`))
		case "/series/79126/episodes/default/eng?page=0":
			_, _ = w.Write([]byte(`{"data":{"episodes":[{"id":101,"seasonNumber":1,"number":1,"name":"The Target","aired":"2002-06-02","image":"https://artworks.thetvdb.com/banners/"},
				{"id":201,"seasonNumber":2,"number":1,"name":"Ebb Tide"}]},"links":{"next":"` + srv.URL + `/series/79126/episodes/default/eng?page=1"}}`))
		case "/episodes/101/extended?":
			_, _ = w.Write([]byte(`{"data":{"characters":[
				{"name":"Writer","personName":"David Simon","peopleId":300,"peopleType":"Writer","sort":1},
				{"name":"Bunk","personName":"Wendell Pierce","peopleId":301,"peopleType":"Guest Star","sort":2}]}}`))
		case "/series/79126/episodes/dvd/eng?page=0":
			_, _ = w.Write([]byte(`{"data":{"episodes":[{"seasonNumber":1,"number":1,"name":"The Detail"}]},"links":{"next":null}}`))
		case "/series/79126/episodes/default/eng?page=1":
			_, _ = w.Write([]byte(`{"data":{"episodes":[{"id":102,"seasonNumber":1,"number":2,"name":"The Detail"},
				{"id":601,"seasonNumber":6,"number":1,"name":"Coming","aired":"2100-01-04"}]},"links":{"next":null}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("key", "", unlimited{})
	c.base = srv.URL
	return c, &logins
}

func TestDetailsInTheClientsLanguageAndCountry(t *testing.T) {
	c, logins := fake(t)
	got, err := c.Details(t.Context(), gb, 79126)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Metadata{
		Title: "The Wire", OriginalTitle: "The Wire", Overview: "Baltimore.", Certificate: "18",
		ReleaseDate: time.Date(2002, 6, 2, 0, 0, 0, 0, time.UTC), Year: 2002,
		Genres: []string{"Drama"}, Studios: []string{"HBO"},
		IDs:     map[domain.Provider]string{domain.ProviderTVDB: "79126", domain.ProviderIMDb: "tt0306414", domain.ProviderTMDB: "1438"},
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://artworks.thetvdb.com/banners/posters/79126-2.jpg"}},
		// Billed in TVDB's order, the host left out, and a picture TVDB has none of not given.
		Credits: []domain.Credit{
			{Name: "Sonja Sohn", IDs: map[domain.Provider]string{domain.ProviderTVDB: "292"}, Kind: domain.CreditActor, Role: "Kima Greggs"},
			{Name: "Dominic West", IDs: map[domain.Provider]string{domain.ProviderTVDB: "289"}, Kind: domain.CreditActor, Role: "Jimmy McNulty", Photo: "https://artworks.thetvdb.com/banners/person/289/a.jpg"},
			{Name: "Lance Reddick", IDs: map[domain.Provider]string{domain.ProviderTVDB: "290"}, Kind: domain.CreditActor, Role: "Cedric Daniels"},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Details (-want +got):\n%s", diff)
	}
	if *logins != 2 {
		t.Errorf("signed in %d times, want twice: once, then again when the first token was refused", *logins)
	}
}

func TestSeasonsReadEveryPage(t *testing.T) {
	c, _ := fake(t)
	got, err := c.Seasons(t.Context(), gb, 79126, []int{1}, domain.OrderAired)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for n := 1; n <= 2; n++ {
		titles = append(titles, got[1].Episodes[n].Title)
	}
	if got[1].Episodes[1].Artwork != nil {
		t.Errorf("an episode TVDB has no picture for has %v", got[1].Episodes[1].Artwork)
	}
	if _, ok := got[2]; strings.Join(titles, ", ") != "The Target, The Detail" || ok {
		t.Errorf("seasons = %+v, want season 1 with both pages' episodes, and nothing of season 2", got)
	}
}

func TestAnEpisodeYetToAirIsDescribedThoughItsSeasonIsNotAsked(t *testing.T) {
	c, _ := fake(t)
	got, err := c.Seasons(t.Context(), gb, 79126, []int{1}, domain.OrderAired)
	if err != nil {
		t.Fatal(err)
	}
	coming := got[6].Episodes[1]
	if coming.Title != "Coming" || !coming.ReleaseDate.Equal(time.Date(2100, 1, 4, 0, 0, 0, 0, time.UTC)) || len(got[6].Episodes) != 1 {
		t.Errorf("season 6 = %+v, want its episode yet to air", got[6])
	}
}

func TestSeasonsAreNumberedInTheOrderAsked(t *testing.T) {
	c, _ := fake(t)
	got, err := c.Seasons(t.Context(), gb, 79126, []int{1}, domain.OrderDVD)
	if err != nil || got[1].Episodes[1].Title != "The Detail" {
		t.Errorf("on DVD, season 1 = %+v, %v; want The Detail first", got[1], err)
	}
}

func TestAnEpisodeCreditsItsGuestsAndCrew(t *testing.T) {
	c, _ := fake(t)
	got, err := c.Seasons(t.Context(), gb, 79126, []int{1}, domain.OrderAired)
	if err != nil {
		t.Fatal(err)
	}
	// The guest before the writer TVDB sorts first: the cast is billed before the crew.
	want := []domain.Credit{
		{Name: "Wendell Pierce", IDs: map[domain.Provider]string{domain.ProviderTVDB: "301"}, Kind: domain.CreditGuestStar, Role: "Bunk"},
		{Name: "David Simon", IDs: map[domain.Provider]string{domain.ProviderTVDB: "300"}, Kind: domain.CreditWriter, Role: "Writer"},
	}
	if diff := cmp.Diff(want, got[1].Episodes[1].Credits); diff != "" {
		t.Errorf("the first episode's credits (-want +got):\n%s", diff)
	}
	// TVDB answers the second episode's record as not found: it is credited with no one, and the
	// season is still described.
	if got[1].Episodes[2].Title != "The Detail" || got[1].Episodes[2].Credits != nil {
		t.Errorf("the second episode = %+v, want its title and no credits", got[1].Episodes[2])
	}
}

func TestSearchSaysWhatEachShowIsAboutInTheClientsLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"data":{"token":"t"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"tvdb_id":"79126","name":"The Wire","year":"2002","overview":"Baltimore, as TVDB has it.",
				"overviews":{"fra":"Baltimore, en français.","eng":"Baltimore."}},
			{"tvdb_id":"1","name":"Wired","overview":"Only in its own words."}]}`))
	}))
	t.Cleanup(srv.Close)
	c := New("key", "", unlimited{})
	c.base = srv.URL
	got, err := c.Search(t.Context(), gb, "The Wire", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Overview != "Baltimore." || got[1].Overview != "Only in its own words." {
		t.Errorf("overviews: %+v, want each in English, else as TVDB has it", got)
	}
}

// A provider is found able to do what it does only while its methods are the capabilities' own.
func TestItHasItsCapabilities(t *testing.T) {
	got := provider.Capabilities(New("key", "", unlimited{}))
	if want := []domain.Capability{domain.CapabilityDescribe, domain.CapabilitySearch}; !slices.Equal(got, want) {
		t.Errorf("capabilities %v, want %v", got, want)
	}
}

func TestAShowsPicturesAreRankedByTheLanguageAskedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"data":{"token":"t"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"name":"The Wire","image":"https://artworks.thetvdb.com/banners/p.jpg","artworks":[
			{"type":2,"image":"/en.jpg","language":"eng","score":90,"width":680,"height":1000},
			{"type":2,"image":"/de-low.jpg","language":"deu","score":10},
			{"type":2,"image":"/de.jpg","language":"deu","score":50},
			{"type":2,"image":"/none.jpg","score":99},
			{"type":3,"image":"/bg-en.jpg","language":"eng","score":99},
			{"type":3,"image":"/bg.jpg","score":1},
			{"type":23,"image":"/logo-en.png","language":"eng"},
			{"type":7,"image":"/season.jpg","language":"deu"}]}}`))
	}))
	t.Cleanup(srv.Close)
	c := New("key", "", unlimited{})
	c.base = srv.URL
	got, err := c.Details(t.Context(), domain.LocaleOf("de-DE"), 79126)
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginalTitle != "The Wire" {
		t.Errorf("original title %q, want the show's own name, whatever language it is asked in", got.OriginalTitle)
	}
	var order []string
	for _, a := range got.Artwork {
		order = append(order, string(a.Kind)+" "+strings.TrimPrefix(a.URL, "https://artworks.thetvdb.com")+" "+a.Language)
	}
	want := []string{
		"poster /de.jpg de", "poster /de-low.jpg de", "poster /en.jpg en", "poster /none.jpg ",
		"backdrop /bg.jpg ", "backdrop /bg-en.jpg en",
		"logo /logo-en.png en",
	}
	if !slices.Equal(order, want) {
		t.Errorf("pictures:\n%v\nwant German before English before none, a backdrop with no words first, a season's left out:\n%v", order, want)
	}
}
