//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAPlaylistKeepsItsOrder(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	part := func(name string) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}
	}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v1"), []Film{{Title: "Heat", Folder: "Heat", Copies: []Copy{part("Heat")}}}, nil); err != nil {
		t.Fatal(err)
	}
	var episodes []Episode
	for n := range 3 {
		episodes = append(episodes, Episode{Season: 1, Episodes: []int{n + 1}, Title: "Cosmos", Folder: "Cosmos/Season 1", ByNumber: true, Copies: []Copy{part("Cosmos" + string(rune('1'+n)))}})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Cosmos/Season 1", []byte("v1"), Show{Title: "Cosmos", Folder: "Cosmos"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	heat, show := oneItem(t, s, `kind = 'movie'`).ID, oneItem(t, s, `kind = 'show'`).ID
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "")
	if err != nil {
		t.Fatal(err)
	}

	// Heat, then the show as its three episodes, then Heat again.
	list, err := s.AddPlaylist(ctx, oliver.ID, "Night in", []uuid.UUID{heat, show, heat})
	if err != nil {
		t.Fatal(err)
	}
	read := func() ([]PlaylistEntry, []string) {
		t.Helper()
		entries, total, err := s.PlaylistEntries(ctx, oliver.ID, list, 0, 50)
		if err != nil || int(total) != len(entries) {
			t.Fatal(entries, total, err)
		}
		var names []string
		for _, e := range entries {
			name := e.Card.Title
			if e.Card.EpisodeNumber != nil {
				name += string(rune('0' + *e.Card.EpisodeNumber))
			}
			names = append(names, name)
		}
		return entries, names
	}
	entries, names := read()
	if want := []string{"Heat", "Cosmos1", "Cosmos2", "Cosmos3", "Heat"}; !slices.Equal(names, want) {
		t.Fatalf("playlist = %q, want %q", names, want)
	}
	if err := s.MovePlaylistEntry(ctx, oliver.ID, list, entries[4].ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFromPlaylist(ctx, oliver.ID, list, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, names = read(); !slices.Equal(names, []string{"Heat", "Cosmos1", "Cosmos2", "Cosmos3"}) {
		t.Errorf("after moving the second Heat first and removing the first: %q", names)
	}
	summaries, err := s.Playlists(ctx, oliver.ID)
	if err != nil || len(summaries) != 1 || summaries[0].Entries != 4 || summaries[0].DurationMS != (4*time.Hour).Milliseconds() {
		t.Errorf("playlists = %+v, %v; want one of four entries, four hours", summaries, err)
	}

	// Another profile's playlist is not there to it.
	for name, err := range map[string]error{
		"read":   func() error { _, _, err := s.PlaylistEntries(ctx, kid.ID, list, 0, 50); return err }(),
		"add":    s.AddToPlaylist(ctx, kid.ID, list, []uuid.UUID{heat}),
		"rename": s.RenamePlaylist(ctx, kid.ID, list, "Mine now"),
		"remove": s.RemovePlaylist(ctx, kid.ID, list),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("the kid's %s: %v, want ErrNotFound", name, err)
		}
	}
	if err := s.AddToPlaylist(ctx, oliver.ID, list, []uuid.UUID{uuid.NewV7()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("adding a title there is not: %v, want ErrNotFound", err)
	}
	if err := s.RemovePlaylist(ctx, oliver.ID, list); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.Playlists(ctx, oliver.ID); len(left) != 0 {
		t.Errorf("after removing it: %+v", left)
	}
}
