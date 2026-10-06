//go:build integration

package store

import (
	stdcmp "cmp"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

func TestAWallIsNarrowedAndSortedAsAskedFor(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Mixed", domain.LibraryMovies, "/srv/mixed")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceTMDB, domain.SourceMDBList)}); err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	copyOf := func(name string, width int, rng domain.Range, d time.Duration) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
			Duration: d, Streams: []domain.Stream{{Kind: domain.StreamVideo, Codec: "hevc", Width: width, Height: width * 9 / 16, Range: rng}},
		}}}}
	}
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		title, cert string
		year        int
		genres      []string
		width       int
		rng         domain.Range
		length      time.Duration
		imdb        float64
	}{
		{"Alien", "18", 1979, []string{"Horror", "Science Fiction"}, 3840, domain.RangeHDR10, 117 * time.Minute, 85},
		{"Brazil", "15", 1985, []string{"Comedy", "Science Fiction"}, 1920, domain.RangeSDR, 142 * time.Minute, 79},
		{"Émile", "PG", 2001, []string{"Drama"}, 720, domain.RangeSDR, 90 * time.Minute, 0},
	} {
		film := Film{Title: f.title, Folder: f.title, Copies: []Copy{copyOf(f.title, f.width, f.rng, f.length)}}
		if _, err := s.SaveFolder(ctx, lib.ID, f.title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		id := cards[0].ID
		ids[f.title] = id
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{
			Certificate: f.cert, Year: f.year, Genres: f.genres, Studios: []string{"Studio " + f.title},
		}, nil); err != nil {
			t.Fatal(err)
		}
		if f.imdb > 0 {
			if err := s.SaveRatings(ctx, id, domain.SourceMDBList, []domain.Rating{{Site: domain.SiteIMDb, Score: f.imdb}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A show of two episodes, the first watched.
	episode := func(n int) Episode {
		return Episode{
			Season: 1, Episodes: []int{n}, Title: "Cosmos", Folder: "Cosmos/Season 1", ByNumber: true,
			Copies: []Copy{copyOf("Cosmos"+string(rune('0'+n)), 1920, domain.RangeSDR, time.Hour)},
		}
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Cosmos/Season 1", []byte("v1"), Show{Title: "Cosmos", Folder: "Cosmos"}, []Episode{episode(1), episode(2)}, nil); err != nil {
		t.Fatal(err)
	}
	episodes, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE kind = 'episode' ORDER BY episode_number`)
	if err != nil || len(episodes) != 2 {
		t.Fatal(episodes, err)
	}
	if err := s.MarkWatched(ctx, oliver.ID, episodes[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	// Alien watched and a favourite, Brazil part way and on the watchlist.
	if err := s.MarkWatched(ctx, oliver.ID, ids["Alien"], nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Favourite(ctx, oliver.ID, ids["Alien"]); err != nil {
		t.Fatal(err)
	}
	if _, err := saveProgress(ctx, s, oliver.ID, ids["Brazil"], 30*time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Watchlist(ctx, oliver.ID, ids["Brazil"]); err != nil {
		t.Fatal(err)
	}

	titles := func(p WallPage) []string {
		t.Helper()
		p.Profile, p.Limit = oliver.ID, 10
		p.Order = stdcmp.Or(p.Order, domain.Ascending)
		p.Sort = stdcmp.Or(p.Sort, domain.SortTitle)
		cards, total, err := s.Wall(ctx, lib.ID, p)
		if err != nil || int(total) != len(cards) {
			t.Fatalf("%+v: %d of %d, %v", p, len(cards), total, err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	for _, tc := range []struct {
		name string
		page WallPage
		want []string
	}{
		{"science fiction", WallPage{Filter: WallFilter{Genres: []string{"Science Fiction", "Western"}}}, []string{"Alien", "Brazil"}},
		{"from 1985 or 2001", WallPage{Filter: WallFilter{Years: []int{1985, 2001}}}, []string{"Brazil", "Émile"}},
		{"rated 15 or PG", WallPage{Filter: WallFilter{Certificates: []string{"15", "PG"}}}, []string{"Brazil", "Émile"}},
		{"by a studio", WallPage{Filter: WallFilter{Studios: []string{"Studio Alien"}}}, []string{"Alien"}},
		{"under E, unaccented", WallPage{Filter: WallFilter{StartsWith: "E"}}, []string{"Émile"}},
		{"watched", WallPage{Filter: WallFilter{Marks: []domain.Mark{domain.MarkWatched}}}, []string{"Alien"}},
		{"unwatched", WallPage{Filter: WallFilter{Marks: []domain.Mark{domain.MarkUnwatched}}}, []string{"Brazil", "Cosmos", "Émile"}},
		{"in progress", WallPage{Filter: WallFilter{Marks: []domain.Mark{domain.MarkInProgress}}}, []string{"Brazil", "Cosmos"}},
		{"favourites", WallPage{Filter: WallFilter{Marks: []domain.Mark{domain.MarkFavourite}}}, []string{"Alien"}},
		{"on the watchlist", WallPage{Filter: WallFilter{Marks: []domain.Mark{domain.MarkWatchlist}}}, []string{"Brazil"}},
		{"in 4K or SD", WallPage{Filter: WallFilter{Resolutions: []domain.Resolution{domain.ResolutionUHD, domain.ResolutionSD}}}, []string{"Alien", "Émile"}},
		{"in 1080p, a show by its episodes", WallPage{Filter: WallFilter{Resolutions: []domain.Resolution{domain.ResolutionFHD}}}, []string{"Brazil", "Cosmos"}},
		{"in HDR10", WallPage{Filter: WallFilter{Ranges: []domain.Range{domain.RangeHDR10}}}, []string{"Alien"}},
		{"IMDb 80 and up", WallPage{Filter: WallFilter{RatingSite: domain.SiteIMDb, MinRating: 80}}, []string{"Alien"}},
		{"science fiction, unwatched", WallPage{Filter: WallFilter{Genres: []string{"Science Fiction"}, Marks: []domain.Mark{domain.MarkUnwatched}}}, []string{"Brazil"}},
		{"best on IMDb, the unrated last", WallPage{Sort: domain.SortRating, RatingSite: domain.SiteIMDb, Order: domain.Descending}, []string{"Alien", "Brazil", "Cosmos", "Émile"}},
		{"longest first, a show last", WallPage{Sort: domain.SortRuntime, Order: domain.Descending}, []string{"Brazil", "Alien", "Émile", "Cosmos"}},
		{"last played first, the unplayed last", WallPage{Sort: domain.SortPlayed, Order: domain.Descending, Filter: WallFilter{Genres: []string{"Science Fiction", "Drama"}}}, []string{"Brazil", "Alien", "Émile"}},
	} {
		if got := titles(tc.page); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}

	facets, err := s.Facets(ctx, lib.ID, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	want := Facets{
		Genres: []string{"Comedy", "Drama", "Horror", "Science Fiction"}, Years: []int{2001, 1985, 1979},
		Certificates: []string{"15", "18", "PG"}, Studios: []string{"Studio Alien", "Studio Brazil", "Studio Émile"},
		Resolutions: []domain.Resolution{domain.ResolutionSD, domain.ResolutionFHD, domain.ResolutionUHD},
		Ranges:      []domain.Range{domain.RangeSDR, domain.RangeHDR10}, RatingSites: []domain.RatingSite{domain.SiteIMDb},
	}
	if diff := cmp.Diff(want, facets); diff != "" {
		t.Errorf("facets (-want +got):\n%s", diff)
	}
	letters, err := s.Letters(ctx, lib.ID, oliver.ID, WallFilter{Marks: []domain.Mark{domain.MarkUnwatched}})
	if err != nil || !slices.Equal(letters, []Letter{{"B", 1}, {"C", 1}, {"E", 1}}) {
		t.Errorf("letters of the unwatched = %v, %v", letters, err)
	}
}
