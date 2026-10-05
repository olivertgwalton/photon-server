package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
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

// Wall answers one card titled after the page it was asked for, and refuses a cursor it did not give.
func (fakeCatalogue) Wall(_ context.Context, lib uuid.UUID, p store.WallPage) ([]store.Card, string, error) {
	if lib != films {
		return nil, "", store.ErrNotFound
	}
	if p.After != "" && p.After != "next" {
		return nil, "", store.ErrBadCursor
	}
	title := string(p.Sort) + " " + string(p.Order) + " " + time.Duration(p.Limit).String()
	return []store.Card{{ID: films, Kind: domain.ItemMovie, Title: title, ReleaseDate: time.Date(1995, 12, 15, 0, 0, 0, 0, time.UTC)}}, "next", nil
}

func TestWall(t *testing.T) {
	for _, tc := range []struct {
		query      string
		wantStatus int
		wantTitle  string
	}{
		{"", http.StatusOK, "title asc 50ns"},
		{"?sort=added", http.StatusOK, "added desc 50ns"},
		{"?sort=added&order=asc&limit=200&after=next", http.StatusOK, "added asc 200ns"},
		{"?sort=rating", http.StatusBadRequest, ""},
		{"?order=up", http.StatusBadRequest, ""},
		{"?limit=201", http.StatusBadRequest, ""},
		{"?after=forged", http.StatusBadRequest, ""},
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
			Next string `json:"next"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 1 || got.Items[0].Title != tc.wantTitle || got.Items[0].ReleaseDate != "1995-12-15" || got.Next != "next" {
			t.Errorf("%q: body = %+v, want one card %q released 1995-12-15 and next", tc.query, got, tc.wantTitle)
		}
	}
	if rec := serve(t, http.MethodGet, "/api/v1/libraries/"+uuid.NewV7().String()+"/titles", goodToken, ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown library: status = %d, want 404", rec.Code)
	}
}
