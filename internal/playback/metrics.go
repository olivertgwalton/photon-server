package playback

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// scrapeWithin bounds reading what a scrape reports from Valkey or Postgres.
const scrapeWithin = 5 * time.Second

var playbacksDesc = prometheus.NewDesc("photon_playbacks", "The playbacks this node serves now, by how each plays.",
	[]string{"method"}, nil)

func newStarts() *prometheus.CounterVec {
	starts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "photon_playback_starts_total", Help: "The playbacks started by clients asking this node, by how each plays.",
	}, []string{"method"})
	for _, m := range domain.PlayMethods() {
		starts.WithLabelValues(string(m))
	}
	return starts
}

// Opened counts a playback started whose stream opened, once it has: one a node refused, abandoned
// for the next to be asked, is no start.
func (s *Sessions) Opened(method domain.PlayMethod) { s.starts.WithLabelValues(string(method)).Inc() }

func (s *Sessions) Describe(ch chan<- *prometheus.Desc) {
	ch <- playbacksDesc
	s.starts.Describe(ch)
}

func (s *Sessions) Collect(ch chan<- prometheus.Metric) {
	s.starts.Collect(ch)
	ctx, cancel := context.WithTimeout(context.Background(), scrapeWithin)
	defer cancel()
	all, err := s.live.Playbacks(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(playbacksDesc, err)
		return
	}
	serving := map[domain.PlayMethod]int{}
	for _, p := range all {
		if p.Node == s.node {
			serving[p.Method]++
		}
	}
	for _, m := range domain.PlayMethods() {
		ch <- prometheus.MustNewConstMetric(playbacksDesc, prometheus.GaugeValue, float64(serving[m]), string(m))
	}
}

// refusal is why a playback that encodes its video is refused.
type refusal string

const (
	// refusedFull is every node that could encode it encoding as many videos as it may.
	refusedFull refusal = "full"
	// refusedNoEncoder is no node that could: each serves only, is drained, or cannot make what it
	// asks.
	refusedNoEncoder refusal = "no_encoder"
)

func newRefusals() *prometheus.CounterVec {
	refusals := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "photon_transcode_refusals_total", Help: "The playbacks this node refused for want of a node to encode them, by why.",
	}, []string{"reason"})
	for _, r := range []refusal{refusedFull, refusedNoEncoder} {
		refusals.WithLabelValues(string(r))
	}
	return refusals
}

func (p *Placer) Describe(ch chan<- *prometheus.Desc) { p.refusals.Describe(ch) }

func (p *Placer) Collect(ch chan<- prometheus.Metric) { p.refusals.Collect(ch) }
