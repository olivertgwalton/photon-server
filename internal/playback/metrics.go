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

func (s *Sessions) Describe(ch chan<- *prometheus.Desc) {
	ch <- playbacksDesc
}

func (s *Sessions) Collect(ch chan<- prometheus.Metric) {
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
