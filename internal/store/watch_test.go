//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestWhatAProfileHasWatched(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.AddProfile(ctx, "Guest", domain.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}
	var episodes []Episode
	for n := 1; n <= 3; n++ {
		episodes = append(episodes, Episode{
			Season: 1, Episodes: []int{n}, Title: "episode", Folder: "The Wire/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte{byte(n)}, Parts: []Part{{
				RelPath: "The Wire/Season 1/" + string(rune('0'+n)) + ".mkv", Size: 1, ModTime: time.Unix(0, 0),
				Facts: &domain.Facts{Duration: time.Hour},
			}}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire/Season 1", []byte("v1"), Show{Title: "the wire", Folder: "The Wire"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	show, err := s.q.Item.WithContext(ctx).Where(s.q.Item.Kind.Eq(string(domain.ItemShow))).Take()
	if err != nil {
		t.Fatal(err)
	}
	page := func(profile uuid.UUID) TitlePage {
		t.Helper()
		p, err := s.Title(ctx, profile, uuid.UUID(show.ID))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	season := func(profile uuid.UUID) TitlePage {
		t.Helper()
		p, err := s.Title(ctx, profile, page(profile).Seasons[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first, second := season(oliver.ID).Episodes[0].ID, season(oliver.ID).Episodes[1].ID

	// In order: a stop near the start puts the position back to nothing.
	for _, p := range []struct {
		at   time.Duration
		want domain.Reach
	}{{2 * time.Minute, domain.ReachStart}, {20 * time.Minute, domain.ReachResumable}} {
		if reach, err := s.SaveProgress(ctx, oliver.ID, second, p.at, domain.ReachStart); err != nil || reach != p.want {
			t.Errorf("progress at %v: %s, %v; want %s", p.at, reach, err, p.want)
		}
	}
	if reach, err := s.SaveProgress(ctx, oliver.ID, first, 58*time.Minute, domain.ReachStart); err != nil || reach != domain.ReachEnd {
		t.Errorf("progress near the end: %s, %v; want it watched", reach, err)
	}
	eps := season(oliver.ID).Episodes
	if eps[0].State.WatchedAt == nil || eps[0].State.Plays != 1 || eps[1].State.PositionMS != (20*time.Minute).Milliseconds() {
		t.Errorf("episodes = %+v, %+v; want the first watched, the second resumable at 20 minutes", eps[0].State, eps[1].State)
	}
	if st := page(oliver.ID).State; st.Unwatched != 2 || st.WatchedAt != nil || st.LastPlayedAt == nil {
		t.Errorf("show = %+v, want two left, not watched, last played", st)
	}
	if st := page(guest.ID).State; st.Unwatched != 3 || st.LastPlayedAt != nil {
		t.Errorf("another profile's show = %+v, want all three left", st)
	}

	if err := s.MarkWatched(ctx, oliver.ID, uuid.UUID(show.ID)); err != nil {
		t.Fatal(err)
	}
	if st := page(oliver.ID).State; st.Unwatched != 0 || st.WatchedAt == nil {
		t.Errorf("after marking the show, it = %+v, want every episode watched", st)
	}
	if err := s.MarkUnwatched(ctx, oliver.ID, page(oliver.ID).Seasons[0].ID); err != nil {
		t.Fatal(err)
	}
	if st := season(oliver.ID).Episodes[1].State; st.WatchedAt != nil || st.PositionMS != 0 || st.Plays != 1 {
		t.Errorf("after unmarking the season, an episode = %+v, want unwatched from the start, its play still counted", st)
	}

	if err := s.Favourite(ctx, oliver.ID, uuid.UUID(show.ID)); err != nil {
		t.Fatal(err)
	}
	cards, _, err := s.Wall(ctx, lib.ID, WallPage{Profile: oliver.ID, Sort: domain.SortTitle, Order: domain.Ascending, Limit: 5})
	if err != nil || len(cards) != 1 || cards[0].State.FavouriteAt == nil || cards[0].State.Unwatched != 3 {
		t.Errorf("card = %+v, %v; want a favourite with three left", cards, err)
	}
	if err := s.Unfavourite(ctx, oliver.ID, uuid.UUID(show.ID)); err != nil {
		t.Fatal(err)
	}
	if page(oliver.ID).State.FavouriteAt != nil {
		t.Error("still a favourite after unfavouriting")
	}
	if _, err := s.SaveProgress(ctx, oliver.ID, uuid.NewV7(), time.Minute, domain.ReachStart); !errors.Is(err, ErrNotFound) {
		t.Errorf("progress on no title: %v, want ErrNotFound", err)
	}
}

func TestAPlaybackIsOnePlayHoweverOftenItReportsTheEnd(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	heat := []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
		RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
	}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v"), []Film{{Title: "Heat", Folder: "Heat", Copies: heat}}, nil); err != nil {
		t.Fatal(err)
	}
	row, err := s.q.Item.WithContext(ctx).Where(s.q.Item.Kind.Eq(string(domain.ItemMovie))).Take()
	if err != nil {
		t.Fatal(err)
	}
	film := uuid.UUID(row.ID)
	state := func() TitleState {
		t.Helper()
		p, err := s.Title(ctx, oliver.ID, film)
		if err != nil {
			t.Fatal(err)
		}
		return p.State
	}

	// A player reports every ten seconds through the last tenth, then stops at the end: its
	// playback has reached the end since the first of those reports.
	before := domain.ReachResumable
	for at := 55 * time.Minute; at <= time.Hour; at += 10 * time.Second {
		reach, err := s.SaveProgress(ctx, oliver.ID, film, at, before)
		if err != nil || reach != domain.ReachEnd {
			t.Fatalf("progress at %v: %s, %v; want the end", at, reach, err)
		}
		before = reach
	}
	first := state()
	if first.Plays != 1 || first.WatchedAt == nil || first.PositionMS != 0 {
		t.Fatalf("after one playback = %+v, want one play, watched, no position", first)
	}

	if err := s.MarkWatched(ctx, oliver.ID, film); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProgress(ctx, oliver.ID, film, time.Hour, domain.ReachResumable); err != nil {
		t.Fatal(err)
	}
	if again := state(); again.Plays != 2 || !again.WatchedAt.Equal(*first.WatchedAt) {
		t.Errorf("after marking it and watching it again = %+v, want a second play and the first watched time %v", again, first.WatchedAt)
	}
}
