package reach

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type kept struct {
	mu sync.Mutex
	n  domain.Network
}

func (k *kept) Network(context.Context) (domain.Network, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.n, nil
}

// A node goes by how the server is reached from its first request, and takes up a change as it is
// told of it.
func TestANodeTakesUpHowTheServerIsReachedAsItIsTold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := &kept{n: domain.Network{Discovery: domain.DiscoveryBroadcast}}
		events := make(chan domain.Event)
		r, err := New(t.Context(), settings, func() (<-chan domain.Event, func()) { return events, func() {} }, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "172.18.0.2:5000"
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		if r.Client(req).String() != "172.18.0.2" || r.PublicURL() != nil || r.Discovery() != domain.DiscoveryBroadcast {
			t.Fatalf("at first: client %s, public %v, discovery %s", r.Client(req), r.PublicURL(), r.Discovery())
		}
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			r.Run(ctx)
		}()
		synctest.Wait()
		settings.mu.Lock()
		settings.n = domain.Network{
			TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}, PublicURL: "https://photon.example", Discovery: domain.DiscoveryOff,
		}
		settings.mu.Unlock()
		events <- domain.Event{Kind: domain.EventNetworkChanged}
		synctest.Wait()
		if r.Client(req).String() != "203.0.113.9" || r.PublicURL().String() != "https://photon.example" || r.Discovery() != domain.DiscoveryOff {
			t.Errorf("after the change: client %s, public %v, discovery %s", r.Client(req), r.PublicURL(), r.Discovery())
		}
		stop()
		<-done
	})
}
