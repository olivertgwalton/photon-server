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
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv", "")
	if err != nil {
		t.Fatal(err)
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
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
	// Hushed is told of everything but TV, and of a library that is no library.
	hushed := profile("Hushed", domain.NotifyAll)
	prefs, err := s.Preferences(ctx, hushed)
	if err != nil {
		t.Fatal(err)
	}
	prefs.QuietLibraries = []uuid.UUID{tv.ID, uuid.NewV7()}
	if prefs, err = s.SetPreferences(ctx, hushed, prefs); err != nil || !slices.Equal(prefs.QuietLibraries, []uuid.UUID{tv.ID}) {
		t.Fatalf("quiet libraries kept: %v %v, want TV alone", prefs.QuietLibraries, err)
	}
	if err := s.SetAccess(ctx, kid, ProfileAccess{Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	scan := showScans(t, s, tv.ID)
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
	for _, p := range []uuid.UUID{idle, quiet, kid, hushed} {
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
	if unread, err := s.ClearNotification(ctx, fan, got.Notifications[0].ID); err != nil || unread != 0 {
		t.Fatalf("opened: %d unread, %v; want none", unread, err)
	}
	if _, err := s.ClearNotification(ctx, idle, got.Notifications[0].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("opening another profile's notification: %v, want ErrNotFound", err)
	}
	if err := s.MarkWatched(ctx, fan, episode(5), nil); err != nil {
		t.Fatal(err)
	}
	// A special the fan's watching is past is old news; the next episode is a new notification.
	if told := scan(6); !slices.Contains(told, fan) {
		t.Fatalf("episode 6 told %v, want the fan among them", told)
	}
	got = inbox(fan)
	if len(got.Notifications) != 1 || got.Unread != 1 || got.Notifications[0].New != 1 || got.Notifications[0].Latest != episode(6) {
		t.Fatalf("after opening: %+v, want a new unread one of episode 6, and the opened one gone", got)
	}
	if told := scan(0); slices.Contains(told, fan) {
		t.Errorf("an episode before the furthest the fan watched told it")
	}

	prefs.QuietLibraries = nil
	if prefs, err = s.SetPreferences(ctx, hushed, prefs); err != nil || len(prefs.QuietLibraries) != 0 {
		t.Errorf("no library quiet: %v %v, want none", prefs.QuietLibraries, err)
	}

	// Cleared up to a moment, one added after it stays.
	if _, err := s.ClearNotifications(ctx, fan, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if got := inbox(fan); len(got.Notifications) == 0 {
		t.Errorf("cleared up to before any was added: %+v, want them kept", got)
	}
	// Cleared, a notification leaves the inbox read, and what it told of is not told again.
	if unread, err := s.ClearNotifications(ctx, fan, time.Now()); err != nil || unread != 0 {
		t.Fatalf("cleared: %d unread, %v; want none", unread, err)
	}
	if got := inbox(fan); len(got.Notifications) != 0 || got.Unread != 0 {
		t.Errorf("after clearing: %+v, want an empty inbox", got)
	}
	if _, err := s.Notify(ctx, tv.ID, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if got := inbox(fan); len(got.Notifications) != 0 {
		t.Errorf("cleared titles told again: %+v", got)
	}

	if unread, err := s.ReadNotifications(ctx, everything, time.Now()); err != nil || unread != 0 {
		t.Fatalf("read all: %d unread, %v; want none", unread, err)
	}
	if got := inbox(everything); got.Unread != 0 || len(got.Notifications) == 0 {
		t.Errorf("everything read: %+v, want its notifications kept, none unread", got)
	}
}

// showScans answers a scan of a library that saves its show with more episodes of its first season,
// as a scan finding them would, and tells who cares of them.
func showScans(t *testing.T, s *Store, lib uuid.UUID) func(more ...int) []uuid.UUID {
	t.Helper()
	copies := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}
	}
	var episodes []int
	return func(more ...int) []uuid.UUID {
		t.Helper()
		since := time.Now()
		episodes = append(episodes, more...)
		var eps []Episode
		for _, n := range episodes {
			rel := fmt.Sprintf("Severance/S01E%02d.mkv", n)
			eps = append(eps, Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: "Severance", ByNumber: true, Copies: copies(rel)})
		}
		if _, err := s.SaveShowFolder(t.Context(), lib, "Severance", fmt.Append(nil, episodes), Show{Title: "Severance", Folder: "Severance"}, eps, nil); err != nil {
			t.Fatal(err)
		}
		told, err := s.Notify(t.Context(), lib, since)
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
}

func TestPush(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv", "")
	if err != nil {
		t.Fatal(err)
	}
	fan, err := s.AddProfile(ctx, "Fan", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddProfile(ctx, "Other", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.PushKey(ctx, []byte("first"))
	if err != nil || string(key) != "first" {
		t.Fatalf("the first key: %q %v", key, err)
	}
	if key, err := s.PushKey(ctx, []byte("second")); err != nil || string(key) != "first" {
		t.Errorf("another node's key: %q %v, want the first kept", key, err)
	}

	scan := showScans(t, s, tv.ID)
	scan(1)
	if err := s.Watchlist(ctx, fan.ID, oneItem(t, s, "kind = 'show'").ID); err != nil {
		t.Fatal(err)
	}
	pushJobs := func() int {
		return countRows(t, s, `SELECT count(*) FROM jobs WHERE kind = 'push' AND subject = $1`, fan.ID)
	}
	scan(2)
	if pushJobs() != 0 {
		t.Error("a push was queued for a profile with no browser subscribed")
	}
	if p, err := s.Pushes(ctx, fan.ID); err != nil || len(p) != 0 {
		t.Errorf("pushes of a profile with no browser: %+v %v, want none", p, err)
	}

	sub, made, err := s.Subscribe(ctx, other.ID, PushSubscription{Kind: domain.PushWeb, Endpoint: "https://push.example.com/a", P256DH: []byte{4}, Auth: []byte{1}, Language: "en"})
	if err != nil || !made {
		t.Fatalf("subscribing: %v, made %v", err, made)
	}
	// The same browser subscribed again, for the fan, is the fan's from now.
	if again, made, err := s.Subscribe(ctx, fan.ID, PushSubscription{Kind: domain.PushWeb, Endpoint: "https://push.example.com/a", P256DH: []byte{4}, Auth: []byte{2}, Language: "fr"}); err != nil || made || again.ID != sub.ID {
		t.Fatalf("subscribed again: %+v %v, made %v; want the same subscription", again, err, made)
	}
	if subs, err := s.PushSubscriptions(ctx, other.ID); err != nil || len(subs) != 0 {
		t.Errorf("the other's subscriptions: %+v %v, want none", subs, err)
	}
	if subs, err := s.PushSubscriptions(ctx, fan.ID); err != nil || len(subs) != 1 || subs[0].Language != "fr" || subs[0].Auth[0] != 2 {
		t.Errorf("the fan's subscriptions: %+v %v", subs, err)
	}

	scan(3, 4)
	if pushJobs() != 1 {
		t.Fatal("no push queued for the subscribed fan")
	}
	pushes, err := s.Pushes(ctx, fan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 1 || len(pushes[0].Items) != 2 || pushes[0].New != 3 {
		t.Fatalf("pushes = %+v, want one notification of 3 new, episodes 3 and 4 not yet pushed", pushes)
	}
	if err := s.Pushed(ctx, fan.ID, pushes[0].Items); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Pushes(ctx, fan.ID); err != nil || len(p) != 0 {
		t.Errorf("after pushing: %+v %v, want none left", p, err)
	}

	// An Apple device is its token and the push service that issued it, in place of a browser's keys.
	production := domain.APNsProduction
	phone := PushSubscription{Kind: domain.PushApple, Endpoint: "a1b2c3", ApnsEnvironment: &production, Language: "en"}
	if _, _, err := s.Subscribe(ctx, fan.ID, phone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Subscribe(ctx, fan.ID, PushSubscription{Kind: domain.PushApple, Endpoint: "d4e5", Language: "en"}); err == nil {
		t.Error("an Apple device with no push service was kept")
	}
	subs, err := s.PushSubscriptions(ctx, fan.ID)
	if err != nil || len(subs) != 2 || subs[1].Kind != domain.PushApple || subs[1].ApnsEnvironment == nil || *subs[1].ApnsEnvironment != production {
		t.Errorf("the fan's devices: %+v %v, want its browser and its phone", subs, err)
	}
	if c, err := s.PushRelayCredential(ctx); err != nil || c != "" {
		t.Errorf("a credential before one was asked for: %q %v", c, err)
	}
	if err := s.SetPushRelayCredential(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if c, err := s.PushRelayCredential(ctx); err != nil || c != "c1" {
		t.Errorf("the credential kept: %q %v", c, err)
	}

	if err := s.Unsubscribe(ctx, other.ID, sub.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unsubscribing another profile's browser: %v, want ErrNotFound", err)
	}
	if err := s.Unsubscribe(ctx, fan.ID, sub.ID); err != nil {
		t.Error(err)
	}
}
