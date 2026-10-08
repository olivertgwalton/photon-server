package hls

import "github.com/prometheus/client_golang/prometheus"

var (
	transcodesDesc = prometheus.NewDesc("photon_transcodes",
		"The videos this node encodes now: for playbacks, and for downloads' conversions.", []string{"kind"}, nil)
	slotsDesc = prometheus.NewDesc("photon_transcode_slots",
		"The most videos this node encodes at once; absent where it has no limit.", nil, nil)
)

func (r *Remuxer) Describe(ch chan<- *prometheus.Desc) {
	ch <- transcodesDesc
	ch <- slotsDesc
}

func (r *Remuxer) Collect(ch chan<- prometheus.Metric) {
	active, conversions, limit := r.Transcodes()
	ch <- prometheus.MustNewConstMetric(transcodesDesc, prometheus.GaugeValue, float64(active-conversions), "playback")
	ch <- prometheus.MustNewConstMetric(transcodesDesc, prometheus.GaugeValue, float64(conversions), "conversion")
	if limit != Unlimited {
		ch <- prometheus.MustNewConstMetric(slotsDesc, prometheus.GaugeValue, float64(limit))
	}
}
