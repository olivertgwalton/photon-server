package main

import (
	"testing"

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
