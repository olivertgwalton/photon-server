package httpapi

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
)

type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type route struct {
	pattern string
	query   []string
	handle  http.HandlerFunc
}

type API struct {
	logger *slog.Logger
	info   Info
	mux    *http.ServeMux
}

func New(logger *slog.Logger, info Info) *API {
	a := &API{logger: logger, info: info, mux: http.NewServeMux()}
	for _, r := range a.publicRoutes() {
		a.mux.Handle(r.pattern, a.checkQuery(r))
	}
	a.mux.HandleFunc("/", a.unmatched)
	return a
}

func (a *API) publicRoutes() []route {
	return []route{
		{pattern: "GET /api/v1/server", handle: a.server},
	}
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

func (a *API) checkQuery(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.URL.Query() {
			if !slices.Contains(rt.query, name) {
				writeProblem(w, a.logger, codeUnknownParameter, name)
				return
			}
		}
		rt.handle(w, r)
	})
}

// The catch-all "/" takes wrong-method requests too, so 405 is worked out by asking the mux.
func (a *API) unmatched(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	} {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := a.mux.Handler(probe); pattern != "/" {
			allowed = append(allowed, method)
		}
	}
	if len(allowed) == 0 {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeProblem(w, a.logger, codeMethodNotAllowed, "")
}

func (a *API) server(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.info)
}
