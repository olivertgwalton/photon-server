package jellyfin

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// slowCluster is slow to say which node runs stuck: it answers once release is closed, by when
// this node runs it. Every other play session it says another node runs.
type slowCluster struct {
	hlsFiles
	stuck   uuid.UUID
	release chan struct{}

	mu    sync.Mutex
	asked map[uuid.UUID]int
}

func (c *slowCluster) Owner(_ context.Context, session uuid.UUID) (domain.Node, bool, error) {
	c.mu.Lock()
	c.asked[session]++
	c.mu.Unlock()
	if session == c.stuck {
		<-c.release
	}
	return domain.Node{}, true, nil
}

func (c *slowCluster) Has(session uuid.UUID) bool {
	select {
	case <-c.release:
		return session == c.stuck
	default:
		return false
	}
}

func (c *slowCluster) times(session uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.asked[session]
}

func openingStuck(t *testing.T) (*slowCluster, func(session uuid.UUID) <-chan bool) {
	t.Helper()
	cluster := &slowCluster{stuck: uuid.NewV7(), release: make(chan struct{}), asked: map[uuid.UUID]int{}}
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{HLS: cluster, Owners: cluster})
	item := uuid.NewV7()
	open := func(session uuid.UUID) <-chan bool {
		done := make(chan bool, 1)
		go func() {
			done <- api.open(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/videos/"+guid(item)+"/master.m3u8", nil), item, session)
		}()
		return done
	}
	return cluster, open
}

// One play session slow to start keeps no other app's from starting on the node.
func TestASlowStartHoldsUpNoOtherPlaySession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cluster, open := openingStuck(t)
		slow := open(cluster.stuck)
		synctest.Wait()
		other := uuid.NewV7()
		if done := <-open(other); done || cluster.times(other) != 1 {
			t.Errorf("another play session: answered %v, its owner asked %d times; want it handed on", done, cluster.times(other))
		}
		close(cluster.release)
		<-slow
	})
}

// Two fetches of a play session's master at once start it once.
func TestAPlaySessionFetchedTwiceAtOnceStartsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cluster, open := openingStuck(t)
		first := open(cluster.stuck)
		synctest.Wait()
		second := open(cluster.stuck)
		synctest.Wait()
		close(cluster.release)
		if a, b := <-first, <-second; a || b || cluster.times(cluster.stuck) != 1 {
			t.Errorf("answered %v and %v, its owner asked %d times; want it started once and both served", a, b, cluster.times(cluster.stuck))
		}
	})
}
