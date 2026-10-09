//go:build integration

package store

import (
	"errors"
	"maps"
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
		got, err := s.TrackerAccounts(ctx, profile)
		if err != nil || len(got) != 1 || string(got[0].Tracker)+" "+got[0].Username != want || got[0].LinkedAt.IsZero() {
			t.Errorf("%v's accounts: %+v, %v; want only %s", profile, got, err, want)
		}
	}
}
