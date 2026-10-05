package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
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
	// signedAddress is a route reached by an address the server signed, for players that send
	// no headers of their own.
	signedAddress access = "signed_address"
	// signedPath is an HLS route whose path carries its playback's signature.
	signedPath access = "signed_path"
	admin      access = "admin"
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
	StartPairing(ctx context.Context, d auth.Device) (auth.PairingStart, error)
	ApprovePairing(ctx context.Context, approver domain.Session, userCode string) (auth.Device, error)
	PollPairing(ctx context.Context, deviceCode string) (kv.PairingState, string, domain.Profile, error)
	SwitchProfile(ctx context.Context, session domain.Session, target uuid.UUID, secret string) (domain.Profile, error)
	SetPIN(ctx context.Context, profile uuid.UUID, pin string) error
	Devices(ctx context.Context, session domain.Session) ([]store.DeviceListing, error)
	SignOutDevice(ctx context.Context, session domain.Session, device uuid.UUID) error
}

type limiter interface {
	Allow(ctx context.Context, key string, l kv.Limit) (time.Duration, error)
}

type profileLister interface {
	Profiles(ctx context.Context) ([]store.ProfileListing, error)
}

// Services are what the API's routes call.
type Services struct {
	// Ready reports whether everything a request may need is reachable.
	Ready     func(context.Context) error
	Auth      authenticator
	Profiles  profileLister
	Catalogue catalogue
	Libraries libraryAdmin
	// History is the plays each profile has finished.
	History history
	// Editing is an admin's say over what a title is.
	Editing editing
	// People are those credited on titles, and PersonDescriber says who they are.
	People          people
	PersonDescriber personDescriber
	// Playlists are each profile's own.
	Playlists playlists
	// Collections are box sets, a provider's and an admin's.
	Collections collections
	// Providers are the metadata providers the server has, and ProviderSettings what an admin set
	// of them.
	Providers        providerList
	ProviderSettings providerSettings
	// ProfileAdmin adds, changes and removes the household's profiles.
	ProfileAdmin profileAdmin
	Tasks        tasks
	Jobs         jobQueue
	// NowPlaying is every playback going on, across the cluster.
	NowPlaying nowPlaying
	Pictures   pictures
	Watching   watching
	Playing    playing
	Playbacks  playbacks
	Remuxing   remuxing
	HLS        hlsFiles
	// Owners say which node serves a playback's HLS, nil on a server of one node.
	Owners owners
	// Signer signs the addresses titles play from.
	Signer  playback.Signer
	Artwork pictureCache
	Limits  limiter
	// TrustedProxies are the peers whose X-Forwarded-For names the client. None by default.
	TrustedProxies []netip.Prefix
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
		case signedAddress:
			h = a.requireSignature(h)
		case signedPath:
			h = a.requireSignedPath(a.routeToOwner(h))
		case admin:
			h = a.requireAdmin(h)
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
		{pattern: "POST /api/v1/auth/device/start", access: public, handle: a.startPairing},
		{pattern: "POST /api/v1/auth/device/approve", access: signedIn, handle: a.approvePairing},
		{pattern: "POST /api/v1/auth/device/poll", access: public, handle: a.pollPairing},
		{pattern: "GET /api/v1/profiles", access: signedIn, handle: a.profiles},
		{pattern: "PUT /api/v1/session/profile", access: signedIn, handle: a.switchProfile},
		{pattern: "PUT /api/v1/me/pin", access: signedIn, handle: a.setPIN},
		{pattern: "DELETE /api/v1/me/pin", access: signedIn, handle: a.clearPIN},
		{pattern: "GET /api/v1/auth/devices", access: signedIn, handle: a.devices},
		{pattern: "DELETE /api/v1/auth/devices/{id}", access: signedIn, handle: a.signOutDevice},
		{pattern: "GET /api/v1/libraries", access: signedIn, handle: a.libraries},
		{
			pattern: "GET /api/v1/libraries/{id}/titles", access: signedIn,
			query: append([]string{"sort", "order", "offset", "limit"}, wallFilterParameters...), handle: a.wall,
		},
		{pattern: "GET /api/v1/libraries/{id}/letters", access: signedIn, query: wallFilterParameters, handle: a.letters},
		{pattern: "GET /api/v1/libraries/{id}/facets", access: signedIn, handle: a.facets},
		{pattern: "GET /api/v1/libraries/{id}/collections", access: signedIn, query: []string{"offset", "limit"}, handle: a.libraryCollections},
		{pattern: "GET /api/v1/titles/{id}/members", access: signedIn, handle: a.members},
		{pattern: "GET /api/v1/titles/{id}/similar", access: signedIn, handle: a.similar},
		{pattern: "GET /api/v1/people/{id}", access: signedIn, handle: a.person},
		{pattern: "GET /api/v1/history", access: signedIn, query: []string{"offset", "limit"}, handle: a.ownHistory},
		{pattern: "GET /api/v1/admin/history", access: admin, query: []string{"profile", "offset", "limit"}, handle: a.adminHistory},
		{pattern: "GET /api/v1/playlists", access: signedIn, handle: a.playlistsOf},
		{pattern: "POST /api/v1/playlists", access: signedIn, handle: a.addPlaylist},
		{pattern: "PATCH /api/v1/playlists/{id}", access: signedIn, handle: a.setPlaylist},
		{pattern: "DELETE /api/v1/playlists/{id}", access: signedIn, handle: a.removePlaylist},
		{pattern: "GET /api/v1/playlists/{id}/entries", access: signedIn, query: []string{"offset", "limit"}, handle: a.playlistEntries},
		{pattern: "POST /api/v1/playlists/{id}/entries", access: signedIn, handle: a.addToPlaylist},
		{pattern: "PUT /api/v1/playlists/{id}/entries/{entry}/position", access: signedIn, handle: a.moveEntry},
		{pattern: "DELETE /api/v1/playlists/{id}/entries/{entry}", access: signedIn, handle: a.removeEntry},
		{pattern: "PATCH /api/v1/admin/titles/{id}", access: admin, handle: a.editTitle},
		{pattern: "DELETE /api/v1/admin/titles/{id}/edits", access: admin, query: []string{"field"}, handle: a.resetEdits},
		{pattern: "GET /api/v1/admin/titles/{id}/candidates", access: admin, query: []string{"provider", "title", "year"}, handle: a.candidates},
		{pattern: "PUT /api/v1/admin/titles/{id}/match", access: admin, handle: a.pinMatch},
		{pattern: "PUT /api/v1/admin/titles/{id}/episode-order", access: admin, handle: a.setEpisodeOrder},
		{pattern: "POST /api/v1/admin/collections", access: admin, handle: a.addCollection},
		{pattern: "PUT /api/v1/admin/collections/{id}/members", access: admin, handle: a.setMembers},
		{pattern: "DELETE /api/v1/admin/collections/{id}", access: admin, handle: a.removeCollection},
		{pattern: "GET /api/v1/titles/{id}", access: signedIn, handle: a.title},
		{pattern: "PUT /api/v1/titles/{id}/progress", access: signedIn, handle: a.progress},
		{pattern: "PUT /api/v1/titles/{id}/watched", access: signedIn, handle: a.mark(watching.MarkWatched)},
		{pattern: "DELETE /api/v1/titles/{id}/watched", access: signedIn, handle: a.mark(watching.MarkUnwatched)},
		{pattern: "PUT /api/v1/titles/{id}/favourite", access: signedIn, handle: a.mark(watching.Favourite)},
		{pattern: "DELETE /api/v1/titles/{id}/favourite", access: signedIn, handle: a.mark(watching.Unfavourite)},
		{pattern: "POST /api/v1/titles/{id}/play", access: signedIn, handle: a.play},
		{pattern: "POST /api/v1/playback/{id}/progress", access: signedIn, handle: a.playbackProgress},
		{pattern: "POST /api/v1/playback/{id}/stop", access: signedIn, handle: a.playbackStop},
		{pattern: "GET /api/v1/hls/{playback}/{exp}/{sig}/{file}", access: signedPath, handle: a.hlsFile},
		{pattern: "GET /api/v1/parts/{id}/stream", access: signedAddress, query: []string{"exp", "sig"}, handle: a.partStream},
		{pattern: "GET /api/v1/admin/libraries", access: admin, handle: a.adminLibraries},
		{pattern: "POST /api/v1/admin/libraries", access: admin, handle: a.addLibrary},
		{pattern: "PATCH /api/v1/admin/libraries/{id}", access: admin, handle: a.setLibrary},
		{pattern: "DELETE /api/v1/admin/libraries/{id}", access: admin, handle: a.removeLibrary},
		{pattern: "POST /api/v1/admin/libraries/{id}/scan", access: admin, handle: a.scanLibrary},
		{pattern: "GET /api/v1/subtitles/{id}/file", access: signedAddress, query: []string{"exp", "sig"}, handle: a.subtitleFile},
		{pattern: "POST /api/v1/admin/profiles", access: admin, handle: a.addProfile},
		{pattern: "PATCH /api/v1/admin/profiles/{id}", access: admin, handle: a.setProfile},
		{pattern: "DELETE /api/v1/admin/profiles/{id}", access: admin, handle: a.removeProfile},
		{pattern: "GET /api/v1/admin/profiles/{id}/access", access: admin, handle: a.profileAccess},
		{pattern: "PUT /api/v1/admin/profiles/{id}/access", access: admin, handle: a.setProfileAccess},
		{pattern: "GET /api/v1/admin/providers", access: admin, handle: a.adminProviders},
		{pattern: "PATCH /api/v1/admin/providers/{id}", access: admin, handle: a.setProvider},
		{pattern: "GET /api/v1/admin/tasks", access: admin, handle: a.adminTasks},
		{pattern: "POST /api/v1/admin/tasks/{key}/run", access: admin, handle: a.runTask},
		{pattern: "GET /api/v1/admin/jobs", access: admin, handle: a.adminJobs},
		{pattern: "POST /api/v1/admin/jobs/{id}/retry", access: admin, handle: a.retryJob},
		{pattern: "GET /api/v1/admin/playbacks", access: admin, handle: a.adminPlaybacks},
		{pattern: "GET /api/v1/home", access: signedIn, query: []string{"limit"}, handle: a.home},
		{pattern: "GET /api/v1/search", access: signedIn, query: []string{"q", "library", "limit"}, handle: a.search},
		{pattern: "GET /api/v1/artwork/{id}", access: public, query: []string{"width"}, handle: a.artwork},
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
