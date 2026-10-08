package hls

import "github.com/prometheus/client_golang/prometheus"

var (
	transcodesDesc = prometheus.NewDesc("photon_transcodes",
		"The videos this node encodes now: for playbacks, and for downloads' conversions.", []string{"kind"}, nil)
	slotsDesc = prometheus.NewDesc("photon_transcode_slots",
		"The most videos this node encodes at once; absent where it has no limit.", nil, nil)
)

// newSegmentWait's buckets run from a segment already made to one whose encode has fallen behind
// the player, past which a player stalls.
func newSegmentWait() prometheus.Histogram {
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "photon_hls_segment_wait_seconds",
		Help:    "How long a request for an HLS segment waits for it to be made.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 4, 8, 15, 30},
	})
}

func (r *Remuxer) Describe(ch chan<- *prometheus.Desc) {
	ch <- transcodesDesc
	ch <- slotsDesc
	r.segmentWait.Describe(ch)
}

func (r *Remuxer) Collect(ch chan<- prometheus.Metric) {
	active, conversions, limit := r.Transcodes()
	ch <- prometheus.MustNewConstMetric(transcodesDesc, prometheus.GaugeValue, float64(active-conversions), "playback")
	ch <- prometheus.MustNewConstMetric(transcodesDesc, prometheus.GaugeValue, float64(conversions), "conversion")
	if limit != Unlimited {
		ch <- prometheus.MustNewConstMetric(slotsDesc, prometheus.GaugeValue, float64(limit))
	}
	r.segmentWait.Collect(ch)
}
