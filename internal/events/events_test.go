//go:build integration

package events

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
	"uuid"

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
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	return New(st, k, Server{ID: uuid.NewV7(), Name: "den"}, log), st
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

func TestAScansChangesAreToldAFewAtATime(t *testing.T) {
	hub, _ := newHub(t)
	running, stop := context.WithCancel(t.Context())
	defer stop()
	go hub.Run(running)
	told, unsubscribe := hub.Subscribe()
	defer unsubscribe()
	lib := uuid.NewV7()
	// Subscribing to Valkey is not instant: a change told before it would be missed.
	again := time.NewTicker(100 * time.Millisecond)
	defer again.Stop()
	for heard := false; !heard; {
		hub.Raise(t.Context(), domain.Event{Kind: domain.EventScanProgress, Library: lib})
		select {
		case e := <-told:
			heard = e.Library == lib
		case <-again.C:
		}
	}
	var added []uuid.UUID
	for range 300 {
		id := uuid.NewV7()
		added = append(added, id)
		hub.Changed(t.Context(), lib, store.Changed{domain.TitleAdded: {id}})
		hub.Changed(t.Context(), lib, store.Changed{domain.TitleUpdated: {id}})
	}
	hub.Changed(t.Context(), lib, store.Changed{domain.TitleRemoved: {added[0]}})

	var events []domain.Event
	quiet := time.After(2*changeWindow + time.Second)
	for waiting := true; waiting; {
		select {
		case e := <-told:
			if e.Kind == domain.EventLibraryChanged && e.Library == lib {
				events = append(events, e)
			}
		case <-quiet:
			waiting = false
		}
	}
	if len(events) != 1 {
		t.Fatalf("told %d library.changed events, want the 601 changes as one", len(events))
	}
	count := func(change domain.TitleChange) int {
		ids, _ := events[0].Details[string(change)].([]any)
		return len(ids)
	}
	if count(domain.TitleAdded) != 299 || count(domain.TitleUpdated) != 0 || count(domain.TitleRemoved) != 1 {
		t.Errorf("told %v; want 299 added, the one removed removed, and nothing as only updated", events[0].Details)
	}
}
