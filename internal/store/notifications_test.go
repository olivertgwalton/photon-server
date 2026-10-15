//go:build integration

package store

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestNotify(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	profile := func(name string, notify domain.Notify) uuid.UUID {
		t.Helper()
		p, err := s.AddProfile(ctx, name, domain.RoleUser, "hash", nil)
		if err != nil {
			t.Fatal(err)
		}
		prefs := domain.DefaultPreferences()
		prefs.Notify = notify
		if _, err := s.SetPreferences(ctx, p.ID, prefs); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	fan, idle, everything, quiet, kid := profile("Fan", domain.NotifyMarked), profile("Idle", domain.NotifyMarked),
		profile("Everything", domain.NotifyAll), profile("Quiet", domain.NotifyNone), profile("Kid", domain.NotifyAll)
	if err := s.SetAccess(ctx, kid, ProfileAccess{Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	copies := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}
	}
	var episodes []int
	// scan saves the show with more episodes of its first season, as a scan finding them would, and
	// tells who cares of them.
	scan := func(more ...int) []uuid.UUID {
		t.Helper()
		since := time.Now()
		episodes = append(episodes, more...)
		var eps []Episode
		for _, n := range episodes {
			rel := fmt.Sprintf("Severance/S01E%02d.mkv", n)
			eps = append(eps, Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: "Severance", ByNumber: true, Copies: copies(rel)})
		}
		if _, err := s.SaveShowFolder(ctx, tv.ID, "Severance", fmt.Append(nil, episodes), Show{Title: "Severance", Folder: "Severance"}, eps, nil); err != nil {
			t.Fatal(err)
		}
		told, err := s.Notify(ctx, tv.ID, since)
		if err != nil {
			t.Fatal(err)
		}
		profiles := make([]uuid.UUID, len(told))
		for i, p := range told {
			profiles[i] = p.ProfileID
		}
		slices.SortFunc(profiles, uuid.UUID.Compare)
		return profiles
	}
	inbox := func(p uuid.UUID) Inbox {
		t.Helper()
		in, err := s.Inbox(ctx, p, nil)
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	episode := func(n int) uuid.UUID {
		return oneItem(t, s, "kind = 'episode' AND episode_number = $1", n).ID
	}

	if told := scan(1, 2); len(told) != 0 {
		t.Fatalf("a library's first titles told %v, want no one", told)
	}
	show := oneItem(t, s, "kind = 'show'").ID
	if err := s.Watchlist(ctx, fan, show); err != nil {
		t.Fatal(err)
	}

	want := []uuid.UUID{fan, everything}
	slices.SortFunc(want, uuid.UUID.Compare)
	if told := scan(3); !slices.Equal(told, want) {
		t.Fatalf("a new episode told %v, want the fan and everything %v", told, want)
	}
	for _, p := range []uuid.UUID{idle, quiet, kid} {
		if in := inbox(p); len(in.Notifications) != 0 || in.Unread != 0 {
			t.Errorf("profile %v told %+v; it marked nothing, wants nothing, or may not see the show", p, in)
		}
	}
	got := inbox(fan)
	if len(got.Notifications) != 1 || got.Unread != 1 {
		t.Fatalf("the fan's inbox = %+v, want one unread", got)
	}
	if n := got.Notifications[0]; n.Title != show || n.Latest != episode(3) || n.New != 1 || n.ReadAt != nil {
		t.Errorf("the fan's notification = %+v, want the show with episode 3 new", n)
	}

	scan(4, 5)
	got = inbox(fan)
	if len(got.Notifications) != 1 || got.Notifications[0].New != 3 || got.Notifications[0].Latest != episode(5) {
		t.Fatalf("more episodes while unread: %+v, want them gathered into one of 3, the latest 5", got)
	}
	if told := scan(); len(told) != 0 {
		t.Errorf("a scan adding nothing told %v", told)
	}
	if _, err := s.Notify(ctx, tv.ID, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if got := inbox(fan); got.Notifications[0].New != 3 {
		t.Errorf("episodes told again: %+v", got)
	}

	if only, err := s.Inbox(ctx, fan, []int64{got.Notifications[0].ID}); err != nil || len(only.Notifications) != 1 || only.Unread != 1 {
		t.Errorf("the fan's one notification by its id: %+v %v", only, err)
	}
	if unread, err := s.ReadNotification(ctx, fan, got.Notifications[0].ID); err != nil || unread != 0 {
		t.Fatalf("read: %d unread, %v; want none", unread, err)
	}
	if _, err := s.ReadNotification(ctx, idle, got.Notifications[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("reading another profile's notification: %v, want ErrNotFound", err)
	}
	if err := s.MarkWatched(ctx, fan, episode(5), nil); err != nil {
		t.Fatal(err)
	}
	// A special the fan's watching is past is old news; the next episode is a new notification.
	if told := scan(6); !slices.Contains(told, fan) {
		t.Fatalf("episode 6 told %v, want the fan among them", told)
	}
	got = inbox(fan)
	if len(got.Notifications) != 2 || got.Unread != 1 || got.Notifications[0].New != 1 || got.Notifications[0].Latest != episode(6) {
		t.Fatalf("after reading: %+v, want a new unread one of episode 6 above the read one", got)
	}
	episodes = append(episodes[:0], 1, 2, 3, 4, 5, 6)
	if told := scan(0); slices.Contains(told, fan) {
		t.Errorf("an episode before the furthest the fan watched told it")
	}

	if unread, err := s.ReadNotifications(ctx, everything, time.Now()); err != nil || unread != 0 {
		t.Fatalf("read all: %d unread, %v; want none", unread, err)
	}
	if got := inbox(everything); got.Unread != 0 || len(got.Notifications) == 0 {
		t.Errorf("everything read: %+v, want its notifications kept, none unread", got)
	}
}
