package media

import (
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"
)

func TestABodyWhoseServerStopsSendingIsGivenUpOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, w := io.Pipe()
		go func() {
			if _, err := w.Write([]byte("xyz")); err != nil {
				t.Error(err)
			}
		}()
		_, err := io.ReadAll(stalling(r, remoteStall))
		if !errors.Is(err, errStalled) {
			t.Errorf("%v, want it given up on as stalled", err)
		}
	})
}

// A player that pauses stops reading for longer than a stall, while its server would send.
func TestABodyReadSlowlyIsNotGivenUpOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, w := io.Pipe()
		go func() {
			if _, err := w.Write([]byte("xyz")); err != nil {
				t.Error(err)
			}
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		}()
		body := stalling(r, remoteStall)
		b := make([]byte, 1)
		for range 3 {
			<-time.After(2 * remoteStall)
			if _, err := io.ReadFull(body, b); err != nil {
				t.Fatalf("read after a pause: %v", err)
			}
		}
	})
}
