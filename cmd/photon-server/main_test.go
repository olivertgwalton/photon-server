package main

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
)

func TestAnOperatorSetsHowManyVideosAreEncodedAtOnce(t *testing.T) {
	for _, tc := range []struct {
		env     string
		accel   domain.Acceleration
		want    int
		invalid bool
	}{
		{env: "3", accel: domain.AccelNVENC, want: 3},
		{env: "unlimited", accel: domain.AccelSoftware, want: hls.Unlimited},
		{env: "", accel: domain.AccelNVENC, want: hardwareTranscodes},
		{env: "0", invalid: true},
		{env: "-2", invalid: true},
		{env: "lots", invalid: true},
	} {
		t.Setenv("PHOTON_MAX_TRANSCODES", tc.env)
		got, err := maxTranscodes(tc.accel)
		if tc.invalid != (err != nil) || got != tc.want {
			t.Errorf("PHOTON_MAX_TRANSCODES=%q on %s: %d, %v; want %d, invalid %t", tc.env, tc.accel, got, err, tc.want, tc.invalid)
		}
	}
	t.Setenv("PHOTON_MAX_TRANSCODES", "")
	if got, _ := maxTranscodes(domain.AccelSoftware); got < 1 {
		t.Errorf("software by default: %d, want at least one", got)
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
