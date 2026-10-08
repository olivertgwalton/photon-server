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
	shorts := library("Shorts", "Pilot")
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
	guest, err := st.AddProfile(ctx, "Guest", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccess(ctx, guest.ID, store.ProfileAccess{Libraries: []uuid.UUID{shorts.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{
		Auth: profiles{"pst_ada": ada, "pst_kid": kid, "pst_guest": guest}, Catalogue: st, Preferences: st, Playlists: st,
	})
	type result struct {
		Items []struct {
			ID, Name, Type, CollectionType string
			IsFolder                       bool
			ChildCount                     *int
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

	// Opened, a box set says how many titles it holds, and lists them in its order.
	alien := all.Items[1].ID
	var set struct {
		Name, Type string
		ChildCount *int
	}
	get("pst_ada", "/Users/"+guid(ada.ID)+"/Items/"+alien, &set)
	if set.Type != "BoxSet" || set.ChildCount == nil || *set.ChildCount != 2 {
		t.Errorf("the Alien collection = %+v, want a box set of two", set)
	}
	var members result
	get("pst_ada", "/Items?parentId="+alien+"&fields="+infuseFields, &members)
	if got := names(members); members.TotalRecordCount != 2 || len(got) != 2 || got[0] != "Alien" || got[1] != "Aliens" || members.Items[0].Type != "Movie" {
		t.Errorf("the Alien collection's titles = %v of %d, want Alien then Aliens", got, members.TotalRecordCount)
	}
	var page result
	get("pst_ada", "/Items?parentId="+alien+"&startIndex=1&limit=1", &page)
	if got := names(page); page.TotalRecordCount != 2 || len(got) != 1 || got[0] != "Aliens" {
		t.Errorf("the second of the Alien collection's titles = %v of %d, want Aliens", got, page.TotalRecordCount)
	}
	var none result
	get("pst_kid", "/Items?parentId="+alien, &none)
	if len(none.Items) != 0 {
		t.Errorf("the kid opens a collection of films it may not see: %v, want nothing", names(none))
	}

	// Jellyfin's web app and Streamyfin find collections only as a view, after the libraries, of
	// those of every library the profile sees; one with none has no such view.
	var views result
	get("pst_kid", "/UserViews", &views)
	if len(views.Items) != 2 || views.Items[1].Name != "Collections" || views.Items[1].Type != "CollectionFolder" || views.Items[1].CollectionType != "boxsets" {
		t.Fatalf("the kid's views = %+v, want the documentaries, then the collections", views.Items)
	}
	view := views.Items[1].ID
	var adas result
	get("pst_ada", "/Users/"+guid(ada.ID)+"/Views", &adas)
	if len(adas.Items) != 4 || adas.Items[3].ID != view {
		t.Errorf("Ada's views = %v, want the three libraries and the same view of collections", names(adas))
	}
	var guests result
	get("pst_guest", "/UserViews", &guests)
	if len(guests.Items) != 1 {
		t.Errorf("the views of a profile with no collections = %v, want its library alone", names(guests))
	}
	var opened struct{ ID, Type, CollectionType string }
	get("pst_kid", "/Items/"+view, &opened)
	if opened.ID != view || opened.CollectionType != "boxsets" {
		t.Errorf("the view of collections by id = %+v", opened)
	}
	var inView result
	get("pst_ada", "/Items?parentId="+view+"&includeItemTypes=BoxSet&recursive=true&sortBy=SortName", &inView)
	if got := names(inView); inView.TotalRecordCount != 2 || len(got) != 2 || got[0] != "Racing" {
		t.Errorf("the view of collections holds %v, want every collection", got)
	}
	var kidsView result
	get("pst_kid", "/Items?parentId="+view, &kidsView)
	if got := names(kidsView); len(got) != 1 || got[0] != "Racing" {
		t.Errorf("the kid's view of collections holds %v, want Racing alone", got)
	}
}
