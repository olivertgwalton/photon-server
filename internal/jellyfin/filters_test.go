//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// An app lists the genres, studios, ratings and years a library's titles have, and narrows the
// library to one by the name or the id it was given, as Jellyfin's web app and Findroid do.
func TestAnAppNarrowsALibraryByWhatItsTitlesHave(t *testing.T) {
	ctx := t.Context()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv"); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		title, cert, studio string
		year                int
		genres              []string
	}{
		{"Alien", "18", "Brandywine", 1979, []string{"Horror", "Science Fiction"}},
		{"Brazil", "15", "Embassy", 1985, []string{"Comedy", "Science Fiction"}},
		{"Heat", "IN:A", "Warner Bros.", 1995, []string{"Crime, Thriller"}},
	} {
		film := store.Film{Title: f.title, Folder: f.title, Copies: []store.Copy{{ContentKey: []byte(f.title), Parts: []store.Part{{
			RelPath: f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
		}}}}}
		if _, err := st.SaveFolder(ctx, films.ID, f.title, []byte("v"), []store.Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := st.Wall(ctx, []uuid.UUID{films.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SaveIdentity(ctx, cards[0].ID, domain.SourceTMDB, domain.Metadata{
			Title: f.title, Certificate: f.cert, Year: f.year, Genres: f.genres, Studios: []string{f.studio},
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{Auth: profiles{"pst_ada": ada}, Catalogue: st})
	get := func(target string, into any) {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
	}
	type named struct{ ID, Name, Type string }
	list := func(target string) ([]string, []named) {
		t.Helper()
		var page struct{ Items []named }
		get(target, &page)
		var names []string
		for _, it := range page.Items {
			names = append(names, it.Name)
		}
		return names, page.Items
	}
	lib := "parentId=" + guid(films.ID)

	genres, items := list("/Genres?" + lib + "&sortBy=SortName")
	if !slices.Equal(genres, []string{"Comedy", "Crime, Thriller", "Horror", "Science Fiction"}) || items[0].Type != "Genre" {
		t.Errorf("genres: %v", items)
	}
	if studios, items := list("/Studios"); !slices.Equal(studios, []string{"Brandywine", "Embassy", "Warner Bros."}) || items[0].Type != "Studio" {
		t.Errorf("studios of every library: %v", items)
	}
	if none, _ := list("/Genres?includeItemTypes=Series"); len(none) != 0 {
		t.Errorf("the genres of shows, of which there are none: %v", none)
	}

	var legacy struct {
		Genres, Tags, OfficialRatings []string
		Years                         []int
	}
	get("/Items/Filters?"+lib, &legacy)
	if len(legacy.Genres) != 4 || legacy.Tags == nil || !slices.Equal(legacy.OfficialRatings, []string{"15", "18", "A"}) ||
		!slices.Equal(legacy.Years, []int{1995, 1985, 1979}) {
		t.Errorf("filters: %+v", legacy)
	}
	var filters struct {
		Genres []named
		Tags   []any
	}
	get("/Items/Filters2?"+lib, &filters)
	if len(filters.Genres) != 4 || filters.Tags == nil || filters.Genres[3].Name != "Science Fiction" || filters.Genres[3].ID != items[3].ID {
		t.Errorf("filters2: %+v, want the genres by the ids /Genres gave", filters)
	}

	walled := func(narrowed string) []string {
		t.Helper()
		names, _ := list("/Items?" + lib + "&recursive=true&includeItemTypes=Movie&sortBy=SortName&" + narrowed)
		return names
	}
	for narrowed, want := range map[string][]string{
		"genreIds=" + filters.Genres[3].ID:                    {"Alien", "Brazil"},
		"genres=" + url.QueryEscape("Crime, Thriller|Comedy"): {"Brazil", "Heat"},
		"studios=Embassy": {"Brazil"},
		"studioIds=" + guid(nameID("Studio", "Brandywine")): {"Alien"},
		"officialRatings=A|18":                              {"Alien", "Heat"},
		"genreIds=" + guid(uuid.NewV7()):                    nil,
		"officialRatings=U":                                 nil,
	} {
		if got := walled(narrowed); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", narrowed, got, want)
		}
	}
}
