package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// metrics are this node's, for Prometheus: the Go runtime's, the process's, the build's, and what
// each of cs says.
func metrics(version string, cs ...prometheus.Collector) *prometheus.Registry {
	build := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "photon_build_info", Help: "The version of photon-server this node runs.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	build.Set(1)
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), build)
	reg.MustRegister(cs...)
	return reg
}
