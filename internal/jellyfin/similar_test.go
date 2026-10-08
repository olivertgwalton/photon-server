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

// A film's page offers the films most like it, as photon's own does, as many as the app asks for
// and with the fields it asks for; a title that is not there has none.
func TestAnAppFindsSimilarTitles(t *testing.T) {
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
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		title  string
		genres []string
	}{
		{"Heat", []string{"Crime", "Thriller"}},
		{"Thief", []string{"Crime", "Thriller"}},
		{"Ronin", []string{"Thriller"}},
		{"Amélie", []string{"Comedy"}},
	} {
		film := store.Film{Title: f.title, Folder: f.title, Copies: []store.Copy{{ContentKey: []byte(f.title), Parts: []store.Part{{
			RelPath: f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour, Container: "matroska,webm"},
		}}}}}
		if _, err := st.SaveFolder(ctx, films.ID, f.title, []byte("v"), []store.Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := st.Wall(ctx, []uuid.UUID{films.ID}, store.WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		ids[f.title] = cards[0].ID
		if err := st.SaveIdentity(ctx, cards[0].ID, domain.SourceTMDB, domain.Metadata{Title: f.title, Genres: f.genres}, nil); err != nil {
			t.Fatal(err)
		}
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st})
	type result struct {
		Items []struct {
			Name         string
			MediaSources []any
		}
		TotalRecordCount int
	}
	similar := func(target string) (int, result) {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Swiftfin iOS", Token="pst_ada"`, "")
		var out result
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, out
	}

	_, all := similar("/Items/" + guid(ids["Heat"]) + "/Similar?userId=" + guid(ada.ID))
	if len(all.Items) != 2 || all.Items[0].Name != "Thief" || all.Items[1].Name != "Ronin" || all.TotalRecordCount != 2 {
		t.Errorf("like Heat = %+v, want Thief then Ronin, and not Amélie", all)
	}
	_, one := similar("/movies/" + guid(ids["Heat"]) + "/similar?limit=1&fields=MediaSources")
	if len(one.Items) != 1 || one.Items[0].Name != "Thief" || len(one.Items[0].MediaSources) != 1 {
		t.Errorf("the one most like Heat, with its copies = %+v, want Thief with one", one)
	}
	if code, _ := similar("/Items/" + guid(uuid.NewV7()) + "/Similar"); code != http.StatusNotFound {
		t.Errorf("like a title not there: %d, want 404", code)
	}
}
