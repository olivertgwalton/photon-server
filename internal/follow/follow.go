// Package follow keeps a node applying what an admin sets, as every node is told of a change.
package follow

import (
	"context"
	"slices"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// resubscribeAfter is the wait before following events again once a stream has ended.
const resubscribeAfter = time.Second

// Events calls reread once subscribed, so nothing told between the two is missed; again as
// subscribe streams an event of one of kinds, and every interval, which takes up a change whose
// event was lost; until ctx ends. A stream that ends, as one that fell behind is, is subscribed to
// again after resubscribeAfter, and reread called again.
func Events(ctx context.Context, subscribe func() (<-chan domain.Event, func()), every time.Duration,
	reread func(context.Context), kinds ...domain.EventKind,
) {
	t := time.NewTicker(every)
	defer t.Stop()
	for ctx.Err() == nil {
		events, stop := subscribe()
		reread(ctx)
		follow(ctx, events, t.C, reread, kinds)
		stop()
	}
}

func follow(ctx context.Context, events <-chan domain.Event, tick <-chan time.Time, reread func(context.Context), kinds []domain.EventKind) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			reread(ctx)
		case e, ok := <-events:
			if !ok {
				select {
				case <-ctx.Done():
				case <-time.After(resubscribeAfter):
				}
				return
			}
			if slices.Contains(kinds, e.Kind) {
				reread(ctx)
			}
		}
	}
}
