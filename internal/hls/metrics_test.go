package hls

import (
	"log/slog"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Each segment served is timed, from its asking to its being made.
func TestEachSegmentServedIsTimed(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{{
		Open: opening("testdata/fragments.mp4"), Part: Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	for _, n := range []int{0, 1} {
		f, err := r.Segment(t.Context(), playback, n)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(r)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "photon_hls_segment_wait_seconds" {
			if got := f.GetMetric()[0].GetHistogram().GetSampleCount(); got != 2 {
				t.Errorf("%d segments timed, want 2", got)
			}
			return
		}
	}
	t.Error("no photon_hls_segment_wait_seconds")
}

// An operator sees what a node encodes, a playback's apart from a download's, of how many at once
// it may; a node with no limit says none.
func TestANodesMetricsSayWhatItEncodesOfHowMany(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 3, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Open(t.Context(), uuid.NewV7(), transcode); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.HoldConversion(t.Context()); !ok {
		t.Fatal("no slot for a conversion")
	}
	const help = `# HELP photon_transcodes The videos this node encodes now: for playbacks, and for downloads' conversions.
# TYPE photon_transcodes gauge
photon_transcodes{kind="conversion"} 1
photon_transcodes{kind="playback"} 1
`
	limited := help + `# HELP photon_transcode_slots The most videos this node encodes at once; absent where it has no limit.
# TYPE photon_transcode_slots gauge
photon_transcode_slots 3
`
	if err := testutil.CollectAndCompare(r, strings.NewReader(limited), "photon_transcodes", "photon_transcode_slots"); err != nil {
		t.Error(err)
	}
	r.SetLimit(Unlimited)
	if err := testutil.CollectAndCompare(r, strings.NewReader(help), "photon_transcodes", "photon_transcode_slots"); err != nil {
		t.Error(err)
	}
}
