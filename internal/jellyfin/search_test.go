//go:build integration

package jellyfin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// An app searching by hints, as Kodi's JellyCon does, is given the titles matching what was typed,
// a page at a time, of the kinds and library it names.
func TestAnAppSearchesByHints(t *testing.T) {
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
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	copies := func(rel string) []store.Copy {
		return []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}}
	}
	for _, f := range []string{"The Night Of", "Night Moves"} {
		if _, err := st.SaveFolder(ctx, films.ID, f, []byte("v"), []store.Film{{Title: f, Folder: f, Copies: copies(f + ".mkv")}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	ep := store.Episode{Season: 1, Episodes: []int{1}, Title: "Night Shift", Folder: "Wire", ByNumber: true, Copies: copies("S01E01.mkv")}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), store.Show{Title: "The Wire", Folder: "Wire"}, []store.Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{Copies: noCopies{}, Discover: noDiscoveries{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st})
	type hint struct {
		ItemID, ID, Name, Type, MediaType, Series string
		IndexNumber                               int
		Artists                                   []string
	}
	search := func(params string) ([]hint, int) {
		t.Helper()
		w := serve(api, http.MethodGet, "/Search/Hints?"+params, `MediaBrowser Client="JellyCon", Token="pst_ada"`, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", params, w.Code, w.Body)
		}
		var result struct {
			SearchHints      []hint
			TotalRecordCount int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.SearchHints, result.TotalRecordCount
	}

	hints, total := search("searchTerm=night")
	if total != 3 || len(hints) != 3 {
		t.Fatalf("night: %+v of %d, want both films and the episode", hints, total)
	}
	for _, h := range hints {
		if h.ItemID != h.ID || h.MediaType != "Video" || h.Artists == nil {
			t.Errorf("a hint as an app decodes one: %+v", h)
		}
		if h.Type == "Episode" && (h.Series != "The Wire" || h.IndexNumber != 1) {
			t.Errorf("the episode: %+v, want it of The Wire, numbered", h)
		}
	}
	if page, total := search("searchTerm=night&startIndex=1&limit=1"); total != 3 || len(page) != 1 || page[0].ID != hints[1].ID {
		t.Errorf("the second of three: %+v of %d", page, total)
	}
	if films, _ := search("searchTerm=night&includeItemTypes=Movie"); len(films) != 2 {
		t.Errorf("films alone: %+v", films)
	}
	if none, total := search("searchTerm=night&includeItemTypes=Audio"); len(none) != 0 || total != 0 {
		t.Errorf("music, of which there is none: %+v", none)
	}
	if tvOnly, _ := search("searchTerm=night&parentId=" + guid(tv.ID)); len(tvOnly) != 1 || tvOnly[0].Name != "Night Shift" {
		t.Errorf("in the TV library: %+v", tvOnly)
	}
	if w := serve(api, http.MethodGet, "/Search/Hints", `MediaBrowser Token="pst_ada"`, ""); w.Code != http.StatusBadRequest {
		t.Errorf("without a searchTerm: %d, want 400", w.Code)
	}
}

// found finds one film in a remote library a search of every library is shown, and a film alone.
type found struct{ d store.Discovery }

func (f found) Find(_ context.Context, _ uuid.UUID, _ string, kinds []domain.ItemKind) ([]store.Discovery, error) {
	if len(kinds) > 0 && !slices.Contains(kinds, domain.ItemMovie) {
		return nil, nil
	}
	return []store.Discovery{f.d}, nil
}

// An app's search of every library is shown, after what they hold, the films a remote library's
// search finds that it does not hold, as titles an app opens as any other; a page past the first,
// or a search of one library, is not.
func TestAnAppsSearchFindsWhatARemoteLibraryDoesNotHoldYet(t *testing.T) {
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
	copies := []store.Copy{{ContentKey: []byte("n"), Parts: []store.Part{{RelPath: "n.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}}
	if _, err := st.SaveFolder(ctx, films.ID, "N", []byte("v"), []store.Film{{Title: "Night Moves", Folder: "N", Copies: copies}}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	nightOf := store.Discovery{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "The Night Of", Year: 2016}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{Copies: noCopies{}, Discover: found{nightOf}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st})
	items := func(params string) ([]string, int) {
		t.Helper()
		w := serve(api, http.MethodGet, "/Items?recursive=true&"+params, `MediaBrowser Token="pst_ada"`, "")
		var result struct {
			Items []struct {
				ID, Name, Type string
			}
			TotalRecordCount int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, i := range result.Items {
			names = append(names, i.Type+" "+i.Name)
		}
		return names, result.TotalRecordCount
	}
	if got, total := items("searchTerm=night"); !slices.Equal(got, []string{"Movie Night Moves", "Movie The Night Of"}) || total != 2 {
		t.Errorf("night: %q of %d, want Night Moves, then The Night Of found", got, total)
	}
	if got, _ := items("searchTerm=night&startIndex=1"); len(got) != 0 {
		t.Errorf("past the first page: %q, want none found shown again", got)
	}
	if got, _ := items("searchTerm=night&parentId=" + guid(films.ID)); !slices.Equal(got, []string{"Movie Night Moves"}) {
		t.Errorf("in Films: %q, want what it holds alone", got)
	}
	if got, _ := items("searchTerm=night&includeItemTypes=Series"); len(got) != 0 {
		t.Errorf("shows: %q, want no film found", got)
	}
}
