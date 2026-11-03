//go:build integration

package jellyfin

import (
	"encoding/json/v2"
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

// A Jellyfin app browses a books library as Jellyfin's: its authors as folders, each holding its
// books, an audiobook as an AudioBook it plays and an ebook as a Book it reads, each in its series.
func TestAnAppBrowsesABooksLibrary(t *testing.T) {
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
	books, err := st.AddLibrary(ctx, "Books", domain.LibraryBooks, "/srv/books", "")
	if err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	version := func(rel string, facts domain.Facts) store.Version {
		return store.Version{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &facts}}}
	}
	for _, b := range []store.Book{
		{Title: "Deathly Hallows", Author: "J.K. Rowling", Series: "Harry Potter", SeriesIndex: 7, Folder: "Hallows", Versions: []store.Version{
			version("Hallows/Hallows.mp3", domain.Facts{Duration: 20 * time.Hour, Container: "mp3", Streams: []domain.Stream{{Kind: domain.StreamAudio, Codec: "mp3"}}}),
		}},
		{Title: "Half-Blood Prince", Author: "J.K. Rowling", Series: "Harry Potter", SeriesIndex: 6, Folder: "Prince", Versions: []store.Version{
			version("Prince/Prince.epub", domain.Facts{Container: "epub", Size: 1}),
		}},
	} {
		if _, err := st.SaveBooksFolder(ctx, books.ID, b.Folder, []byte("v"), []store.Book{b}); err != nil {
			t.Fatal(err)
		}
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, Playlists: st})
	type bookItem struct {
		Name           string `json:"Name"`
		ID             string `json:"Id"`
		Type           string `json:"Type"`
		MediaType      string `json:"MediaType"`
		IsFolder       bool   `json:"IsFolder"`
		CollectionType string `json:"CollectionType"`
		SeriesName     string `json:"SeriesName"`
		IndexNumber    *int   `json:"IndexNumber"`
	}
	items := func(target string) []bookItem {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", target, w.Code)
		}
		var got struct{ Items []bookItem }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Items
	}
	views := items("/UserViews")
	if len(views) != 1 || views[0].CollectionType != "books" {
		t.Fatalf("views: %+v, want the books library as Jellyfin's books", views)
	}
	authors := items("/Items?ParentId=" + views[0].ID)
	if len(authors) != 1 || authors[0].Name != "J.K. Rowling" || authors[0].Type != "Folder" || !authors[0].IsFolder {
		t.Fatalf("authors: %+v, want J.K. Rowling's folder", authors)
	}
	got := items("/Items?ParentId=" + authors[0].ID)
	kinds := func(items []bookItem) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Name+" "+it.Type+" "+it.MediaType+" "+it.SeriesName)
		}
		return out
	}
	want := []string{"Half-Blood Prince Book Book Harry Potter", "Deathly Hallows AudioBook Audio Harry Potter"}
	if !slices.Equal(kinds(got), want) {
		t.Errorf("the author's books: %v, want %v", kinds(got), want)
	}
	if wall := items("/Items?ParentId=" + views[0].ID + "&IncludeItemTypes=Book,AudioBook&Recursive=true&SortBy=SortName"); len(wall) != 2 {
		t.Errorf("the library's books: %v, want both", kinds(wall))
	}
}
