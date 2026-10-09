package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	films = uuid.MustParse("0199b3c0-0000-7000-8000-000000000001")
	shows = uuid.MustParse("0199b3c0-0000-7000-8000-000000000002")
)

type fakeCatalogue struct{}

// LibrariesSeen answers that the profile put Shows first.
func (fakeCatalogue) LibrariesSeen(context.Context, uuid.UUID) ([]*store.SeenLibrary, error) {
	return []*store.SeenLibrary{
		{ID: shows, Name: "Shows", Kind: domain.LibraryShows},
		{ID: films, Name: "Films", Kind: domain.LibraryMovies},
	}, nil
}

func (fakeCatalogue) SetLibraryOrder(_ context.Context, _ uuid.UUID, libs []uuid.UUID) error {
	for _, l := range libs {
		if l != films && l != shows {
			return store.ErrNotFound
		}
	}
	return nil
}

// Wall answers one card titled after the page it was asked for, of 120 in all.
func (fakeCatalogue) Wall(_ context.Context, libs []uuid.UUID, p store.WallPage) ([]store.Card, int64, error) {
	if len(libs) != 1 || libs[0] != films {
		return nil, 0, store.ErrNotFound
	}
	title := string(p.Sort) + " " + string(p.Order) + " " + strconv.Itoa(p.Offset) + "+" + strconv.Itoa(p.Limit)
	if f := p.Filter; f.MinRating > 0 || len(f.Genres) > 0 || len(f.Marks) > 0 || len(f.Years) > 0 {
		title += fmt.Sprintf(" %v %v %v %s>=%v", f.Genres, f.Marks, f.Years, f.RatingSite, f.MinRating)
	}
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: title, ReleaseDate: time.Date(1995, 12, 15, 0, 0, 0, 0, time.UTC)}}, 120, nil
}

func (fakeCatalogue) Letters(_ context.Context, lib, _ uuid.UUID, f store.WallFilter) ([]store.Letter, error) {
	if lib != films {
		return nil, store.ErrNotFound
	}
	if f.StartsWith != "" {
		return []store.Letter{{Letter: f.StartsWith, Count: 1}}, nil
	}
	return []store.Letter{{Letter: "#", Count: 2}, {Letter: "A", Count: 7}}, nil
}

func (fakeCatalogue) Facets(_ context.Context, lib, _ uuid.UUID) (store.Facets, error) {
	if lib != films {
		return store.Facets{}, store.ErrNotFound
	}
	return store.Facets{Genres: []string{"Crime"}}, nil
}

func (fakeCatalogue) Similar(_ context.Context, _, id uuid.UUID) ([]store.Card, error) {
	if id != films {
		return nil, store.ErrNotFound
	}
	return []store.Card{{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "Thief"}}, nil
}

// Next has an episode after films and nothing after anything else.
func (fakeCatalogue) Next(_ context.Context, _, id uuid.UUID) (store.Card, error) {
	if id != films {
		return store.Card{}, store.ErrNoNext
	}
	return store.Card{ID: uuid.NewV7(), Kind: domain.ItemEpisode, Title: "The Target"}, nil
}

func (fakeCatalogue) Title(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	if id != films {
		return store.TitlePage{}, store.ErrNotFound
	}
	return store.TitlePage{ID: id, Kind: domain.ItemMovie, Title: "Heat"}, nil
}

// Search finds one title, on the first page alone, titled after what it was asked.
func (fakeCatalogue) Search(_ context.Context, q store.SearchQuery) ([]store.Card, int64, error) {
	if q.Offset > 0 {
		return []store.Card{}, 1, nil
	}
	title := q.Text + " " + q.Library.String()
	if len(q.Kinds) > 0 {
		title += fmt.Sprint(" ", q.Kinds)
	}
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: title}}, 1, nil
}

func (fakeCatalogue) Home(_ context.Context, profile uuid.UUID, limit int) ([]store.HomeRow, error) {
	if profile != oliver.ID {
		return nil, nil
	}
	cards := make([]store.Card, limit)
	for i := range cards {
		cards[i] = store.Card{ID: films, Kind: domain.ItemEpisode, Title: "Pilot", Show: &store.TitleRef{ID: films, Title: "The Wire"}}
	}
	return []store.HomeRow{
		{Kind: domain.RowNextUp, Cards: cards},
		{Kind: domain.RowRecentFilms, Library: &store.LibraryRef{ID: films, Name: "Films"}, Cards: []store.Card{{ID: films, Kind: domain.ItemMovie, Title: "Heat"}}},
	}, nil
}

