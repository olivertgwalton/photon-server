//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Each link's tracker is asked after it at most once each interval, however many look, until it
// is slowed, started again or ended.
func TestALinksTrackerIsAskedAtMostOncePerInterval(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	ctx := t.Context()
	due := func() []TrackerLink {
		t.Helper()
		got, err := k.DueTrackerLinks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	oliver, ada := uuid.NewV7(), uuid.NewV7()
	start := func(profile uuid.UUID, tr domain.Tracker, device string, interval, life time.Duration) TrackerLink {
		t.Helper()
		l := TrackerLink{
			Profile: profile, Tracker: tr, DeviceCode: device, UserCode: "ABCD1234",
			VerificationURI: "https://auth.trakt.tv/activate", VerificationURIComplete: "https://auth.trakt.tv/activate/ABCD1234",
			Interval: interval, Expires: time.Now().Add(life).Truncate(time.Second),
		}
		if err := k.StartTrackerLink(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	if got := due(); len(got) != 0 {
		t.Fatalf("due with none started: %+v", got)
	}
	want := start(oliver, domain.TrackerTrakt, "device", 0, time.Minute)
	start(ada, domain.TrackerSimkl, "waits", time.Hour, time.Minute)
	got := due()
	if len(got) != 1 || got[0].DeviceCode != want.DeviceCode || !got[0].Expires.Equal(want.Expires) || got[0].Profile != oliver {
		t.Fatalf("due: %+v; want only oliver's, as started", got)
	}
	if l, ok, err := k.TrackerLink(ctx, ada, domain.TrackerSimkl); err != nil || !ok || l.DeviceCode != "waits" {
		t.Errorf("ada's link: %+v %v %v; want it held, not yet due", l, ok, err)
	}
	if err := k.SlowTrackerLink(ctx, oliver, domain.TrackerTrakt, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := due(); len(got) != 0 {
		t.Errorf("due once slowed: %+v", got)
	}
	start(oliver, domain.TrackerTrakt, "again", 0, time.Minute)
	if got := due(); len(got) != 1 || got[0].DeviceCode != "again" {
		t.Errorf("started again: %+v; want the new code due", got)
	}
	if ended, err := k.EndTrackerLink(ctx, oliver, domain.TrackerTrakt); err != nil || !ended {
		t.Errorf("ending: %v, %v; want it ended", ended, err)
	}
	if ended, err := k.EndTrackerLink(ctx, oliver, domain.TrackerTrakt); err != nil || ended {
		t.Errorf("ending again: %v, %v; want none to end", ended, err)
	}
	if err := k.SlowTrackerLink(ctx, oliver, domain.TrackerTrakt, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := k.TrackerLink(ctx, oliver, domain.TrackerTrakt); err != nil || ok {
		t.Errorf("an ended link, slowed after: held %v, %v", ok, err)
	}
}
