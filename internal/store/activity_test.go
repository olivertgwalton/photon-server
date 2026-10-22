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
	all, more, err := s.Activity(ctx, "", Newest, 10)
	if err != nil || more || len(all) != 4 {
		t.Fatalf("Activity = %d, more %v, %v; want 4 and no more", len(all), more, err)
	}
	if all[0].Kind != domain.EventLibraryScanned || all[0].Library != (uuid.UUID{}) || all[1].Kind != domain.EventSignInRefused || all[1].Details != (domain.SignInDetails{Name: "Guest"}) {
		t.Errorf("newest first: %+v", all[:2])
	}
	ins, more, err := s.Activity(ctx, domain.EventSignedIn, Newest, 1)
	if err != nil || !more || len(ins) != 1 || ins[0].Profile != oliver.ID || ins[0].Details != (domain.SignInDetails{Device: "Living room"}) {
		t.Errorf("sign-ins' first page = %+v, more %v, %v; want the latest, and more", ins, more, err)
	}
	if older, more, err := s.Activity(ctx, domain.EventSignedIn, Before{At: ins[0].At, ID: ins[0].ID}, 1); err != nil || more || len(older) != 1 || !older[0].At.Equal(now.Add(-40*24*time.Hour)) {
		t.Errorf("sign-ins' second page = %+v, more %v, %v; want the one from 40 days ago, and no more", older, more, err)
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
	ins, _, err = s.Activity(ctx, domain.EventSignedIn, Newest, 10)
	if err != nil || len(ins) != 1 || ins[0].Profile != (uuid.UUID{}) {
		t.Errorf("after removing Oliver: %+v, %v; want his sign-in kept, naming no one", ins, err)
	}
}

func TestWhatHasExpiredIsForgotten(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	for i, n := range []NewSession{
		{Kind: domain.SessionDevice, DeviceName: "Old phone", ExpiresAt: &past},
		{Kind: domain.SessionDevice, DeviceName: "Television", ExpiresAt: &future},
		{Kind: domain.SessionKey, DeviceName: "Backups"},
	} {
		n.ProfileID, n.TokenHash = oliver.ID, []byte{byte(i)}
		if _, err := s.CreateSession(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.PruneExpiredDevices(ctx); err != nil || n != 1 {
		t.Errorf("pruned %d devices, %v; want the old phone", n, err)
	}
	devices, err := s.Devices(ctx, nil)
	if err != nil || len(devices) != 1 || devices[0].DeviceName != "Television" {
		t.Errorf("devices = %+v, %v; want the television alone", devices, err)
	}
	if keys, err := s.Keys(ctx); err != nil || len(keys) != 1 {
		t.Errorf("keys = %+v, %v; want the key, which does not expire", keys, err)
	}

	kept, err := s.AddWebhook(ctx, "http://example.com/kept", domain.WebhookPhoton, []domain.EventKind{domain.EventSignedIn}, "s")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := s.AddWebhook(ctx, "http://example.com/gone", domain.WebhookPhoton, []domain.EventKind{domain.EventSignedIn}, "s")
	if err != nil {
		t.Fatal(err)
	}
	// Each delivery is queued, then left as it is or failed for good that long ago.
	for _, d := range []struct {
		hook   uuid.UUID
		body   string
		failed time.Duration
	}{
		{kept.ID, "failed long ago", 40 * 24 * time.Hour},
		{kept.ID, "failed lately", 24 * time.Hour},
		{kept.ID, "still trying", 0},
		{gone.ID, "to a webhook removed", time.Hour},
	} {
		if err := s.QueueDelivery(ctx, d.hook, domain.EventSignedIn, []byte(d.body)); err != nil {
			t.Fatal(err)
		}
		if d.failed > 0 {
			if _, err := s.pool.Exec(ctx, `
				UPDATE jobs SET state = 'dead', run_after = now() - $2::interval
				WHERE subject = (SELECT id FROM webhook_deliveries WHERE body = $1)`, d.body, d.failed); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.RemoveWebhook(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PruneFailedDeliveries(ctx, time.Now().Add(-30*24*time.Hour)); err != nil || n != 1 {
		t.Errorf("pruned %d deliveries, %v; want the one that failed long ago", n, err)
	}
	var bodies []string
	if err := s.pool.QueryRow(ctx, `SELECT array_agg(body ORDER BY body) FROM webhook_deliveries`).Scan(&bodies); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'deliver_webhook'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != "failed lately" || bodies[1] != "still trying" || jobs != 2 {
		t.Errorf("kept deliveries %q and %d jobs; want the one that failed lately and the one still trying, with theirs", bodies, jobs)
	}
}
