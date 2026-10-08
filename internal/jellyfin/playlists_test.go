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

// playlistsAPI is a household's films, Heat, Alien and Thief, and two profiles, Ada and Bob, each
// signed in by their own token.
func playlistsAPI(t *testing.T) (*API, *store.Store, map[string]uuid.UUID, domain.Profile, domain.Profile) {
	t.Helper()
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
	lib, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	films := map[string]uuid.UUID{}
	for _, f := range []string{"Heat", "Alien", "Thief"} {
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
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.AddProfile(ctx, "Bob", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada, "pst_bob": bob}, Catalogue: st, Playlists: st,
	})
	return api, st, films, ada, bob
}

// playlistResult is a BaseItemDtoQueryResult of playlists.
type playlistResult struct {
	Items []struct {
		ID, Name, Type, MediaType string
		IsFolder                  bool
		ChildCount                *int
	}
	TotalRecordCount int
}

// readAs reads target as the profile token names, into into where it is answered.
func readAs(t *testing.T, api *API, token, target string, into any) int {
	t.Helper()
	w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="`+token+`"`, "")
	if w.Code == http.StatusOK && into != nil {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
	return w.Code
}

// An app lists the profile's playlists, and opens one by its id; no profile sees another's.
func TestAnAppListsItsPlaylists(t *testing.T) {
	api, st, films, ada, _ := playlistsAPI(t)
	night, err := st.AddPlaylist(t.Context(), ada.ID, "Night", []uuid.UUID{films["Heat"], films["Thief"]})
	if err != nil {
		t.Fatal(err)
	}
	var mine playlistResult
	readAs(t, api, "pst_ada", "/Items?includeItemTypes=Playlist&recursive=true&fields=ChildCount", &mine)
	if len(mine.Items) != 1 || mine.TotalRecordCount != 1 {
		t.Fatalf("Ada's playlists = %+v, want Night", mine)
	}
	if p := mine.Items[0]; p.ID != guid(night) || p.Name != "Night" || p.Type != "Playlist" || p.MediaType != "Video" || !p.IsFolder ||
		p.ChildCount == nil || *p.ChildCount != 2 {
		t.Errorf("Night = %+v, want a folder of video, of two", p)
	}
	var opened struct{ Name, Type string }
	if code := readAs(t, api, "pst_ada", "/Items/"+guid(night), &opened); code != http.StatusOK || opened.Name != "Night" || opened.Type != "Playlist" {
		t.Errorf("Night opened = %d %+v", code, opened)
	}
	var bobs playlistResult
	readAs(t, api, "pst_bob", "/Items?includeItemTypes=Playlist&recursive=true", &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("Bob's playlists = %+v, want none: Night is Ada's", bobs)
	}
	if code := readAs(t, api, "pst_bob", "/Items/"+guid(night), nil); code != http.StatusNotFound {
		t.Errorf("Bob opens Ada's playlist: %d, want 404", code)
	}
}
