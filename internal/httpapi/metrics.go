package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// scrapeHandler answers what g gathers. A source that cannot be read, Valkey out of reach say,
// leaves its own metrics out and is logged; the rest are answered.
func scrapeHandler(g prometheus.Gatherer, logger *slog.Logger) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn), ErrorHandling: promhttp.ContinueOnError,
	})
}

func (a *API) metrics(w http.ResponseWriter, r *http.Request) { a.scrape.ServeHTTP(w, r) }
