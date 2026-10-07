package nodes

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type kept struct {
	mu  sync.Mutex
	set domain.NodeSettings
}

func (k *kept) JoinNode(ctx context.Context, id uuid.UUID, _ string) (domain.NodeRecord, error) {
	return k.Node(ctx, id)
}

func (k *kept) Node(_ context.Context, id uuid.UUID) (domain.NodeRecord, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return domain.NodeRecord{ID: id, NodeSettings: k.set}, nil
}

func (k *kept) change(s domain.NodeSettings) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.set = s
}

type slots struct {
	mu    sync.Mutex
	limit int
}

func (s *slots) Transcodes() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return 1, 0, s.limit
}

func (s *slots) SetLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = limit
}

// A node takes up what an admin sets of it as it is told: its role, which says whether it
// encodes, and its limit, its encoder's own where none is set.
func TestANodeTakesUpWhatIsSetOfItAsItIsTold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := &kept{set: domain.NodeSettings{Role: domain.NodeAll, LimitSource: domain.LimitAutomatic}}
		t8 := &slots{}
		self, err := Join(t.Context(), settings, uuid.NewV7(), "gpu-1", "http://gpu-1", domain.Encoder{}, 8, t8, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		if n := self.Node(); !self.Encodes() || n.Limit != 8 || n.LimitSource != domain.LimitAutomatic || n.Role != domain.NodeAll {
			t.Fatalf("joined as %+v, encodes %v; want all, its encoder's 8", n, self.Encodes())
		}
		events := make(chan domain.Event)
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			self.Run(ctx, func() (<-chan domain.Event, func()) { return events, func() {} })
		}()
		settings.change(domain.NodeSettings{Role: domain.NodeServe, LimitSource: domain.LimitSet, Limit: 3})
		events <- domain.Event{Kind: domain.EventNodesChanged}
		synctest.Wait()
		if n := self.Node(); self.Encodes() || n.Limit != 3 || n.Role != domain.NodeServe || n.LimitSource != domain.LimitSet {
			t.Errorf("after the change: %+v, encodes %v; want serve, set to 3, encoding nothing", n, self.Encodes())
		}
		stop()
		<-done
	})
}
