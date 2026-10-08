package hls

import "context"

// conversion is a download's conversion holding a transcode slot until it ends or a playback
// takes the slot.
type conversion struct{ stop context.CancelCauseFunc }

// HoldConversion gives a download's conversion a transcode slot for as long as no playback needs
// it: ok is false where every slot is held, and held is cancelled with ErrPreempted when a playback
// takes the slot. release gives the slot back once the conversion ends.
func (r *Remuxer) HoldConversion(ctx context.Context) (held context.Context, release func(), ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full() {
		return nil, nil, false
	}
	held, stop := context.WithCancelCause(ctx)
	c := &conversion{stop: stop}
	r.conversions[c] = struct{}{}
	r.change()
	return held, func() {
		r.mu.Lock()
		if _, ok := r.conversions[c]; ok {
			delete(r.conversions, c)
			r.change()
		}
		r.mu.Unlock()
		stop(nil)
	}, true
}

// preempt stops a conversion holding a slot, answering whether there was one; the caller holds
// r.mu. Its slot is free as this returns, not once its ffmpeg has gone.
func (r *Remuxer) preempt() bool {
	for c := range r.conversions {
		delete(r.conversions, c)
		c.stop(ErrPreempted)
		return true
	}
	return false
}

// SetLimit changes how many videos may be encoded at once, or Unlimited. Those encoding beyond a
// lower limit go on; none is begun until there is room under it.
func (r *Remuxer) SetLimit(limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit != r.limit {
		r.limit = limit
		r.change()
	}
}

// Changes is told as a transcode slot is taken or given back, once for any number since it was
// last read.
func (r *Remuxer) Changes() <-chan struct{} { return r.changed }

// change tells Changes, without waiting for it to be read.
func (r *Remuxer) change() {
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

// Transcodes answers how many videos this node is encoding, how many of those are conversions,
// and the most that may be at once.
func (r *Remuxer) Transcodes() (active, conversions, limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transcodes(), len(r.conversions), r.limit
}

// full is whether every transcode slot is held; the caller holds r.mu.
func (r *Remuxer) full() bool { return r.limit != Unlimited && r.transcodes() >= r.limit }

// transcodes counts the remuxes encoding video and the conversions; the caller holds r.mu. The
// sessions are the one account of what playback is encoding, as a remux leaves them however its
// playback ends, or where it is never opened.
func (r *Remuxer) transcodes() int {
	n := len(r.conversions)
	for _, s := range r.sessions {
		if s.encodes() {
			n++
		}
	}
	return n
}
