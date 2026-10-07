package follow

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A node reads what is set as it starts following, as it is told of a change of what it follows
// and of nothing else, every interval, and once more each time its stream is back after ending.
func TestANodeRereadsAsToldEveryIntervalAndOnceBackFromALostStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var reads, subscriptions atomic.Int32
		streams := make(chan chan domain.Event, 2)
		first, second := make(chan domain.Event), make(chan domain.Event)
		streams <- first
		streams <- second
		subscribe := func() (<-chan domain.Event, func()) {
			subscriptions.Add(1)
			return <-streams, func() {}
		}
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			Events(ctx, subscribe, time.Minute, func(context.Context) { reads.Add(1) }, domain.EventNetworkChanged)
		}()
		want := func(n int32, after string) {
			t.Helper()
			synctest.Wait()
			if got := reads.Load(); got != n {
				t.Errorf("read %d times %s, want %d", got, after, n)
			}
		}
		want(1, "once following")
		first <- domain.Event{Kind: domain.EventNetworkChanged}
		want(2, "once told of a change")
		first <- domain.Event{Kind: domain.EventLibraryChanged}
		want(2, "once told of something else")
		<-time.After(time.Minute)
		want(3, "a minute on")
		close(first)
		<-time.After(resubscribeAfter)
		want(4, "once the stream was back")
		if n := subscriptions.Load(); n != 2 {
			t.Errorf("subscribed %d times, want once more after the stream ended", n)
		}
		stop()
		<-done
	})
}
