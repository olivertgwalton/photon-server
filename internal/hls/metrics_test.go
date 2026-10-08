package hls

import (
	"log/slog"
	"strings"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

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
