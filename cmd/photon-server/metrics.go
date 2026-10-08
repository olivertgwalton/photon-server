package main

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

// metricsHandler answers what reg gathers. A source that cannot be read, Valkey out of reach say,
// leaves its own metrics out and is logged; the rest are answered.
func metricsHandler(reg *prometheus.Registry, logger *slog.Logger) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn), ErrorHandling: promhttp.ContinueOnError,
	})
}
