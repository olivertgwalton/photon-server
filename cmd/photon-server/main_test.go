package main

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Where an admin sets no limit, a node encodes as many videos at once as its encoder keeps up
// with: on hardware, as many as a GeForce driver before 591.44 allows; in software, one per four
// processors, and at least one.
func TestANodeEncodesWhatItsEncoderKeepsUpWith(t *testing.T) {
	if got := automaticTranscodes(domain.AccelNVENC); got != hardwareTranscodes {
		t.Errorf("on NVENC: %d, want %d", got, hardwareTranscodes)
	}
	if got := automaticTranscodes(domain.AccelSoftware); got < 1 {
		t.Errorf("in software: %d, want at least one", got)
	}
}

func TestANodeKeepsItsIDAcrossRestartsWhileItKeepsItsCache(t *testing.T) {
	cache := t.TempDir()
	first, err := nodeID(cache)
	if err != nil {
		t.Fatal(err)
	}
	again, err := nodeID(cache)
	if err != nil || again != first {
		t.Errorf("restarted on the same cache: %v, %v; want %v", again, err, first)
	}
	other, err := nodeID(t.TempDir())
	if err != nil || other == first {
		t.Errorf("on a cache of its own: %v, %v; want a new node", other, err)
	}
}

type stopping struct{ stopped bool }

func (s *stopping) Stop() { s.stopped = true }

type streams struct {
	mu   sync.Mutex
	left []uuid.UUID
}

func (s *streams) Playbacks() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.left
}

func (s *streams) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.left = nil
}

// A node told to stop takes nothing new at once, then serves until its streams have played to
// their end; or until its time is up, or it is told to stop again.
func TestANodeStoppingPlaysItsStreamsToTheirEnd(t *testing.T) {
	quiet := slog.New(slog.DiscardHandler)
	for _, tc := range []struct {
		name string
		// ends is how the drain is ended, waited at most a minute for.
		ends func(s *streams, again chan os.Signal)
		want time.Duration
	}{
		{"its streams end", func(s *streams, _ chan os.Signal) { <-time.After(30 * time.Minute); s.end() }, 30 * time.Minute},
		{"its time is up", func(*streams, chan os.Signal) {}, time.Hour},
		{"told again", func(_ *streams, again chan os.Signal) { <-time.After(time.Minute); again <- os.Interrupt }, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				self, s, again := &stopping{}, &streams{left: []uuid.UUID{uuid.NewV7()}}, make(chan os.Signal, 1)
				go tc.ends(s, again)
				began := time.Now()
				drain(t.Context(), self, s, again, time.Hour, quiet)
				if !self.stopped {
					t.Error("a node draining was not stopped taking new work")
				}
				if took := time.Since(began); took < tc.want || took > tc.want+drainPoll {
					t.Errorf("drained for %v, want about %v", took, tc.want)
				}
			})
		})
	}
}
