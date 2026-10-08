//go:build integration

package playback

import (
	"context"
	"os"
	"sync"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// Two nodes asked at once to start one play session, as a Jellyfin app's first requests may
// land on both behind a balancer, start it once: one node runs it, and the other is told so and
// hands the app on to it.
func TestAPlaySessionIsStartedOnOneNodeOnly(t *testing.T) {
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	quiet := func(context.Context, domain.Event) {}
	nodes := []uuid.UUID{uuid.NewV7(), uuid.NewV7()}
	session := uuid.NewV7()
	for range 20 {
		session = uuid.NewV7()
		var wg sync.WaitGroup
		errs := make([]error, len(nodes))
		ready := make(chan struct{})
		for i, node := range nodes {
			s := NewSessions(k, positions{}, served{}, quiet, node)
			wg.Go(func() {
				<-ready
				_, errs[i] = s.Start(t.Context(), session, domain.PlayTranscode, domain.PlaybackCard{}, 0, node)
			})
		}
		close(ready)
		wg.Wait()
		started := 0
		for _, err := range errs {
			if err == nil {
				started++
			}
		}
		if started != 1 {
			t.Fatalf("started on %d nodes of 2 (%v), want one", started, errs)
		}
		p, ok, err := k.Playback(t.Context(), session)
		winner := nodes[0]
		if errs[0] != nil {
			winner = nodes[1]
		}
		if !ok || err != nil || p.Node != winner {
			t.Fatalf("the session is kept as on %s (%v, %v), want the node that started it, %s", p.Node, ok, err, winner)
		}
	}
}
