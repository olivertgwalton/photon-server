//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
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
				Facts: &media.Facts{Duration: time.Hour},
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

	for position, want := range map[time.Duration]domain.Reach{
		2 * time.Minute: domain.ReachStart, 20 * time.Minute: domain.ReachResumable,
	} {
		if reach, err := s.SaveProgress(ctx, oliver.ID, second, position); err != nil || reach != want {
			t.Errorf("progress at %v: %s, %v; want %s", position, reach, err, want)
		}
	}
	if reach, err := s.SaveProgress(ctx, oliver.ID, first, 58*time.Minute); err != nil || reach != domain.ReachEnd {
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
	if _, err := s.SaveProgress(ctx, oliver.ID, uuid.NewV7(), time.Minute); !errors.Is(err, ErrNotFound) {
		t.Errorf("progress on no title: %v, want ErrNotFound", err)
	}
}
