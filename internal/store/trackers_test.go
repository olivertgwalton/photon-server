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
