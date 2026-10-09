//go:build integration

package jellyfin

import (
	"context"
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

// household is a household's films, Heat, Alien and Thief, and two profiles, Ada and Bob, each
// signed in by their own token; and the events the server raised.
type household struct {
	api      *API
	st       *store.Store
	films    map[string]uuid.UUID
	ada, bob domain.Profile
	raised   *[]domain.Event
}

func newHousehold(t *testing.T) household {
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
	h := household{st: st, films: map[string]uuid.UUID{}, raised: &[]domain.Event{}}
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
		h.films[f] = cards[0].ID
	}
	if h.ada, err = st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil); err != nil {
		t.Fatal(err)
	}
	if h.bob, err = st.AddProfile(ctx, "Bob", domain.RoleUser, "hash", nil); err != nil {
		t.Fatal(err)
	}
	h.api = New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{},
		Auth:   profiles{"pst_ada": h.ada, "pst_bob": h.bob}, Catalogue: st, Preferences: st, Playlists: st,
		Raise: func(_ context.Context, e domain.Event) { *h.raised = append(*h.raised, e) },
	})
	return h
}

// send asks target as the profile token names, reading the reply into into where it is answered.
func (h household) send(t *testing.T, method, token, target, body string, into any) int {
	t.Helper()
	w := serve(h.api, method, target, `MediaBrowser Client="Jellyfin Web", Token="`+token+`"`, body)
	if w.Code == http.StatusOK && into != nil {
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("%s %s: %v", method, target, err)
		}
	}
	return w.Code
}

func (h household) read(t *testing.T, token, target string, into any) int {
	t.Helper()
	return h.send(t, http.MethodGet, token, target, "", into)
}

// playlistResult is a BaseItemDtoQueryResult of playlists, or of a playlist's items.
type playlistResult struct {
	Items []struct {
		ID, Name, Type, MediaType, PlaylistItemID string
		IsFolder                                  bool
		ChildCount                                *int
		MediaSources                              []any
	}
	TotalRecordCount int
}

func (r playlistResult) names() []string {
	var out []string
	for _, it := range r.Items {
		out = append(out, it.Name)
	}
	return out
}

// An app lists the profile's playlists, and opens one by its id; no profile sees another's.
func TestAnAppListsItsPlaylists(t *testing.T) {
	h := newHousehold(t)
	night, err := h.st.AddPlaylist(t.Context(), h.ada.ID, "Night", []uuid.UUID{h.films["Heat"], h.films["Thief"]})
	if err != nil {
		t.Fatal(err)
	}
	var mine playlistResult
	h.read(t, "pst_ada", "/Items?includeItemTypes=Playlist&recursive=true&fields=ChildCount", &mine)
	if len(mine.Items) != 1 || mine.TotalRecordCount != 1 {
		t.Fatalf("Ada's playlists = %+v, want Night", mine)
	}
	if p := mine.Items[0]; p.ID != guid(night) || p.Name != "Night" || p.Type != "Playlist" || p.MediaType != "Video" || !p.IsFolder ||
		p.ChildCount == nil || *p.ChildCount != 2 {
		t.Errorf("Night = %+v, want a folder of video, of two", p)
	}
	var opened struct{ Name, Type string }
	if code := h.read(t, "pst_ada", "/Items/"+guid(night), &opened); code != http.StatusOK || opened.Name != "Night" || opened.Type != "Playlist" {
		t.Errorf("Night opened = %d %+v", code, opened)
	}
	var bobs playlistResult
	h.read(t, "pst_bob", "/Items?includeItemTypes=Playlist&recursive=true", &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("Bob's playlists = %+v, want none: Night is Ada's", bobs)
	}
	if code := h.read(t, "pst_bob", "/Items/"+guid(night), nil); code != http.StatusNotFound {
		t.Errorf("Bob opens Ada's playlist: %d, want 404", code)
	}
}

