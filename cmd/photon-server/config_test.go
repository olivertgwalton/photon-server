package main

import (
	"slices"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
)

// An encoder an admin sets is tried alone: on the device named, a second NVIDIA card too, which
// is not tried of itself, or on every device of its acceleration where none is named; software
// tries none.
func TestAnEncoderSetIsTheOneTried(t *testing.T) {
	all := []hls.Hardware{
		{Accel: domain.AccelNVENC, Device: "0"},
		{Accel: domain.AccelQSV, Device: "/dev/dri/renderD128"},
		{Accel: domain.AccelQSV, Device: "/dev/dri/renderD129"},
		{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128"},
		{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD129"},
	}
	for _, tc := range []struct {
		choice domain.EncoderChoice
		want   []hls.Hardware
	}{
		{domain.EncoderChoice{Source: domain.EncoderSet, Acceleration: domain.AccelNVENC, Device: "1"}, []hls.Hardware{{Accel: domain.AccelNVENC, Device: "1"}}},
		{domain.EncoderChoice{Source: domain.EncoderSet, Acceleration: domain.AccelVAAPI, Device: "/dev/dri/renderD129"}, all[4:5]},
		{domain.EncoderChoice{Source: domain.EncoderSet, Acceleration: domain.AccelQSV}, all[1:3]},
		{domain.EncoderChoice{Source: domain.EncoderSet, Acceleration: domain.AccelSoftware}, nil},
	} {
		if got := chosen(all, tc.choice); !slices.Equal(got, tc.want) {
			t.Errorf("%+v: tried %v, want %v", tc.choice, got, tc.want)
		}
	}
}
