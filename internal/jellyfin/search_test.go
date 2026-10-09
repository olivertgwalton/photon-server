//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{Copies: noCopies{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st})
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