// The watchlist holds the one film, from offset 0.
func (fakeCatalogue) RowPage(_ context.Context, _ uuid.UUID, row domain.HomeRow, offset, _ int) ([]store.Card, int64, error) {
	if row != domain.RowWatchlist {
		return nil, 0, store.ErrNotFound
	}
	if offset > 0 {
		return []store.Card{}, 1, nil
	}
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: "Heat"}}, 1, nil
}

func TestHome(t *testing.T) {
	rec := serve(t, http.MethodGet, "/api/v1/home?limit=2", goodToken, "")
	var got struct {
		Rows []struct {
			Kind    string `json:"kind"`
			Title   string `json:"title"`
			Library struct {
				Name string `json:"name"`
			} `json:"library"`
			Items []struct {
				Title string `json:"title"`
				Show  struct {
					Title string `json:"title"`
				} `json:"show"`
			} `json:"items"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 || got.Rows[0].Kind != "next_up" || len(got.Rows[0].Items) != 2 || got.Rows[0].Items[0].Show.Title != "The Wire" ||
		got.Rows[1].Kind != "recently_added_films" || got.Rows[1].Library.Name != "Films" {
		t.Errorf("home = %+v, want the signed-in profile's next up, two episodes of The Wire, then Films' recently added", got)
	}
	// Each row is headed as the server words it, a library's by the library.
	if got.Rows[0].Title != "Next Up" || got.Rows[1].Title != "Recently Added in Films" {
		t.Errorf("headings %q and %q", got.Rows[0].Title, got.Rows[1].Title)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/home?limit=0", goodToken, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("limit=0: %d, want 400", rec.Code)
	}
}

func TestHomeRow(t *testing.T) {
	for _, tc := range []struct {
		target   string
		want     int
		wantBody string
	}{
		{"/api/v1/home/watchlist", http.StatusOK, `"offset":0,"total":1}`},
		{"/api/v1/home/watchlist?offset=1", http.StatusOK, `{"items":[],"offset":1,"total":1}`},
		{"/api/v1/home/recently_added_films", http.StatusNotFound, ""},
		{"/api/v1/home/watchlist?limit=0", http.StatusBadRequest, ""},
	} {
		rec := serve(t, http.MethodGet, tc.target, goodToken, "")
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Errorf("%s: %d %s, want %d %s", tc.target, rec.Code, rec.Body, tc.want, tc.wantBody)
		}
	}
}

func TestSearch(t *testing.T) {
	for _, tc := range []struct {
		query      string
		wantStatus int
		wantTitle  string
		wantPeople int
	}{
		{"?q=heat", http.StatusOK, "heat 00000000-0000-0000-0000-000000000000", 0},
		{"?q=sigourney", http.StatusOK, "sigourney 00000000-0000-0000-0000-000000000000", 1},
		{"?q=heat&library=" + films.String(), http.StatusOK, "heat " + films.String(), 0},
		{"?q=sigourney&kind=movie", http.StatusOK, "sigourney 00000000-0000-0000-0000-000000000000 [movie]", 0},
		{"?q=sigourney&kind=show,episode&kind=person", http.StatusOK, "sigourney 00000000-0000-0000-0000-000000000000 [show episode]", 1},
		{"?q=sigourney&kind=person", http.StatusOK, "", 1},
		{"", http.StatusBadRequest, "", 0},
		{"?q=heat&library=films", http.StatusBadRequest, "", 0},
		{"?q=heat&kind=film", http.StatusBadRequest, "", 0},
		{"?q=heat&offset=-1", http.StatusBadRequest, "", 0},
		{"?q=heat&limit=0", http.StatusBadRequest, "", 0},
	} {
		rec := serve(t, http.MethodGet, "/api/v1/search"+tc.query, goodToken, "")
		if rec.Code != tc.wantStatus {
			t.Errorf("%q: status = %d, want %d", tc.query, rec.Code, tc.wantStatus)
			continue
		}
		if tc.wantStatus != http.StatusOK {
			continue
		}
		var got searchJSON
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		titles := got.Titles.Items
		if tc.wantTitle == "" && len(titles) != 0 || tc.wantTitle != "" && (len(titles) != 1 || titles[0].Title != tc.wantTitle) {
			t.Errorf("%q: titles = %+v, want %q", tc.query, titles, tc.wantTitle)
		}
		if len(got.People.Items) != tc.wantPeople {
			t.Errorf("%q: people = %+v, want %d", tc.query, got.People.Items, tc.wantPeople)
		}
	}
	var next searchJSON
	if err := json.NewDecoder(serve(t, http.MethodGet, "/api/v1/search?q=sigourney&offset=1&limit=1", goodToken, "").Body).Decode(&next); err != nil {
		t.Fatal(err)
	}
	if next.Titles.Offset != 1 || next.Titles.Total != 1 || next.People.Offset != 1 || next.People.Total != 1 || len(next.Titles.Items) != 0 {
		t.Errorf("the second page = %+v, want it empty, counting the one title and one person", next)
	}
}

func TestWall(t *testing.T) {
	for _, tc := range []struct {
		query      string
		wantStatus int
		wantTitle  string
	}{
		{"", http.StatusOK, "title asc 0+50"},
		{"?sort=added", http.StatusOK, "added desc 0+50"},
		{"?sort=released", http.StatusOK, "released desc 0+50"},
		{"?sort=added&order=asc&limit=200&offset=60", http.StatusOK, "added asc 60+200"},
		{"?sort=popularity", http.StatusBadRequest, ""},
		{"?order=up", http.StatusBadRequest, ""},
		{"?limit=201", http.StatusBadRequest, ""},
		{"?offset=-1", http.StatusBadRequest, ""},
		{"?after=cursor", http.StatusBadRequest, ""},
		{"?genre=Drama,Comedy&genre=War&mark=unwatched&year=1999&min_rating=70", http.StatusOK, "title asc 0+50 [Drama Comedy War] [unwatched] [1999] imdb>=70"},
		{"?mark=seen", http.StatusBadRequest, ""},
		{"?resolution=8k", http.StatusBadRequest, ""},
		{"?min_rating=101", http.StatusBadRequest, ""},
		{"?starts_with=ab", http.StatusBadRequest, ""},
		{"?sort=rating&rating_site=tmdb", http.StatusOK, "rating desc 0+50"},
	} {
		rec := serve(t, http.MethodGet, "/api/v1/libraries/"+films.String()+"/titles"+tc.query, goodToken, "")
		if rec.Code != tc.wantStatus {
			t.Errorf("%q: status = %d, want %d", tc.query, rec.Code, tc.wantStatus)
			continue
		}
		if tc.wantStatus != http.StatusOK {
			continue
		}
		var got struct {
			Items []struct {
				Title       string `json:"title"`
				ReleaseDate string `json:"release_date"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 1 || got.Items[0].Title != tc.wantTitle || got.Items[0].ReleaseDate != "1995-12-15" || got.Total != 120 {
			t.Errorf("%q: body = %+v, want one card %q released 1995-12-15 of 120", tc.query, got, tc.wantTitle)
		}
	}
	var listed struct{ Items []struct{ Name string } }
	if err := json.Unmarshal(serve(t, http.MethodGet, "/api/v1/libraries", goodToken, "").Body.Bytes(), &listed); err != nil ||
		len(listed.Items) != 2 || listed.Items[0].Name != "Shows" {
		t.Errorf("libraries = %+v, %v; want Shows first, as the profile put it", listed.Items, err)
	}
	for body, want := range map[string]int{
		`{"library_ids":["` + films.String() + `","` + shows.String() + `"]}`: http.StatusNoContent,
		`{"library_ids":["` + uuid.NewV7().String() + `"]}`:                   http.StatusNotFound,
		`{"library_ids":"films"}`:                                             http.StatusBadRequest,
	} {
		if rec := serve(t, http.MethodPut, "/api/v1/profile/library-order", goodToken, body); rec.Code != want {
			t.Errorf("ordering %s: status = %d, want %d", body, rec.Code, want)
		}
	}
	if rec := serve(t, http.MethodGet, "/api/v1/libraries/"+uuid.NewV7().String()+"/titles", goodToken, ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown library: status = %d, want 404", rec.Code)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/libraries/"+films.String()+"/letters", goodToken, ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `[{"letter":"#","count":2},{"letter":"A","count":7}]`) {
		t.Errorf("letters: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/libraries/"+films.String()+"/letters?starts_with=m", goodToken, ""); !strings.Contains(rec.Body.String(), `"letter":"M"`) {
		t.Errorf("letters narrowed: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/titles/"+films.String()+"/similar", goodToken, ""); !strings.Contains(rec.Body.String(), `"title":"Thief"`) {
		t.Errorf("similar: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/titles/"+films.String()+"/next", goodToken, ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"title":"The Target"`) {
		t.Errorf("next: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/titles/"+uuid.NewV7().String()+"/next", goodToken, ""); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), `"detail":"no episode follows"`) {
		t.Errorf("nothing next: %d %s, want a 404 saying so", rec.Code, rec.Body)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/libraries/"+films.String()+"/facets", goodToken, ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"genres":["Crime"]`) || !strings.Contains(rec.Body.String(), `"marks":["watched","unwatched","in_progress","favourite","watchlist"]`) {
		t.Errorf("facets: %d %s", rec.Code, rec.Body)
	}
}

// matroskaCatalogue has films as one copy in Matroska.
type matroskaCatalogue struct{ fakeCatalogue }

func (matroskaCatalogue) Title(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	return store.TitlePage{
		ID: id, Kind: domain.ItemMovie, Title: "Heat",
		Versions: []store.VersionPage{{
			ID: id, Container: "matroska,webm", Edition: "Director's Cut",
			Streams: []domain.Stream{
				{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Range: domain.RangeDV},
				{Index: 1, Kind: domain.StreamAudio, Codec: "eac3", Language: language.English, Channels: 6},
			},
			Subtitles: []store.SubtitleRef{{ID: id, Codec: "subrip", Language: "de", Forced: true}},
		}},
	}, nil
}

// A client shows a copy and its tracks by the names the server gives, in its reader's language,
// and the answer says which language that is so no cache serves it to another reader.
func TestATitlesCopiesAndTracksAreNamedForTheReader(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Auth: fakeAuth{}, Catalogue: matroskaCatalogue{}, Preferences: &fakePreferences{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/"+films.String(), nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.9,en;q=0.5")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	var got struct {
		Versions []struct {
			DisplayTitle string `json:"display_title"`
			Streams      []struct {
				DisplayTitle string `json:"display_title"`
			} `json:"streams"`
			Subtitles []struct {
				DisplayTitle string `json:"display_title"`
			} `json:"subtitles"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got.Versions) != 1 {
		t.Fatalf("title page: %d %s", rec.Code, rec.Body)
	}
	v := got.Versions[0]
	if v.DisplayTitle != "Director's Cut (4K HEVC Dolby Vision)" || v.Streams[1].DisplayTitle != "Anglais (Dolby Digital+ 5.1)" ||
		v.Subtitles[0].DisplayTitle != "Allemand Forced (SRT External)" {
		t.Errorf("names %+v", v)
	}
	if rec.Header().Get("Content-Language") != "fr" || !slices.Contains(rec.Header().Values("Vary"), "Accept-Language") {
		t.Errorf("headers %v, want the answer marked French and varying by Accept-Language", rec.Header())
	}
}

func TestACopysContainerIsNamedAsClientsNameIt(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Auth: fakeAuth{}, Catalogue: matroskaCatalogue{}, Preferences: &fakePreferences{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/"+films.String(), nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"container":"mkv"`) {
		t.Errorf("a Matroska copy: %d %s, want its container named mkv", rec.Code, rec.Body)
	}
}

// ratedCatalogue has films rated in the US's system, which is not the server's.
type ratedCatalogue struct{ fakeCatalogue }

func (ratedCatalogue) Title(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	return store.TitlePage{ID: id, Kind: domain.ItemMovie, Title: "Heat", Certificate: "US:R"}, nil
}

func (ratedCatalogue) Wall(context.Context, []uuid.UUID, store.WallPage) ([]store.Card, int64, error) {
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: "Heat", Certificate: "US:R"}}, 1, nil
}

func (ratedCatalogue) Facets(context.Context, uuid.UUID, uuid.UUID) (store.Facets, error) {
	return store.Facets{Certificates: []string{"15", "US:R"}}, nil
}

// A viewer sees a certificate from another country as its country gives it, as Plex shows de/12 as
// 12; an edit and a filter still name its country, so parental controls read it in its system.
func TestACertificateIsShownWithoutItsCountry(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Auth: fakeAuth{}, Catalogue: ratedCatalogue{}, Preferences: &fakePreferences{},
	})
	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if page := get("/api/v1/titles/" + films.String()); !strings.Contains(page, `"certificate":"R"`) ||
		!strings.Contains(page, `"qualified_certificate":"US:R"`) {
		t.Errorf("title page: %s, want R shown and US:R to edit", page)
	}
	if wall := get("/api/v1/libraries/" + films.String() + "/titles"); !strings.Contains(wall, `"certificate":"R"`) {
		t.Errorf("wall: %s, want the card rated R", wall)
	}
	if facets := get("/api/v1/libraries/" + films.String() + "/facets"); !strings.Contains(facets,
		`"certificates":[{"name":"15","value":"15"},{"name":"R","value":"US:R"}]`) {
		t.Errorf("facets: %s, want R shown and US:R filtered by", facets)
	}
}
