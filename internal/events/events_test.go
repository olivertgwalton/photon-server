//go:build integration

package events

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func newHub(t *testing.T) (*Hub, *store.Store) {
	t.Helper()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"))
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	return New(st, k, log), st
}

func TestTheLogKeepsWhatAnAdminReadsLater(t *testing.T) {
	hub, st := newHub(t)
	ctx := t.Context()
	oliver, err := st.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h")
	if err != nil {
		t.Fatal(err)
	}
	hub.Raise(ctx, domain.Event{Kind: domain.EventSignedIn, Profile: oliver.ID, Details: map[string]any{"device": "Living room"}})
	hub.Raise(ctx, domain.Event{Kind: domain.EventTaskStarted, Details: map[string]any{"task": domain.TaskSweepJobs}})
	hub.Raise(ctx, domain.Event{Kind: domain.EventSignInRefused, Details: map[string]any{"name": "Oliver"}})
	got, total, err := st.Activity(ctx, "", 0, 10)
	if err != nil || total != 2 {
		t.Fatalf("Activity = %+v of %d, %v; want the sign-in and the refusal, not the task starting", got, total, err)
	}
	if got[0].Kind != domain.EventSignInRefused || got[1].Kind != domain.EventSignedIn || got[1].Profile != oliver.ID {
		t.Errorf("Activity = %+v; want the refusal, then Oliver's sign-in", got)
	}
	if since := time.Since(got[1].At); since < 0 || since > time.Minute {
		t.Errorf("signed in at %v, want now", got[1].At)
	}
}
