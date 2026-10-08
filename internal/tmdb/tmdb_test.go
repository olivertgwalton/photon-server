package tmdb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type unlimited struct{}

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

// gb is what the tests ask in, as a server set to en-GB asks.
var gb = domain.LocaleOf("en-GB")

func serve(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	return serveIn(t, "en-GB", routes)
}

func serveIn(t *testing.T, language string, routes map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.URL.Query().Get("language") != language {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.URL.Path+"?"+r.URL.Query().Encode()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(t, w, body)
	}))
	t.Cleanup(srv.Close)
	c := New("token", unlimited{})
	c.api.Base = srv.URL
	return c
}

func TestSearchAsksForTheYearByKind(t *testing.T) {
	c := serve(t, map[string]string{
		"/search/tv?first_air_date_year=2002&include_adult=false&language=en-GB&query=The+Wire": `{"results":[
			{"id":1438,"name":"The Wire","original_name":"The Wire","first_air_date":"2002-06-02","overview":"Baltimore."}]}`,
	})
	got, err := c.Search(t.Context(), gb, Show, "The Wire", 2002)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]domain.Candidate{{ID: "1438", Title: "The Wire", OriginalTitle: "The Wire", Year: 2002, Overview: "Baltimore."}}, got); diff != "" {
		t.Errorf("Search (-want +got):\n%s", diff)
	}
}

