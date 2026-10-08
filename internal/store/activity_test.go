//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestTheActivityLogIsReadNewestFirstAndForgetsTheOld(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Microsecond)
	for _, e := range []domain.Event{
		{Kind: domain.EventSignedIn, At: now.Add(-40 * 24 * time.Hour), Profile: oliver.ID},
		{Kind: domain.EventSignedIn, At: now.Add(-time.Hour), Profile: oliver.ID, Details: domain.SignInDetails{Device: "Living room"}},
		{Kind: domain.EventSignInRefused, At: now.Add(-time.Minute), Details: domain.SignInDetails{Name: "Guest"}},
		// A library removed as the event was on its way is left out, not refused.
		{Kind: domain.EventLibraryScanned, At: now, Library: uuid.NewV7()},
	} {
		if _, err := s.AddActivity(ctx, e); err != nil {
			t.Fatalf("%s: %v", e.Kind, err)
		}
	}
	all, total, err := s.Activity(ctx, "", 0, 10)
	if err != nil || total != 4 || len(all) != 4 {
		t.Fatalf("Activity = %d of %d, %v; want 4", len(all), total, err)
	}
	if all[0].Kind != domain.EventLibraryScanned || all[0].Library != (uuid.UUID{}) || all[1].Kind != domain.EventSignInRefused || all[1].Details != (domain.SignInDetails{Name: "Guest"}) {
		t.Errorf("newest first: %+v", all[:2])
	}
	ins, total, err := s.Activity(ctx, domain.EventSignedIn, 0, 1)
	if err != nil || total != 2 || len(ins) != 1 || ins[0].Profile != oliver.ID || ins[0].Details != (domain.SignInDetails{Device: "Living room"}) {
		t.Errorf("sign-ins' first page = %+v of %d, %v; want the latest of 2", ins, total, err)
	}

	if n, err := s.PruneActivity(ctx, now.Add(-30*24*time.Hour)); err != nil || n != 1 {
		t.Errorf("pruned %d, %v; want the one from 40 days ago", n, err)
	}
	// A profile removed leaves its entries, naming no one.
	if _, err := s.AddProfile(ctx, "Spare", domain.RoleAdmin, "h", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveProfile(ctx, oliver.ID, nil); err != nil {
		t.Fatal(err)
	}
	ins, total, err = s.Activity(ctx, domain.EventSignedIn, 0, 10)
	if err != nil || total != 1 || ins[0].Profile != (uuid.UUID{}) {
		t.Errorf("after removing Oliver: %+v of %d, %v; want his sign-in kept, naming no one", ins, total, err)
	}
}