// Jellyfin's web app and Streamyfin find playlists only as a view, after the libraries, of the
// profile's own; a profile with none has no such view.
func TestAnAppFindsPlaylistsInTheirView(t *testing.T) {
	h := newHousehold(t)
	if _, err := h.st.AddPlaylist(t.Context(), h.ada.ID, "Night", []uuid.UUID{h.films["Heat"]}); err != nil {
		t.Fatal(err)
	}
	var views struct {
		Items []struct{ ID, Name, Type, CollectionType string }
	}
	h.read(t, "pst_ada", "/UserViews", &views)
	if len(views.Items) != 2 || views.Items[1].Name != "Playlists" || views.Items[1].Type != "ManualPlaylistsFolder" || views.Items[1].CollectionType != "playlists" {
		t.Fatalf("Ada's views = %+v, want the films, then the playlists", views.Items)
	}
	view := views.Items[1].ID
	var opened struct{ ID, CollectionType string }
	if code := h.read(t, "pst_ada", "/Items/"+view, &opened); code != http.StatusOK || opened.ID != view || opened.CollectionType != "playlists" {
		t.Errorf("the view of playlists by id = %d %+v", code, opened)
	}
	var inView playlistResult
	h.read(t, "pst_ada", "/Items?parentId="+view+"&includeItemTypes=Playlist&recursive=true", &inView)
	if got := inView.names(); len(got) != 1 || got[0] != "Night" {
		t.Errorf("the view of playlists holds %v, want Night", got)
	}
	var bobs struct{ Items []struct{ Name string } }
	h.read(t, "pst_bob", "/Users/"+guid(h.bob.ID)+"/Views", &bobs)
	if len(bobs.Items) != 1 {
		t.Errorf("Bob's views = %+v, want the films alone: he has no playlist", bobs.Items)
	}
	var inBobs playlistResult
	h.read(t, "pst_bob", "/Items?parentId="+view, &inBobs)
	if len(inBobs.Items) != 0 {
		t.Errorf("Bob opens the view of playlists: %v, want none: Night is Ada's", inBobs.names())
	}
}

// An app reads a playlist's items in its order, a page at a time, each with the entry's own id;
// no profile reads another's.
func TestAnAppReadsAPlaylist(t *testing.T) {
	h := newHousehold(t)
	night, err := h.st.AddPlaylist(t.Context(), h.ada.ID, "Night", []uuid.UUID{h.films["Thief"], h.films["Heat"], h.films["Thief"]})
	if err != nil {
		t.Fatal(err)
	}
	var all playlistResult
	h.read(t, "pst_ada", "/Playlists/"+guid(night)+"/Items?userId="+guid(h.ada.ID)+"&fields=MediaSources", &all)
	if all.TotalRecordCount != 3 || len(all.Items) != 3 || all.Items[0].Name != "Thief" || all.Items[1].Name != "Heat" ||
		all.Items[2].ID != all.Items[0].ID || len(all.Items[1].MediaSources) != 1 {
		t.Fatalf("Night = %+v, want Thief, Heat and Thief again, with their copies", all)
	}
	if all.Items[0].PlaylistItemID == "" || all.Items[0].PlaylistItemID == all.Items[2].PlaylistItemID {
		t.Errorf("entries %q and %q: want each its own id, Thief twice told apart", all.Items[0].PlaylistItemID, all.Items[2].PlaylistItemID)
	}
	var second playlistResult
	h.read(t, "pst_ada", "/playlists/"+guid(night)+"/items?startIndex=1&limit=1", &second)
	if second.TotalRecordCount != 3 || len(second.Items) != 1 || second.Items[0].PlaylistItemID != all.Items[1].PlaylistItemID {
		t.Errorf("the second entry of three = %+v, want Heat's", second)
	}
	if code := h.read(t, "pst_bob", "/Playlists/"+guid(night)+"/Items", nil); code != http.StatusNotFound {
		t.Errorf("Bob reads Ada's playlist: %d, want 404", code)
	}
}