func TestDetailsTakeTheCountrysCertificate(t *testing.T) {
	c := serve(t, map[string]string{
		"/movie/348?append_to_response=release_dates%2Cexternal_ids%2Cvideos%2Cimages%2Ccredits&include_image_language=en%2Cnull&include_video_language=en%2Cnull&language=en-GB": `{
			"id":348,"title":"Alien","original_title":"Alien","overview":"In space.","tagline":"Scream.",
			"release_date":"1979-05-25","belongs_to_collection":{"id":8091,"name":"Alien Collection","poster_path":"/set.jpg","backdrop_path":null},"vote_average":8.2,"vote_count":15000,"genres":[{"name":"Horror"}],"production_companies":[{"name":"Brandywine"}],"networks":[{"name":"Brandywine"}],
			"external_ids":{"imdb_id":"tt0078748"},
			"credits":{"cast":[{"id":10205,"name":"Sigourney Weaver","character":"Ripley","profile_path":"/sw.jpg"}],
				"crew":[{"id":578,"name":"Ridley Scott","job":"Director","department":"Directing"},
					{"id":1,"name":"Dan O'Bannon","job":"Screenplay","department":"Writing"},
					{"id":2,"name":"A Gaffer","job":"Gaffer","department":"Lighting"}]},
			"images":{
				"posters":[{"file_path":"/plain.jpg","vote_average":9},{"file_path":"/english.jpg","iso_639_1":"en","width":2000,"height":3000,"vote_average":5}],
				"backdrops":[{"file_path":"/lettered.jpg","iso_639_1":"en","vote_average":9},{"file_path":"/clean.jpg","vote_average":4}]},
			"videos":{"results":[
				{"type":"Trailer","site":"YouTube","key":"fan","name":"Fan cut","official":false,"published_at":"2020-01-01T00:00:00.000Z"},
				{"type":"Opening Credits","site":"YouTube","key":"titles","name":"Titles","official":true,"published_at":"2019-01-01T00:00:00.000Z"},
				{"type":"Trailer","site":"YouTube","key":"studio","name":"Trailer","iso_639_1":"en","official":true,"published_at":"2021-01-01T00:00:00.000Z"}]},
			"release_dates":{"results":[
				{"iso_3166_1":"US","release_dates":[{"certification":"R"}]},
				{"iso_3166_1":"GB","release_dates":[{"certification":""},{"certification":"18"}]}]}}`,
	})
	got, _, err := c.Details(t.Context(), gb, Movie, 348)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Metadata{
		Title: "Alien", OriginalTitle: "Alien", Overview: "In space.", Tagline: "Scream.", Certificate: "18",
		ReleaseDate: time.Date(1979, 5, 25, 0, 0, 0, 0, time.UTC), Year: 1979,
		Genres: []string{"Horror"}, Studios: []string{"Brandywine"},
		IDs:     map[domain.Provider]string{domain.ProviderTMDB: "348", domain.ProviderIMDb: "tt0078748"},
		Ratings: []domain.Rating{{Site: domain.SiteTMDB, Score: 82, Votes: 15000}},
		Credits: []domain.Credit{
			{Name: "Sigourney Weaver", IDs: map[domain.Provider]string{domain.ProviderTMDB: "10205"}, Photo: imageURL + "/sw.jpg", Kind: domain.CreditActor, Role: "Ripley"},
			{Name: "Ridley Scott", IDs: map[domain.Provider]string{domain.ProviderTMDB: "578"}, Kind: domain.CreditDirector, Role: "Director"},
			{Name: "Dan O'Bannon", IDs: map[domain.Provider]string{domain.ProviderTMDB: "1"}, Kind: domain.CreditWriter, Role: "Screenplay"},
		},
		Collections: []domain.Grouping{{ID: "8091", Title: "Alien Collection", Artwork: []domain.Artwork{
			{Kind: domain.ArtworkPoster, URL: imageURL + "/set.jpg"},
		}}},
		Artwork: []domain.Artwork{
			{Kind: domain.ArtworkPoster, URL: imageURL + "/english.jpg", Language: "en", Width: 2000, Height: 3000},
			{Kind: domain.ArtworkPoster, URL: imageURL + "/plain.jpg"},
			{Kind: domain.ArtworkBackdrop, URL: imageURL + "/clean.jpg"},
			{Kind: domain.ArtworkBackdrop, URL: imageURL + "/lettered.jpg", Language: "en"},
		},
		Videos: []domain.RemoteVideo{
			{Kind: domain.ExtraTrailer, Site: "YouTube", Key: "studio", Name: "Trailer", Language: "en", Published: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Kind: domain.ExtraOther, Site: "YouTube", Key: "titles", Name: "Titles", Published: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Kind: domain.ExtraTrailer, Site: "YouTube", Key: "fan", Name: "Fan cut", Published: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Details (-want +got):\n%s", diff)
	}
}

func TestAnotherLanguageStillGetsEnglishPictures(t *testing.T) {
	c := serveIn(t, "de-DE", map[string]string{
		"/movie/348?append_to_response=release_dates%2Cexternal_ids%2Cvideos%2Cimages%2Ccredits&include_image_language=de%2Cnull%2Cen&include_video_language=de%2Cnull&language=de-DE": `{
			"id":348,"title":"Alien",
			"images":{
				"posters":[
					{"file_path":"/plain.jpg","vote_average":9},
					{"file_path":"/english-few.jpg","iso_639_1":"en","vote_average":6,"vote_count":2},
					{"file_path":"/english-many.jpg","iso_639_1":"en","vote_average":6,"vote_count":40},
					{"file_path":"/german.jpg","iso_639_1":"de","vote_average":3}],
				"logos":[
					{"file_path":"/english.png","iso_639_1":"en","vote_average":5},
					{"file_path":"/vector.svg","iso_639_1":"en","vote_average":4}]}}`,
	})
	got, _, err := c.Details(t.Context(), domain.LocaleOf("de-DE"), Movie, 348)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Artwork{
		{Kind: domain.ArtworkPoster, URL: imageURL + "/german.jpg", Language: "de"},
		{Kind: domain.ArtworkPoster, URL: imageURL + "/english-many.jpg", Language: "en"},
		{Kind: domain.ArtworkPoster, URL: imageURL + "/english-few.jpg", Language: "en"},
		{Kind: domain.ArtworkPoster, URL: imageURL + "/plain.jpg"},
		{Kind: domain.ArtworkLogo, URL: imageURL + "/english.png", Language: "en"},
		// A logo uploaded as SVG is fetched as TMDB's PNG of it.
		{Kind: domain.ArtworkLogo, URL: imageURL + "/vector.png", Language: "en"},
	}
	if diff := cmp.Diff(want, got.Artwork); diff != "" {
		t.Errorf("artwork (-want +got):\n%s", diff)
	}
}

func TestMissingSeason(t *testing.T) {
	c := serve(t, nil)
	if _, err := c.Season(t.Context(), gb, 1438, 0); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Season of a season TMDB lacks: err = %v, want ErrNotFound", err)
	}
}

func TestAPersonIsDescribed(t *testing.T) {
	c := serve(t, map[string]string{
		"/person/10205?language=en-GB": `{"name":"Sigourney Weaver","biography":"An actor.","birthday":"1949-10-08","deathday":null,"place_of_birth":"New York City","profile_path":"/sw.jpg"}`,
	})
	got, err := c.Person(t.Context(), gb, "10205")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Person{Name: "Sigourney Weaver", Biography: "An actor.", Born: time.Date(1949, 10, 8, 0, 0, 0, 0, time.UTC), Birthplace: "New York City", Photo: imageURL + "/sw.jpg"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Person (-want +got):\n%s", diff)
	}
}

// A provider answering far more than any title takes is refused rather than read into memory.
func TestAnAnswerTooLargeIsRefused(t *testing.T) {
	c := serve(t, map[string]string{
		"/search/movie?include_adult=false&language=en-GB&query=Alien": `{"results":[],"padding":"` + strings.Repeat("x", 9<<20) + `"}`,
	})
	if got, err := c.Search(t.Context(), gb, Movie, "Alien", 0); err == nil {
		t.Errorf("a 9 MiB answer: %v, want it refused", got)
	}
}

// TMDB asking for a moment, as it does past its rate limit, is given it: the title is still
// identified, not failed into its backoff.
func TestARequestTMDBAsksToSlowIsSentAgain(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if asked++; asked == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		reply(t, w, `{"results":[{"id":348,"title":"Alien","release_date":"1979-05-25"}]}`)
	}))
	t.Cleanup(srv.Close)
	c := New("token", unlimited{})
	c.api.Base = srv.URL
	got, err := c.Search(t.Context(), gb, Movie, "Alien", 0)
	if err != nil || len(got) != 1 || asked != 2 {
		t.Errorf("Search = %v, %v after %d requests; want Alien, asked again once", got, err, asked)
	}
}

