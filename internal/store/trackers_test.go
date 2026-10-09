//go:build integration

package store

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnAdminSetsTheAppEachTrackerLinksThrough(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	for _, set := range []struct {
		tracker domain.Tracker
		id      string
	}{{domain.TrackerTrakt, "one"}, {domain.TrackerSimkl, "two"}, {domain.TrackerTrakt, "three"}, {domain.TrackerSimkl, ""}} {
		if err := s.SetTrackerClient(ctx, set.tracker, set.id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.TrackerClients(ctx)
	if want := map[domain.Tracker]string{domain.TrackerTrakt: "three"}; err != nil || !maps.Equal(got, want) {
		t.Errorf("TrackerClients = %v, %v; want %v", got, err, want)
	}
}

// A profile links one account on each tracker, a second in place of the first, and unlinking it
// answers what the tracker granted, so the tracker can be told; another profile's are its own.
func TestAProfileLinksAnAccountOnEachTracker(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	ada, err := s.AddProfile(ctx, "Ada", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Microsecond)
	for _, link := range []struct {
		profile  uuid.UUID
		tracker  domain.Tracker
		username string
	}{
		{oliver.ID, domain.TrackerTrakt, "old"},
		{oliver.ID, domain.TrackerTrakt, "oliver"},
		{oliver.ID, domain.TrackerSimkl, "oliver"},
		{ada.ID, domain.TrackerTrakt, "ada"},
	} {
		tok := TrackerTokens{Access: link.username + "-access", Refresh: link.username + "-refresh", Expires: expires}
		if err := s.LinkTracker(ctx, link.profile, link.tracker, link.username, tok); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LinkTracker(ctx, uuid.NewV7(), domain.TrackerTrakt, "x", TrackerTokens{Expires: expires}); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such profile: %v, want ErrNotFound", err)
	}
	tok, err := s.UnlinkTracker(ctx, oliver.ID, domain.TrackerTrakt)
	if want := (TrackerTokens{"oliver-access", "oliver-refresh", expires}); err != nil || tok != want {
		t.Errorf("unlinking: %+v, %v; want %+v", tok, err, want)
	}
	if _, err := s.UnlinkTracker(ctx, oliver.ID, domain.TrackerTrakt); !errors.Is(err, ErrNotFound) {
		t.Errorf("unlinking again: %v, want ErrNotFound", err)
	}
	for profile, want := range map[uuid.UUID]string{oliver.ID: "simkl oliver", ada.ID: "trakt ada"} {
		got, err := s.TrackerGrants(ctx, profile)
		if err != nil || len(got) != 1 || string(got[0].Tracker)+" "+got[0].Username != want || got[0].LinkedAt.IsZero() {
			t.Errorf("%v's accounts: %+v, %v; want only %s", profile, got, err, want)
		}
	}
}

// Nodes refreshing an account at once refresh it once; each is answered the new tokens, and a
// refresh that fails keeps the old.
func TestAnAccountIsRefreshedOnceWhoeverAsks(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	soon := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	if err := s.LinkTracker(ctx, oliver.ID, domain.TrackerTrakt, "oliver", TrackerTokens{"old", "once", soon}); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(24 * time.Hour)
	refused := errors.New("invalid_grant")
	if _, err := s.RefreshTracker(ctx, oliver.ID, domain.TrackerTrakt, stale, func(context.Context, TrackerTokens) (TrackerTokens, error) {
		return TrackerTokens{}, refused
	}); !errors.Is(err, refused) {
		t.Errorf("a refused refresh: %v", err)
	}
	later := time.Now().Add(7 * 24 * time.Hour).Truncate(time.Microsecond)
	var calls atomic.Int32
	var wg sync.WaitGroup
	got := make([]TrackerTokens, 4)
	errs := make([]error, len(got))
	for i := range got {
		wg.Go(func() {
			got[i], errs[i] = s.RefreshTracker(ctx, oliver.ID, domain.TrackerTrakt, stale, func(_ context.Context, old TrackerTokens) (TrackerTokens, error) {
				calls.Add(1)
				if old.Refresh != "once" {
					t.Errorf("refreshed with %q, a refresh token already used", old.Refresh)
				}
				return TrackerTokens{"new", "next", later}, nil
			})
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	want := TrackerTokens{"new", "next", later}
	if calls.Load() != 1 || slices.ContainsFunc(got, func(tok TrackerTokens) bool { return tok != want }) {
		t.Errorf("refreshed %d times, answering %+v; want once, each answered %+v", calls.Load(), got, want)
	}
	if _, err := s.RefreshTracker(ctx, oliver.ID, domain.TrackerSimkl, stale, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("no account: %v, want ErrNotFound", err)
	}
}

// What a linked profile marks watched or unwatched, by play, by hand or by import, waits to be
// told to each tracker it linked, claimed by one node at a time; its progress, and what it did
// before linking, do not.
func TestATrackerIsToldWhatAProfileWatches(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	hour := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}}
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	heat := Film{Title: "Heat", Folder: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}, Copies: hour("Heat/h.mkv")}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []Film{heat}, nil); err != nil {
		t.Fatal(err)
	}
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	wire := Show{Title: "The Wire", Folder: "Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	pilot := Episode{Season: 1, Episodes: []int{2}, Title: "The Detail", Folder: "Wire", ByNumber: true, Copies: hour("Wire/S01E02.mkv")}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), wire, []Episode{pilot}, nil); err != nil {
		t.Fatal(err)
	}
	film := oneItem(t, s, "kind = 'movie'").ID
	episode := oneItem(t, s, "kind = 'episode'").ID
	ada, err := s.AddProfile(ctx, "Ada", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Watched before Ada linked an account: no tracker is told.
	if err := s.MarkWatched(ctx, ada.ID, episode, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkUnwatched(ctx, ada.ID, episode); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []domain.Tracker{domain.TrackerTrakt, domain.TrackerSimkl} {
		if err := s.LinkTracker(ctx, ada.ID, tr, "ada", TrackerTokens{"a", "r", time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	watched := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	before := watched.Add(-time.Hour)
	if _, err := s.SaveProgress(ctx, ada.ID, film, 20*time.Minute, time.Hour, domain.ReachStart, &before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProgress(ctx, ada.ID, film, 58*time.Minute, time.Hour, domain.ReachResumable, &watched); err != nil {
		t.Fatal(err)
	}
	// Watched again: a rewatch is its scrobble's to tell.
	if _, err := s.SaveProgress(ctx, ada.ID, film, 59*time.Minute, time.Hour, domain.ReachResumable, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, ada.ID, episode, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkUnwatched(ctx, ada.ID, episode); err != nil {
		t.Fatal(err)
	}

	if got, err := s.ClaimTrackerChanges(ctx, time.Now().Add(-time.Minute), time.Minute, 100); err != nil || len(got) != 0 {
		t.Fatalf("claimed %+v, %v before the changes settled", got, err)
	}
	claim := func() []TrackerChanges {
		t.Helper()
		got, err := s.ClaimTrackerChanges(ctx, time.Now().Add(time.Minute), time.Minute, 100)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := claim()
	if len(got) != 2 {
		t.Fatalf("claimed %+v, want each of Ada's two accounts' changes", got)
	}
	for _, account := range got {
		c := account.Changes
		if account.Profile != ada.ID || len(c) != 3 || len(account.Rows) != 3 {
			t.Fatalf("%s: %+v, want the film watched, the episode watched and then unwatched", account.Tracker, account)
		}
		if c[0].Kind != domain.ItemMovie || c[0].IDs[domain.ProviderTMDB] != "949" || c[0].WatchedAt == nil || !c[0].WatchedAt.Equal(watched) {
			t.Errorf("%s's film: %+v", account.Tracker, c[0])
		}
		if c[1].Kind != domain.ItemEpisode || c[1].IDs[domain.ProviderTVDB] != "79126" || c[1].Season != 1 || c[1].Episode != 2 || c[1].WatchedAt == nil {
			t.Errorf("%s's episode watched: %+v", account.Tracker, c[1])
		}
		if c[2].Episode != 2 || c[2].WatchedAt != nil {
			t.Errorf("%s's episode unwatched: %+v", account.Tracker, c[2])
		}
	}
	if again := claim(); len(again) != 0 {
		t.Errorf("claimed again while another node holds them: %+v", again)
	}
	if err := s.ForgetTrackerChanges(ctx, got[0].Rows); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetTrackerWatched(ctx, ada.ID, got[1].Tracker, film); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM tracker_outbox`); n != 2 {
		t.Errorf("%d changes left, want the second account's two of the episode", n)
	}
	if _, err := s.UnlinkTracker(ctx, ada.ID, got[1].Tracker); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM tracker_outbox`); n != 0 {
		t.Errorf("%d changes left once the account is unlinked", n)
	}
}
