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
	return k.SeeNode(ctx, id)
}

func (k *kept) SeeNode(_ context.Context, id uuid.UUID) (domain.NodeRecord, error) {
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
// encodes, its limit, its encoder's own where none is set, and whether it is drained.
func TestANodeTakesUpWhatIsSetOfItAsItIsTold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		settings := &kept{set: domain.NodeSettings{Role: domain.NodeAll, LimitSource: domain.LimitAutomatic, Availability: domain.NodeActive}}
		t8 := &slots{}
		self, err := Join(t.Context(), settings, uuid.NewV7(), "gpu-1", "http://gpu-1", domain.Encoder{}, 8, t8, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		if n := self.Node(); !self.TakesTranscodes() || n.Limit != 8 || n.LimitSource != domain.LimitAutomatic || n.Role != domain.NodeAll {
			t.Fatalf("joined as %+v, encodes %v; want all, its encoder's 8", n, self.TakesTranscodes())
		}
		events := make(chan domain.Event)
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			self.Run(ctx, func() (<-chan domain.Event, func()) { return events, func() {} })
		}()
		settings.change(domain.NodeSettings{Role: domain.NodeServe, LimitSource: domain.LimitSet, Limit: 3, Availability: domain.NodeActive})
		events <- domain.Event{Kind: domain.EventNodesChanged}
		synctest.Wait()
		if n := self.Node(); self.TakesTranscodes() || n.Limit != 3 || n.Role != domain.NodeServe || n.LimitSource != domain.LimitSet {
			t.Errorf("after the change: %+v, encodes %v; want serve, set to 3, encoding nothing", n, self.TakesTranscodes())
		}
		<-self.Changes()
		// Drained, it takes nothing new, and tells the others so at once.
		settings.change(domain.NodeSettings{Role: domain.NodeAll, LimitSource: domain.LimitSet, Limit: 3, Availability: domain.NodeDraining})
		events <- domain.Event{Kind: domain.EventNodesChanged}
		synctest.Wait()
		if n := self.Node(); self.TakesTranscodes() || n.Availability != domain.NodeDraining {
			t.Errorf("drained: %+v, takes %v; want it taking nothing new", n, self.TakesTranscodes())
		}
		select {
		case <-self.Changes():
		default:
			t.Error("draining was not told, for the others to be told at once")
		}
		stop()
		<-done
	})
}

// A node whose process is stopping takes nothing new whatever is set of it, and tells the others it
// is draining, at once.
func TestANodeStoppingTellsTheOthersItIsDraining(t *testing.T) {
	settings := &kept{set: domain.NodeSettings{Role: domain.NodeAll, LimitSource: domain.LimitAutomatic, Availability: domain.NodeActive}}
	self, err := Join(t.Context(), settings, uuid.NewV7(), "gpu-1", "http://gpu-1", domain.Encoder{}, 8, &slots{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	self.Stop()
	if n := self.Node(); self.TakesTranscodes() || n.Availability != domain.NodeDraining {
		t.Errorf("stopping: %+v, takes %v; want it draining, taking nothing new", n, self.TakesTranscodes())
	}
	select {
	case <-self.Changes():
	default:
		t.Error("stopping was not told, for the others to be told at once")
	}
}