func TestAnEpisodeIsRatedByItsVotesAlone(t *testing.T) {
	c := serve(t, map[string]string{
		"/tv/1438/season/1?language=en-GB": `{"name":"Season 1","episodes":[
			{"episode_number":1,"name":"The Target","vote_average":8.1,"vote_count":120},
			{"episode_number":2,"name":"The Detail","vote_average":0,"vote_count":0}]}`,
	})
	got, err := c.Season(t.Context(), gb, 1438, 1)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]domain.Rating{{Site: domain.SiteTMDB, Score: 81, Votes: 120}}, got.Episodes[1].Ratings); diff != "" {
		t.Errorf("a voted episode's ratings (-want +got):\n%s", diff)
	}
	if r := got.Episodes[2].Ratings; r != nil {
		t.Errorf("an episode nobody voted on is rated %+v, want nothing", r)
	}
}

// A provider is found able to do what it does only while its methods are the capabilities' own.
func TestItHasItsCapabilities(t *testing.T) {
	got := provider.Capabilities(New("token", unlimited{}))
	if want := []domain.Capability{domain.CapabilityDescribe, domain.CapabilitySearch, domain.CapabilityPerson}; !slices.Equal(got, want) {
		t.Errorf("capabilities %v, want %v", got, want)
	}
}

func TestAFilmUnratedInTheCountryTakesTheUSsCertificate(t *testing.T) {
	c := serve(t, map[string]string{
		"/movie/348?append_to_response=release_dates%2Cexternal_ids%2Cvideos%2Cimages%2Ccredits&include_image_language=en%2Cnull&include_video_language=en%2Cnull&language=en-GB": `{
			"id":348,"title":"Alien",
			"release_dates":{"results":[
				{"iso_3166_1":"FR","release_dates":[{"certification":"12"}]},
				{"iso_3166_1":"US","release_dates":[{"certification":""},{"certification":"R"}]}]}}`,
	})
	got, _, err := c.Details(t.Context(), gb, Movie, 348)
	if err != nil {
		t.Fatal(err)
	}
	if got.Certificate != "US:R" {
		t.Errorf("certificate %q, want the US's R, written as the US's, as Britain gives none", got.Certificate)
	}
}

