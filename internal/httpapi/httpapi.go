package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// access is who may call a route. Every route says; none is public by omission.
type access string

const (
	public   access = "public"
	signedIn access = "signed_in"
)

type route struct {
	pattern string
	access  access
	query   []string
	handle  http.HandlerFunc
}

type authenticator interface {
	SignIn(ctx context.Context, name, password string, device auth.Device) (string, domain.Profile, error)
	Authenticate(ctx context.Context, token string) (domain.Session, error)
	SignOut(ctx context.Context, session uuid.UUID) error
}

// Services are what the API's routes call.
type Services struct {
	// Ready reports whether everything a request may need is reachable.
	Ready func(context.Context) error
	Auth  authenticator
}

type API struct {
	logger *slog.Logger
	info   Info
	svc    Services
	mux    *http.ServeMux
}

func New(logger *slog.Logger, info Info, svc Services) *API {
	a := &API{logger: logger, info: info, svc: svc, mux: http.NewServeMux()}
	for _, r := range a.routes() {
		h := a.checkQuery(r)
		switch r.access {
		case public:
		case signedIn:
			h = a.requireSession(h)
		}
		a.mux.Handle(r.pattern, h)
	}
	a.mux.HandleFunc("/", a.unmatched)
	return a
}

func (a *API) routes() []route {
	return []route{
		{pattern: "GET /api/v1/server", access: public, handle: a.server},
		{pattern: "GET /readyz", access: public, handle: a.readyz},
		{pattern: "POST /api/v1/auth/login", access: public, handle: a.login},
		{pattern: "POST /api/v1/auth/logout", access: signedIn, handle: a.logout},
		{pattern: "GET /api/v1/me", access: signedIn, handle: a.me},
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

func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Ready(r.Context()); err != nil {
		a.logger.WarnContext(r.Context(), "not ready", slog.Any("err", err))
		writeProblem(w, a.logger, codeNotReady, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
