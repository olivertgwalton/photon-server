//go:build integration

package store

import (
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestATitleShowsEachSitesRatingFromItsBestSource(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceMDBList, domain.SourceTMDB)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Jaws", []byte("v1"), []Film{{Title: "Jaws", Folder: "Jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	id := oneItem(t, s, "true").ID
	// TMDB's own score, and MDBList's of TMDB and IMDb: MDBList is ranked first in this library.
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Jaws", Overview: "A shark.", Certificate: "12", Genres: []string{"Thriller"}, Ratings: []domain.Rating{
		{Site: domain.SiteTMDB, Score: 76, Votes: 10000},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRatings(ctx, id, domain.SourceMDBList, []domain.Rating{
		{Site: domain.SiteTMDB, Score: 77, Votes: 10114}, {Site: domain.SiteRottenTomatoes, Score: 97}, {Site: domain.SiteIMDb, Score: 81, Votes: 673852},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Rating{{Site: domain.SiteIMDb, Score: 81, Votes: 673852}, {Site: domain.SiteTMDB, Score: 77, Votes: 10114}, {Site: domain.SiteRottenTomatoes, Score: 97}}
	if !slices.Equal(page.Ratings, want) {
		t.Errorf("ratings = %v, want %v", page.Ratings, want)
	}
	// Its card on the wall says the same, with what a showcase writes beside it.
	cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	c := cards[0]
	if len(c.Ratings) != len(want) || c.Ratings[0] != want[0] || c.Overview != "A shark." || c.Certificate != "12" || !slices.Equal(c.Genres, []string{"Thriller"}) {
		t.Errorf("card = %+v, want the page's ratings, overview, certificate and genres", c)
	}
	// Asked again, MDBList has nothing: TMDB's own score stands.
	if err := s.SaveRatings(ctx, id, domain.SourceMDBList, nil); err != nil {
		t.Fatal(err)
	}
	if page, err = s.Title(ctx, uuid.UUID{}, id); err != nil || !slices.Equal(page.Ratings, []domain.Rating{{Site: domain.SiteTMDB, Score: 76, Votes: 10000}}) {
		t.Errorf("after MDBList forgets it: %v, %v; want TMDB's own", page.Ratings, err)
	}
}

func TestAProvidersSettingsAreSetAndCleared(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	if got, err := s.ProviderSettings(ctx, domain.SourceMDBList); err != nil || len(got) != 0 {
		t.Fatalf("before any: %v, %v", got, err)
	}
	if err := s.SetProviderSettings(ctx, domain.SourceMDBList, map[string]string{"api_key": "k1", "other": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProviderSettings(ctx, domain.SourceMDBList, map[string]string{"other": ""}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ProviderSettings(ctx, domain.SourceMDBList); err != nil || len(got) != 1 || got["api_key"] != "k1" {
		t.Errorf("after clearing one: %v, %v; want the key alone", got, err)
	}
}

// Two sources score a film on IMDb, the one its library trusts less higher: the wall sorts and
// filters it, and the home page ranks it, by the score its card shows.
func TestATitleIsSortedAndFilteredByTheRatingItsCardShows(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, _ := homeLibraries(t, s, []string{"Jaws", "Heat"}, nil)
	jaws, heat := oneItem(t, s, `title = 'Jaws'`), oneItem(t, s, `title = 'Heat'`).ID
	lib := jaws.LibraryID
	if err := s.SetLibrary(ctx, lib, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceTMDB, domain.SourceMDBList)}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		id     uuid.UUID
		source domain.FieldSource
		score  float64
	}{{jaws.ID, domain.SourceTMDB, 60}, {jaws.ID, domain.SourceMDBList, 90}, {heat, domain.SourceMDBList, 75}} {
		if err := s.SaveRatings(ctx, r.id, r.source, []domain.Rating{{Site: domain.SiteIMDb, Score: r.score}}); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(p WallPage) (out []string) {
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib}, p)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cards {
			out = append(out, c.Title)
			if c.Title == "Jaws" && (len(c.Ratings) != 1 || c.Ratings[0].Score != 60) {
				t.Errorf("Jaws's card shows %v, want TMDB's 60", c.Ratings)
			}
		}
		return out
	}
	if got, want := titles(WallPage{Sort: domain.SortRating, RatingSite: domain.SiteIMDb, Order: domain.Descending, Limit: 10}), []string{"Heat", "Jaws"}; !slices.Equal(got, want) {
		t.Errorf("best on IMDb = %v, want %v", got, want)
	}
	if got, want := titles(WallPage{Sort: domain.SortTitle, Filter: WallFilter{RatingSite: domain.SiteIMDb, MinRating: 70}, Limit: 10}), []string{"Heat"}; !slices.Equal(got, want) {
		t.Errorf("IMDb 70 and up = %v, want %v", got, want)
	}
	if got, want := homeRow(t, s, admin, domain.RowTopRatedUnwatched), []string{"Heat", "Jaws"}; !slices.Equal(got, want) {
		t.Errorf("top rated = %v, want %v", got, want)
	}
}