func TestALibraryTakingAnyPicturesTakesTheMostLiked(t *testing.T) {
	c := serveIn(t, "de-DE", map[string]string{
		"/movie/348?append_to_response=release_dates%2Cexternal_ids%2Cvideos%2Cimages%2Ccredits&include_image_language=de%2Cnull%2Cen&include_video_language=de%2Cnull&language=de-DE": `{
			"id":348,"title":"Alien",
			"images":{"posters":[
				{"file_path":"/german.jpg","iso_639_1":"de","vote_average":3},
				{"file_path":"/english.jpg","iso_639_1":"en","vote_average":8},
				{"file_path":"/plain.jpg","vote_average":6}]}}`,
	})
	loc := domain.LocaleOf("de-DE")
	loc.Artwork = domain.ArtworkAny
	got, _, err := c.Details(t.Context(), loc, Movie, 348)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, a := range got.Artwork {
		order = append(order, strings.TrimPrefix(a.URL, imageURL))
	}
	if !slices.Equal(order, []string{"/english.jpg", "/plain.jpg", "/german.jpg"}) {
		t.Errorf("posters %v, want the most liked first, whatever their language", order)
	}
}

// A TMDB list is read a page at a time, its films and shows in its order, by their TMDB ids; a
// person on it is no title.
func TestAListIsReadInItsOrder(t *testing.T) {
	pages := map[string]string{
		"1": `{"items":[{"id":348,"media_type":"movie"},{"id":1399,"media_type":"tv"}],"total_pages":2}`,
		"2": `{"items":[{"id":31,"media_type":"person"},{"id":679,"media_type":"movie"}],"total_pages":2}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Query().Get("page")]
		if r.URL.Path != "/list/8136" || r.Header.Get("Authorization") != "Bearer token" || !ok {
			http.NotFound(w, r)
			return
		}
		reply(t, w, body)
	}))
	t.Cleanup(srv.Close)
	c := New("token", unlimited{})
	c.api.Base = srv.URL
	got, err := c.List(t.Context(), "8136")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Listed{
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderTMDB: "348"}},
		{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTMDB: "1399"}},
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderTMDB: "679"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("listed (-want +got):\n%s", diff)
	}
}

func TestAShowStillAiringIsDescribedWithItsComingSeason(t *testing.T) {
	c := serve(t, map[string]string{
		"/tv/95396?append_to_response=content_ratings%2Cexternal_ids%2Cvideos%2Cimages%2Caggregate_credits&include_image_language=en%2Cnull&include_video_language=en%2Cnull&language=en-GB": `{
			"id":95396,"name":"Severance","next_episode_to_air":{"season_number":2,"episode_number":3}}`,
		"/tv/95396/season/2?language=en-GB": `{"episodes":[
			{"episode_number":2,"name":"Goodbye, Mrs. Selvig","air_date":"2025-01-24"},
			{"episode_number":3,"name":"Who Is Alive?","air_date":"2025-01-31"}]}`,
	})
	_, seasons, err := c.Describe(t.Context(), gb, domain.ItemShow, "95396", domain.SeasonRequest{Order: domain.OrderAired})
	if err != nil {
		t.Fatal(err)
	}
	got := seasons[2].Episodes[3]
	if got.Title != "Who Is Alive?" || !got.ReleaseDate.Equal(time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)) || len(seasons) != 1 {
		t.Errorf("seasons = %+v, want the season of the next episode to air, though no file of it was asked about", seasons)
	}
}

func reply(t *testing.T, w io.Writer, body string) {
	t.Helper()
	// A client hangs up on an answer it refuses, as one too large.
	if _, err := io.WriteString(w, body); err != nil && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ECONNRESET) {
		t.Error(err)
	}
}
