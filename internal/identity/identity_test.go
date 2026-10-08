package identity

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type kept struct {
	mu  sync.Mutex
	set domain.ServerSettings
}

func (k *kept) ServerSettings(context.Context) (domain.ServerSettings, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.set, nil
}

// A server named nothing is called by its node's host; a name and language an admin sets are taken
// up as the node is told of them.
func TestANodeTakesUpTheServersNameAsItIsTold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := &kept{set: domain.ServerSettings{Locale: domain.Locale{Language: "en-US", Country: "US"}}}
		events := make(chan domain.Event)
		s, err := New(t.Context(), settings, "mini", slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		if s.Name() != "mini" || s.Locale().Language != "en-US" {
			t.Fatalf("at first: %q in %+v", s.Name(), s.Locale())
		}
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.Run(ctx, func() (<-chan domain.Event, func()) { return events, func() {} })
		}()
		synctest.Wait()
		settings.mu.Lock()
		settings.set = domain.ServerSettings{Name: "Den", Locale: domain.Locale{Language: "en-GB", Country: "GB"}}
		settings.mu.Unlock()
		events <- domain.Event{Kind: domain.EventServerChanged}
		synctest.Wait()
		if s.Name() != "Den" || s.Locale() != (domain.Locale{Language: "en-GB", Country: "GB"}) {
			t.Errorf("after the change: %q in %+v", s.Name(), s.Locale())
		}
		stop()
		<-done
	})
}
