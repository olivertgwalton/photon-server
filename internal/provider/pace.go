package provider

import (
	"context"
	"sync"
	"time"
)

// Pace holds each request to a provider back until Every has passed since the last, for one that
// asks to be asked no more often, as MusicBrainz and Open Library ask of an address.
type Pace struct {
	Every time.Duration
	mu    sync.Mutex
	last  time.Time
}

// Wait waits until the next request may go, or ctx ends.
func (p *Pace) Wait(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if d := time.Until(p.last.Add(p.Every)); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	p.last = time.Now()
	return nil
}
