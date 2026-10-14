package main

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/testtool"
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

// A VAAPI overlay that will not burn a test subtitle in is given up for the CPU's at start; a device
// whose one overlay will not keeps it, and keeps encoding.
func TestAnOverlayThatWillNotBurnInIsFoundAtStart(t *testing.T) {
	const vaapi = "/dev/dri/renderD128"
	for _, tc := range []struct {
		name, script string
		hw, want     hls.Hardware
	}{
		{
			"vulkan fails", `case "$*" in *overlay_vulkan*) exit 1;; esac`,
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayVulkan},
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayCPU},
		},
		{
			"overlay_vaapi fails", `case "$*" in *overlay_vaapi*) exit 1;; esac`,
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayVAAPI},
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayCPU},
		},
		{
			"overlay_vaapi burns in", "exit 0",
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayVAAPI},
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayVAAPI},
		},
		{
			"nothing burns in", "exit 1",
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayVulkan},
			hls.Hardware{Accel: domain.AccelVAAPI, Device: vaapi, Overlay: hls.OverlayCPU},
		},
		{
			"overlay_cuda fails", "exit 1",
			hls.Hardware{Accel: domain.AccelNVENC, Device: "0"},
			hls.Hardware{Accel: domain.AccelNVENC, Device: "0"},
		},
	} {
		ffmpeg := testtool.Script(t, t.TempDir(), "ffmpeg", tc.script+"\n")
		if got := burning(t.Context(), ffmpeg, tc.hw, slog.New(slog.DiscardHandler)); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
