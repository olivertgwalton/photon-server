package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var films = uuid.MustParse("0199b3c0-0000-7000-8000-000000000001")

type fakeCatalogue struct{}

func (fakeCatalogue) Libraries(context.Context) ([]domain.Library, error) {
	return []domain.Library{{ID: films, Name: "Films", Kind: domain.LibraryMovies, Root: "/srv/films"}}, nil
}

// Wall answers one card titled after the page it was asked for, of 120 in all.
func (fakeCatalogue) Wall(_ context.Context, lib uuid.UUID, p store.WallPage) ([]store.Card, int64, error) {
	if lib != films {
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

func (fakeCatalogue) Title(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	if id != films {
		return store.TitlePage{}, store.ErrNotFound
	}
	return store.TitlePage{ID: id, Kind: domain.ItemMovie, Title: "Heat"}, nil
}

// Search answers one card titled after what it was asked.
func (fakeCatalogue) Search(_ context.Context, q store.SearchQuery) ([]store.Card, error) {
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: q.Text + " " + q.Library.String()}}, nil
}

func (fakeCatalogue) Home(_ context.Context, profile uuid.UUID, limit int) ([]store.HomeRow, error) {
	if profile != oliver.ID {
		return nil, nil
	}
	cards := make([]store.Card, limit)
	for i := range cards {
		cards[i] = store.Card{ID: films, Kind: domain.ItemEpisode, Title: "Pilot", Show: &store.TitleRef{ID: films, Title: "The Wire"}}
	}
	return []store.HomeRow{{Kind: domain.RowNextUp, Cards: cards}}, nil
}

func TestHome(t *testing.T) {
	rec := serve(t, http.MethodGet, "/api/v1/home?limit=2", goodToken, "")
	var got struct {
		Rows []struct {
			Kind  string `json:"kind"`
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
	if len(got.Rows) != 1 || got.Rows[0].Kind != "next_up" || len(got.Rows[0].Items) != 2 || got.Rows[0].Items[0].Show.Title != "The Wire" {
		t.Errorf("home = %+v, want the signed-in profile's next up, two episodes of The Wire", got)
	}
	if rec := serve(t, http.MethodGet, "/api/v1/home?limit=0", goodToken, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("limit=0: %d, want 400", rec.Code)
	}
}

func TestSearch(t *testing.T) {
	for _, tc := range []struct {
		query      string
		wantStatus int
		wantTitle  string
	}{
		{"?q=heat", http.StatusOK, "heat 00000000-0000-0000-0000-000000000000"},
		{"?q=sigourney", http.StatusOK, "sigourney 00000000-0000-0000-0000-000000000000"},
		{"?q=heat&library=" + films.String(), http.StatusOK, "heat " + films.String()},
		{"", http.StatusBadRequest, ""},
		{"?q=heat&library=films", http.StatusBadRequest, ""},
	} {
		rec := serve(t, http.MethodGet, "/api/v1/search"+tc.query, goodToken, "")
		if rec.Code != tc.wantStatus {
			t.Errorf("%q: status = %d, want %d", tc.query, rec.Code, tc.wantStatus)
			continue
		}
		if tc.wantStatus != http.StatusOK {
			continue
		}
		var got struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
			People []struct {
				Name string `json:"name"`
			} `json:"people"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 1 || got.Items[0].Title != tc.wantTitle {
			t.Errorf("%q: items = %+v, want %q", tc.query, got.Items, tc.wantTitle)
		}
		if wantPeople := strings.Contains(tc.query, "sigourney"); wantPeople != (len(got.People) == 1) {
			t.Errorf("%q: people = %+v", tc.query, got.People)
		}
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
		{"?sort=rating&rating_site=letterboxd", http.StatusOK, "rating desc 0+50"},
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
	if rec := serve(t, http.MethodGet, "/api/v1/libraries/"+films.String()+"/facets", goodToken, ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"genres":["Crime"]`) || !strings.Contains(rec.Body.String(), `"marks":["watched","unwatched","in_progress","favourite"]`) {
		t.Errorf("facets: %d %s", rec.Code, rec.Body)
	}
}