// An app makes a playlist of titles and adds more to its end, as the profile's alone; the
// profile's other apps are told of each change. One for another profile, or shared, is refused,
// and no profile adds to another's.
func TestAnAppMakesAPlaylist(t *testing.T) {
	h := newHousehold(t)
	var made struct{ ID string }
	body := `{"Name":"Night","Ids":["` + guid(h.films["Heat"]) + `"],"UserId":"` + guid(h.ada.ID) + `","MediaType":"Video","IsPublic":false}`
	if code := h.send(t, http.MethodPost, "pst_ada", "/Playlists", body, &made); code != http.StatusOK || !hexID.MatchString(made.ID) {
		t.Fatalf("making Night: %d %+v, want its id", code, made)
	}
	if code := h.send(t, http.MethodPost, "pst_ada", "/Playlists/"+made.ID+"/Items?ids="+guid(h.films["Alien"])+","+guid(h.films["Thief"])+"&userId="+guid(h.ada.ID), "", nil); code != http.StatusNoContent {
		t.Errorf("adding Alien and Thief: %d, want 204", code)
	}
	var night playlistResult
	h.read(t, "pst_ada", "/Playlists/"+made.ID+"/Items", &night)
	if got := night.names(); len(got) != 3 || got[0] != "Heat" || got[1] != "Alien" || got[2] != "Thief" {
		t.Errorf("Night = %v, want Heat, Alien, Thief", got)
	}
	if len(*h.raised) != 2 || (*h.raised)[0].Kind != domain.EventUserDataChanged || (*h.raised)[0].Profile != h.ada.ID {
		t.Errorf("raised %+v, want Ada told twice her playlist changed", *h.raised)
	}

	for _, refused := range []struct {
		name, body string
		want       int
	}{
		{"for Bob", `{"Name":"Theirs","UserId":"` + guid(h.bob.ID) + `"}`, http.StatusForbidden},
		{"public", `{"Name":"Ours","IsPublic":true}`, http.StatusBadRequest},
		{"shared with Bob", `{"Name":"Ours","Users":[{"UserId":"` + guid(h.bob.ID) + `","CanEdit":true}]}`, http.StatusBadRequest},
		{"with no name", `{"Ids":["` + guid(h.films["Heat"]) + `"]}`, http.StatusBadRequest},
		{"of a title not there", `{"Name":"Ghosts","Ids":["` + guid(uuid.NewV7()) + `"]}`, http.StatusNotFound},
	} {
		if code := h.send(t, http.MethodPost, "pst_ada", "/Playlists", refused.body, nil); code != refused.want {
			t.Errorf("a playlist %s: %d, want %d", refused.name, code, refused.want)
		}
	}
	var mine playlistResult
	h.read(t, "pst_ada", "/Items?includeItemTypes=Playlist", &mine)
	if got := mine.names(); len(got) != 1 {
		t.Errorf("Ada's playlists = %v, want Night alone: none refused is made", got)
	}
	if code := h.send(t, http.MethodPost, "pst_bob", "/Playlists/"+made.ID+"/Items?ids="+guid(h.films["Heat"]), "", nil); code != http.StatusNotFound {
		t.Errorf("Bob adds to Ada's playlist: %d, want 404", code)
	}
}

