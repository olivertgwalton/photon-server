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
	if diff := cmp.Diff([]domain.Candidate{{ID: "1438", Title: "The Wire", OriginalTitle: "The Wire", Year: 2002}}, got); diff != "" {
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
	got, err := c.Details(t.Context(), Movie, 348)
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

func TestMissingSeason(t *testing.T) {
	c := serve(t, nil)
	if _, err := c.Season(t.Context(), 1438, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("Season of a season TMDB lacks: err = %v, want ErrNotFound", err)
	}
}

func TestAPersonIsDescribed(t *testing.T) {
	c := serve(t, map[string]string{
		"/person/10205?language=en-GB": `{"name":"Sigourney Weaver","biography":"An actor.","birthday":"1949-10-08","deathday":null,"place_of_birth":"New York City","profile_path":"/sw.jpg"}`,
	})
	got, err := c.Person(t.Context(), "10205")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Person{Name: "Sigourney Weaver", Biography: "An actor.", Born: time.Date(1949, 10, 8, 0, 0, 0, 0, time.UTC), Birthplace: "New York City", Photo: imageURL + "/sw.jpg"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Person (-want +got):\n%s", diff)
	}
}
