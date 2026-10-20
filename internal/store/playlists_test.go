//go:build integration

package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// entryCard is the card of the title an entry plays.
func (s *Store) entryCard(ctx context.Context, t *testing.T, profile uuid.UUID, e PlaylistEntry) Card {
	t.Helper()
	cards, err := s.Cards(ctx, profile, []uuid.UUID{e.Title})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	return cards[0]
}

func TestAPlaylistKeepsItsOrder(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv", "")
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
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
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
			card := s.entryCard(ctx, t, oliver.ID, e)
			name := card.Title
			if card.EpisodeNumber != nil {
				name += string(rune('0' + *card.EpisodeNumber))
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
	// Shuffled, a show plays its episodes.
	cards, err := s.PlayOrder(ctx, oliver.ID, show)
	if err != nil || len(cards) != 3 || *cards[0].EpisodeNumber != 1 || *cards[2].EpisodeNumber != 3 {
		t.Errorf("the show's playable = %+v, %v; want its three episodes in order", cards, err)
	}
	if _, err := s.PlayOrder(ctx, oliver.ID, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("playable of a title not there: %v, want ErrNotFound", err)
	}
	if err := s.AddToPlaylist(ctx, oliver.ID, list, []uuid.UUID{uuid.NewV7()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("adding a title there is not: %v, want ErrNotFound", err)
	}
	if err := s.RemovePlaylist(ctx, oliver.ID, list); err != nil {
		t.Fatal(err)
	}
	left, err := s.Playlists(ctx, oliver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("after removing it: %+v", left)
	}
}

// A smart playlist holds no entries of its own: it plays what its rule finds now, for the profile
// whose it is, so watching an episode takes that episode out of one of unwatched episodes.
func TestASmartPlaylistPlaysWhatItsRuleFinds(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv", "")
	if err != nil {
		t.Fatal(err)
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	var episodes []Episode
	for n := range 3 {
		name := "Cosmos" + string(rune('1'+n))
		episodes = append(episodes, Episode{
			Season: 1, Episodes: []int{n + 1}, Title: name, Folder: "Cosmos/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte(name), Parts: []Part{
				{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}},
			}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Cosmos/Season 1", []byte("v1"), Show{Title: "Cosmos", Folder: "Cosmos"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}

	unseen := SmartRule{
		Kind:   domain.WallEpisodes,
		Sort:   domain.SortTitle,
		Order:  domain.Ascending,
		Filter: WallFilter{Marks: []domain.Mark{domain.MarkUnwatched}},
	}
	list, err := s.AddSmartPlaylist(ctx, oliver.ID, tv.ID, "Not yet seen", unseen)
	if err != nil {
		t.Fatal(err)
	}
	names := func(offset, limit int) ([]string, int64) {
		t.Helper()
		entries, total, err := s.PlaylistEntries(ctx, oliver.ID, list, offset, limit)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, e := range entries {
			if e.ID != (uuid.UUID{}) {
				t.Errorf("%s has an entry of its own, and a rule's finding has none", e.Title)
			}
			out = append(out, s.entryCard(ctx, t, oliver.ID, e).Title)
		}
		return out, total
	}
	if got, total := names(0, 50); !slices.Equal(got, []string{"Cosmos1", "Cosmos2", "Cosmos3"}) || total != 3 {
		t.Fatalf("playlist = %q of %d, want its three unwatched episodes", got, total)
	}
	if got, total := names(1, 1); !slices.Equal(got, []string{"Cosmos2"}) || total != 3 {
		t.Errorf("second page = %q of %d, want Cosmos2 of three", got, total)
	}
	summaries, err := s.Playlists(ctx, oliver.ID)
	if err != nil || len(summaries) != 1 || summaries[0].Entries != 3 || summaries[0].DurationMS != (3*time.Hour).Milliseconds() {
		t.Fatalf("playlists = %+v, %v; want one of three entries, three hours", summaries, err)
	}
	if summaries[0].Rule == nil || summaries[0].Rule.Kind != domain.WallEpisodes || summaries[0].Library != tv.ID {
		t.Errorf("summary = %+v, want the rule it finds its episodes by, over the TV library", summaries[0])
	}

	// Watched, an episode is no longer one the rule finds.
	first, _, err := s.PlaylistEntries(ctx, oliver.ID, list, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, oliver.ID, first[0].Title, nil); err != nil {
		t.Fatal(err)
	}
	if got, total := names(0, 50); !slices.Equal(got, []string{"Cosmos2", "Cosmos3"}) || total != 2 {
		t.Errorf("after watching the first: %q of %d", got, total)
	}
	summaries, err = s.Playlists(ctx, oliver.ID)
	if err != nil || summaries[0].Entries != 2 || summaries[0].DurationMS != (2*time.Hour).Milliseconds() {
		t.Errorf("summary after watching the first = %+v, %v; want two entries, two hours", summaries, err)
	}

	// A rule that takes only its first findings stops the playlist, and its count, at them.
	capped := unseen
	capped.Limit = 1
	if err := s.SetPlaylistRule(ctx, oliver.ID, list, capped); err != nil {
		t.Fatal(err)
	}
	if got, total := names(0, 50); !slices.Equal(got, []string{"Cosmos2"}) || total != 1 {
		t.Errorf("capped at one: %q of %d", got, total)
	}
	if got, total := names(1, 50); len(got) != 0 || total != 1 {
		t.Errorf("past the cap: %q of %d, want none of one", got, total)
	}
	summaries, err = s.Playlists(ctx, oliver.ID)
	if err != nil || summaries[0].Entries != 1 || summaries[0].DurationMS != time.Hour.Milliseconds() {
		t.Errorf("capped summary = %+v, %v; want one entry, one hour", summaries, err)
	}

	// Its entries are the rule's, so nothing else sets them.
	for name, err := range map[string]error{
		"add":    s.AddToPlaylist(ctx, oliver.ID, list, []uuid.UUID{first[0].Title}),
		"remove": s.RemoveFromPlaylist(ctx, oliver.ID, list, uuid.NewV7()),
		"move":   s.MovePlaylistEntry(ctx, oliver.ID, list, uuid.NewV7(), 0),
	} {
		if !errors.Is(err, ErrSmartPlaylist) {
			t.Errorf("%s to a smart playlist: %v, want ErrSmartPlaylist", name, err)
		}
	}
	// And a playlist made by hand takes no rule.
	hand, err := s.AddPlaylist(ctx, oliver.ID, "Night in", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlaylistRule(ctx, oliver.ID, hand, unseen); !errors.Is(err, ErrNotSmartPlaylist) {
		t.Errorf("a rule for a playlist made by hand: %v, want ErrNotSmartPlaylist", err)
	}

	// A rule finds what a playlist can play, from a library the profile sees.
	shows := unseen
	shows.Kind = domain.WallShows
	if _, err := s.AddSmartPlaylist(ctx, oliver.ID, tv.ID, "Shows", shows); !errors.Is(err, ErrRuleNotPlayable) {
		t.Errorf("a rule finding shows: %v, want ErrRuleNotPlayable", err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Unrated: domain.UnratedAllow, Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSmartPlaylist(ctx, kid.ID, tv.ID, "Not mine", unseen); !errors.Is(err, ErrNotFound) {
		t.Errorf("a rule over a library the profile does not see: %v, want ErrNotFound", err)
	}
}
