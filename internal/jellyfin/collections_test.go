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

// An app lists the household's collections as box sets, of every library or of one, a page at a
// time; a profile sees only those of its own libraries.
func TestAnAppBrowsesCollections(t *testing.T) {
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
	films := map[string]uuid.UUID{}
	library := func(name string, titles ...string) domain.Library {
		t.Helper()
		lib, err := st.AddLibrary(ctx, name, domain.LibraryMovies, "/srv/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range titles {
			film := store.Film{Title: f, Folder: f, Copies: []store.Copy{{ContentKey: []byte(f), Parts: []store.Part{{
				RelPath: f + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
			}}}}}
			if _, err := st.SaveFolder(ctx, lib.ID, f, []byte("v"), []store.Film{film}, nil); err != nil {
				t.Fatal(err)
			}
			cards, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			films[f] = cards[0].ID
		}
		return lib
	}
	collection := func(lib domain.Library, name string, titles ...string) uuid.UUID {
		t.Helper()
		id, err := st.AddCollection(ctx, lib.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		var members []uuid.UUID
		for _, f := range titles {
			members = append(members, films[f])
		}
		if err := st.SetMembers(ctx, id, members); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cinema := library("Films", "Alien", "Aliens", "Heat")
	docs := library("Documentaries", "Senna")
	collection(cinema, "Alien Collection", "Alien", "Aliens")
	collection(docs, "Racing", "Senna")
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccess(ctx, kid.ID, store.ProfileAccess{Libraries: []uuid.UUID{docs.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada, "pst_kid": kid}, Catalogue: st,
	})
	type result struct {
		Items []struct {
			ID, Name, Type string
			IsFolder       bool
			ChildCount     *int
		}
		TotalRecordCount int
	}
	get := func(token, target string, into any) {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="`+token+`"`, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
	}
	names := func(r result) []string {
		var out []string
		for _, it := range r.Items {
			out = append(out, it.Name)
		}
		return out
	}

	var all result
	get("pst_ada", "/Items?includeItemTypes=BoxSet&recursive=true&sortBy=SortName", &all)
	if all.TotalRecordCount != 2 || len(all.Items) != 2 || all.Items[0].Name != "Racing" || all.Items[0].Type != "BoxSet" || !all.Items[0].IsFolder {
		t.Errorf("every collection = %+v, want both, box sets, the documentaries' first as their library comes first", all)
	}
	var second result
	get("pst_ada", "/Items?includeItemTypes=BoxSet&recursive=true&startIndex=1&limit=1", &second)
	if second.TotalRecordCount != 2 || len(second.Items) != 1 || second.Items[0].Name != "Alien Collection" {
		t.Errorf("the second collection of two = %v of %d, want the films'", names(second), second.TotalRecordCount)
	}
	var docsOnly result
	get("pst_ada", "/Items?parentId="+guid(docs.ID)+"&includeItemTypes=BoxSet&recursive=true", &docsOnly)
	if len(docsOnly.Items) != 1 || docsOnly.Items[0].Name != "Racing" {
		t.Errorf("one library's collections = %v, want Racing alone", names(docsOnly))
	}
	var kids result
	get("pst_kid", "/Items?includeItemTypes=BoxSet&recursive=true", &kids)
	if len(kids.Items) != 1 || kids.Items[0].Name != "Racing" {
		t.Errorf("the kid's collections = %v, want those of the documentaries alone", names(kids))
	}
}
