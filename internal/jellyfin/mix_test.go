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

// A music app's instant mix of a track, an album, an artist or a genre is tracks of the same
// genres, each playable, a track's own first, as many as the app asks for.
func TestAMusicAppMixesTracksOfTheSameGenres(t *testing.T) {
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
	music, err := st.AddLibrary(ctx, "Music", domain.LibraryMusic, "/srv/music", "")
	if err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	album := func(artist, title, genre string, tracks ...string) {
		t.Helper()
		a := store.Album{Title: title, Artist: artist, Folder: artist + "/" + title}
		for n, name := range tracks {
			rel := a.Folder + "/" + name + ".flac"
			a.Tracks = append(a.Tracks, store.Track{
				Title: name, Number: n + 1, Artists: []string{artist}, Genres: []string{genre}, Folder: a.Folder,
				Version: store.Version{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
					Duration: 4 * time.Minute, Streams: []domain.Stream{{Kind: domain.StreamAudio, Codec: "flac", Channels: 2}},
				}}}},
			})
		}
		if _, err := st.SaveMusicFolder(ctx, music.ID, a.Folder, []byte("v"), []store.Album{a}); err != nil {
			t.Fatal(err)
		}
	}
	album("The Weeknd", "Starboy", "R&B", "Starboy", "Party Monster")
	album("Frank Ocean", "Blonde", "R&B", "Nikes", "Ivy")
	album("Nirvana", "Nevermind", "Grunge", "Lithium")
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st})
	type result struct {
		Items []struct {
			Name         string `json:"Name"`
			ID           string `json:"Id"`
			Type         string `json:"Type"`
			MediaSources []any  `json:"MediaSources"`
		} `json:"Items"`
		TotalRecordCount int `json:"TotalRecordCount"`
	}
	get := func(target string) (int, result) {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Finamp", Token="pst_ada"`, "")
		var out result
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, out
	}
	names := func(r result) []string {
		var out []string
		for _, it := range r.Items {
			if it.Type != "Audio" || len(it.MediaSources) != 1 {
				t.Errorf("%s in a mix: %+v, want a track with its file", it.Name, it)
			}
			out = append(out, it.Name)
		}
		slices.Sort(out)
		return out
	}
	id := func(target, name string) string {
		t.Helper()
		_, r := get(target)
		for _, it := range r.Items {
			if it.Name == name {
				return it.ID
			}
		}
		t.Fatalf("%s: no %s in %+v", target, name, r)
		return ""
	}
	weeknd := id("/Artists/AlbumArtists", "The Weeknd")
	starboy := id("/Items?ParentId="+weeknd, "Starboy")
	track := id("/Items?ParentId="+starboy, "Starboy")
	blonde := id("/Items?ParentId="+id("/Artists/AlbumArtists", "Frank Ocean"), "Blonde")
	nirvana := id("/Artists/AlbumArtists", "Nirvana")

	rnb := []string{"Ivy", "Nikes", "Party Monster", "Starboy"}
	_, fromTrack := get("/Songs/" + track + "/InstantMix?fields=MediaSources")
	if got := names(fromTrack); len(fromTrack.Items) == 0 || fromTrack.Items[0].Name != "Starboy" || !slices.Equal(got, rnb) {
		t.Errorf("a mix of Starboy = %+v, want it first, then the other R&B tracks", fromTrack)
	}
	_, two := get("/Items/" + track + "/InstantMix?limit=2&fields=MediaSources")
	if len(two.Items) != 2 || two.Items[0].Name != "Starboy" || two.TotalRecordCount != 4 {
		t.Errorf("two of the mix of Starboy = %+v, want Starboy and one more of 4", two)
	}
	for _, tc := range []struct {
		target string
		want   []string
	}{
		{"/Albums/" + blonde + "/InstantMix?fields=MediaSources", rnb},
		{"/Artists/" + nirvana + "/InstantMix?fields=MediaSources", []string{"Lithium"}},
		{"/MusicGenres/InstantMix?fields=MediaSources&id=" + guid(nameID("Genre", "Grunge")), []string{"Lithium"}},
	} {
		if _, got := get(tc.target); !slices.Equal(names(got), tc.want) {
			t.Errorf("%s = %+v, want %v", tc.target, got, tc.want)
		}
	}
	for _, target := range []string{"/Items/" + guid(uuid.NewV7()) + "/InstantMix", "/MusicGenres/InstantMix?id=" + guid(nameID("Genre", "Jazz"))} {
		if code, _ := get(target); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, code)
		}
	}
}