// An app moves a playlist's entries and takes them out by their own ids, so of a title in it twice
// only the one named goes; no profile changes another's.
func TestAnAppRearrangesAPlaylist(t *testing.T) {
	h := newHousehold(t)
	night, err := h.st.AddPlaylist(t.Context(), h.ada.ID, "Night", []uuid.UUID{h.films["Heat"], h.films["Alien"], h.films["Thief"], h.films["Heat"]})
	if err != nil {
		t.Fatal(err)
	}
	path := "/Playlists/" + guid(night) + "/Items"
	entries := func() (names, ids []string) {
		t.Helper()
		var r playlistResult
		h.read(t, "pst_ada", path, &r)
		for _, it := range r.Items {
			names, ids = append(names, it.Name), append(ids, it.PlaylistItemID)
		}
		return names, ids
	}
	_, ids := entries()
	if code := h.send(t, http.MethodPost, "pst_ada", path+"/"+ids[2]+"/Move/0", "", nil); code != http.StatusNoContent {
		t.Errorf("moving Thief first: %d, want 204", code)
	}
	if names, _ := entries(); len(names) != 4 || names[0] != "Thief" || names[1] != "Heat" || names[3] != "Heat" {
		t.Errorf("Night = %v, want Thief, Heat, Alien, Heat", names)
	}
	if code := h.send(t, http.MethodDelete, "pst_ada", path+"?entryIds="+ids[0]+","+ids[1], "", nil); code != http.StatusNoContent {
		t.Errorf("taking out the first Heat and Alien: %d, want 204", code)
	}
	if names, _ := entries(); len(names) != 2 || names[0] != "Thief" || names[1] != "Heat" {
		t.Errorf("Night = %v, want Thief and the second Heat", names)
	}
	if len(*h.raised) != 2 {
		t.Errorf("raised %d events, want one for each change", len(*h.raised))
	}
	if code := h.send(t, http.MethodPost, "pst_bob", path+"/"+ids[3]+"/Move/0", "", nil); code != http.StatusNotFound {
		t.Errorf("Bob moves Ada's entry: %d, want 404", code)
	}
	if code := h.send(t, http.MethodDelete, "pst_bob", path+"?entryIds="+ids[3], "", nil); code != http.StatusNotFound {
		t.Errorf("Bob takes out Ada's entry: %d, want 404", code)
	}
	if code := h.send(t, http.MethodPost, "pst_ada", path+"/"+ids[0]+"/Move/0", "", nil); code != http.StatusNotFound {
		t.Errorf("moving an entry taken out: %d, want 404", code)
	}
}

// An app reads a playlist as Jellyfin's apps edit one, shared with no one, and renames it; it may
// not share it, nor replace its titles at once, and no profile reads or renames another's.
func TestAnAppEditsAPlaylist(t *testing.T) {
	h := newHousehold(t)
	night, err := h.st.AddPlaylist(t.Context(), h.ada.ID, "Night", []uuid.UUID{h.films["Heat"], h.films["Thief"]})
	if err != nil {
		t.Fatal(err)
	}
	path := "/Playlists/" + guid(night)
	var dto struct {
		OpenAccess bool
		Shares     []any
		ItemIDs    []string
	}
	h.read(t, "pst_ada", path, &dto)
	if dto.OpenAccess || dto.Shares == nil || len(dto.Shares) != 0 || len(dto.ItemIDs) != 2 || dto.ItemIDs[1] != guid(h.films["Thief"]) {
		t.Errorf("Night = %+v, want Heat and Thief, shared with no one", dto)
	}
	if code := h.send(t, http.MethodPost, "pst_ada", path, `{"Name":"Late","IsPublic":false}`, nil); code != http.StatusNoContent {
		t.Errorf("renaming Night: %d, want 204", code)
	}
	var late struct{ Name string }
	if h.read(t, "pst_ada", "/Items/"+guid(night), &late); late.Name != "Late" {
		t.Errorf("renamed, Night is %q, want Late", late.Name)
	}
	for _, refused := range []struct{ name, body string }{
		{"shared", `{"Name":"Ours","IsPublic":true}`},
		{"given titles at once", `{"Name":"Late","Ids":["` + guid(h.films["Alien"]) + `"]}`},
		{"unnamed", `{}`},
	} {
		if code := h.send(t, http.MethodPost, "pst_ada", path, refused.body, nil); code != http.StatusBadRequest {
			t.Errorf("Late %s: %d, want 400", refused.name, code)
		}
	}
	if code := h.read(t, "pst_bob", path, nil); code != http.StatusNotFound {
		t.Errorf("Bob reads Ada's playlist: %d, want 404", code)
	}
	if code := h.send(t, http.MethodPost, "pst_bob", path, `{"Name":"Mine"}`, nil); code != http.StatusNotFound {
		t.Errorf("Bob renames Ada's playlist: %d, want 404", code)
	}
	if h.read(t, "pst_ada", "/Items/"+guid(night), &late); late.Name != "Late" {
		t.Errorf("after all that, Night is %q, want Late", late.Name)
	}
}
