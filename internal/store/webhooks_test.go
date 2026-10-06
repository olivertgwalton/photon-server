//go:build integration

package store

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Telling the webhooks of a playback that started costs no description of it on a server where no
// webhook asked: most have none, and playbacks start, pause and stop all day.
func TestAWebhookBodyIsMadeOnlyWhereOneAskedForIt(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	made := 0
	body := func(context.Context) ([]byte, error) {
		made++
		return []byte(`{"event":"playback.started"}`), nil
	}
	if err := s.QueueWebhooks(ctx, domain.EventPlaybackStarted, body); err != nil || made != 0 {
		t.Fatalf("with no webhooks: made %d bodies, %v; want none", made, err)
	}
	if _, err := s.AddWebhook(ctx, "http://example.com/hook", []domain.EventKind{domain.EventPlaybackStarted}, "s"); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueWebhooks(ctx, domain.EventPlaybackPaused, body); err != nil || made != 0 {
		t.Fatalf("a kind no webhook asked for: made %d bodies, %v; want none", made, err)
	}
	if err := s.QueueWebhooks(ctx, domain.EventPlaybackStarted, body); err != nil || made != 1 {
		t.Fatalf("a kind a webhook asked for: made %d bodies, %v; want one", made, err)
	}
	jobs, err := s.ClaimJobs(ctx, []domain.JobKind{domain.JobDeliverWebhook}, uuid.NewV7(), time.Minute, 10)
	if err != nil || len(jobs) != 1 {
		t.Errorf("queued %d deliveries, %v; want one", len(jobs), err)
	}
}
