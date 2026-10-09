package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/identity"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/peer"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/reach"
	"github.com/olivertgwalton/photon-server/internal/sso"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// listJSON is every item there is; pageJSON is total's items from offset.
type listJSON[T any] struct {
	Items []T `json:"items"`
}

type pageJSON[T any] struct {
	Items  []T   `json:"items"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type createdJSON struct {
	ID uuid.UUID `json:"id"`
}

type reachedJSON struct {
	Reach domain.Reach `json:"reach"`
}

// itemIDsJSON is titles, in order.
type itemIDsJSON struct {
	ItemIDs []uuid.UUID `json:"item_ids"`
}

type positionJSON struct {
	PositionMS int64 `json:"position_ms"`
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
	// manages is a route for an admin, or a manager over the profiles it keeps.
	manages access = "manages"
	// localNetwork is a route answered only to a client on the server's local networks, and to
	// any other as though it were not there. It is an operator's, and in no description.
	localNetwork access = "local_network"
)

// route is a route and what the API's description says of it. A body, reply or refusal is a value
// of its type; a path wildcard not in path is an id.
type route struct {
	pattern string
	access  access
	summary string
	path    []param
	query   []param
	body    any
	// status is what success answers, with reply; 204 and 202 answer nothing.
	status int
	reply  any
	// again is what success answers, with reply, to a request already met.
	again int
	// refusals are problems with more to say than their code, by status.
	refusals map[int]any
	// delivery is how a route sends media, counted as it is sent by this node; none for one that
	// sends none.
	delivery playback.Delivery
	handle   http.HandlerFunc
}

type authenticator interface {
	auth.Authenticator
	SetUp(ctx context.Context, name, password string, device auth.Device) (string, domain.Profile, error)
	SignInAs(ctx context.Context, profile domain.Profile, identity domain.SignInIdentity, device auth.Device) (string, error)
	SwitchProfile(ctx context.Context, session domain.Session, target uuid.UUID, secret string) (domain.Profile, error)
	StartReset(ctx context.Context, name string) (string, error)
	RedeemReset(ctx context.Context, code, password string) (uuid.UUID, error)
	SetPIN(ctx context.Context, profile uuid.UUID, pin string) error
	ChangePassword(ctx context.Context, session domain.Session, current, password string) error
	Devices(ctx context.Context, session domain.Session) ([]store.DeviceListing, error)
	SignOutDevice(ctx context.Context, session domain.Session, device uuid.UUID) error
	CreateKey(ctx context.Context, creator domain.Session, name string) (uuid.UUID, string, error)
	Keys(ctx context.Context) ([]store.KeyListing, error)
	RevokeKey(ctx context.Context, id uuid.UUID) error
}

type profileLister interface {
	Profiles(ctx context.Context) ([]store.ProfileListing, error)
	HasProfiles(ctx context.Context) (bool, error)
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
	// Preferences are how each profile plays, and the tracks it last chose for each title.
	Preferences preferences
	// Playlists are each profile's own.
	Playlists playlists
	// Collections are box sets, a provider's and an admin's.
	Collections collections
	// Providers are the metadata providers the server has, and ProviderSettings what an admin set
	// of them.
	Providers        providerList
	ProviderSettings providerSettings
	// Plugins are the metadata plugins an admin registered.
	Plugins pluginAdmin
	// Avatars are the profiles' pictures, kept in Artwork.
	Avatars avatars
	// ProfileAdmin adds, changes and removes the household's profiles.
	ProfileAdmin profileAdmin
	Tasks        tasks
	Jobs         jobQueue
	// Backups restores one of this node's dumps, every node stopping for it.
	Backups backupRestores
	// Maintenance is when work that reads media, and that no one waits on, is done.
	Maintenance maintenanceSettings
	// Activity is the log of what has happened, and Events tells it, and more, as it happens.
	Activity activityLog
	Events   eventHub
	// Audience says which of the events each profile is told.
	Audience audience
	// Webhooks are the addresses told of events.
	Webhooks webhooks
	// Importer starts imports of other servers' watch history, and HistoryImports are how each went.
	Importer       importer
	HistoryImports importList
	// Trackers are the profiles' accounts on Trakt, Simkl and MDBList, linked through the apps an
	// admin registered there, which TrackerClients are.
	Trackers       trackerLinks
	TrackerClients trackerClients
	// SignIns are the OpenID Connect providers the household signs in through, and the accounts
	// each profile linked at them.
	SignIns signIns
	// NowPlaying is every playback going on, across the cluster.
	NowPlaying nowPlaying
	Pictures   pictures
	// Themes are titles' theme tunes, those from the theme host kept in Artwork.
	Themes   themes
	Watching watching
	Playing  playing
	Parts    parts
	Copies   copies
	Discover discover
	// Subtitles are the subtitles fetched from providers for copies.
	Subtitles fetchedSubtitles
	Playbacks playbacks
	// Downloads are each profile's, and Conversions make the ones not downloaded as they are.
	Downloads   downloads
	Conversions conversions
	// Placer chooses the node that encodes a playback, and NodeKey checks another node's asking
	// this one to open a remux.
	Placer  placer
	NodeKey nodecall.Key
	HLS     hlsFiles
	// Owners say which node of the cluster serves a playback's HLS.
	Owners owners
	// Signer signs the addresses titles play from.
	Signer  playback.Signer
	Artwork pictureCache
	// Previews are parts' chapter images and trickplay sheets, as recorded and as files.
	Previews     previews
	PreviewFiles previewFiles
	Limits       kv.Limiter
	// Web is the web app, served for every path the API does not own; nil serves the API alone.
	Web *Web
	// Reach is how clients reach the server: the proxies trusted to name the client, and its
	// address outside.
	Reach *reach.Reach
	// Network is how the server is reached, and Secure how this node serves it now; nil Secure
	// never sends a plain request to HTTPS.
	Network networkSettings
	// Storage is where artwork and previews are kept, and Stores this node's, kept there now.
	Storage storageSettings
	// Nodes are the server's nodes as an admin sets them.
	Nodes  nodeSettings
	Stores stores
	Secure secureConnections
	// Jellyfin is this node's serving of Jellyfin's API; nil where it is not run.
	Jellyfin jellyfinListener
	// Identity is what the server is called and what its metadata is asked in, and ServerSettings
	// where an admin sets them.
	Identity       *identity.Server
	ServerSettings serverSettings
	// Setup is how this node was started, and Postgres and Valkey what it reaches.
	Setup    Setup
	Postgres versioned
	Valkey   cluster
	// Metrics are this node's, and Sent counts the media counted in them.
	Metrics prometheus.Gatherer
	Sent    *playback.Sent
}

type API struct {
	logger *slog.Logger
	info   domain.Info
	svc    Services
	mux    *http.ServeMux
	// handler is mux, behind what Secure asks of a plain request.
	handler http.Handler
	// scrape answers Metrics in Prometheus' text format.
	scrape http.Handler
	// description is the API's OpenAPI description, made once.
	description []byte
	// reads are this node's reads of library roots, for checks of them.
	reads *rootReads
}

func New(logger *slog.Logger, info domain.Info, svc Services) *API {
	a := &API{
		logger: logger, info: info, svc: svc, mux: http.NewServeMux(), scrape: scrapeHandler(svc.Metrics, logger),
		reads: &rootReads{read: readRoot},
	}
	routes := a.routes()
	var err error
	if a.description, err = describe(info, routes); err != nil {
		panic("httpapi: " + err.Error())
	}
	for _, r := range routes {
		h := a.checkQuery(r)
		// Within routeToOwner, so a request handed to another node is counted there alone.
		if r.delivery != "" {
			h = a.svc.Sent.Counting(r.delivery, h)
		}
		switch r.access {
		case public:
		case signedIn:
			h = a.requireSession(h)
		case signedAddress:
			h = a.requireSignature(h)
		case signedPath:
			h = a.requireSignedPath(a.routeToOwner("playback", h))
		case admin:
			h = a.requireAdmin(h)
		case manages:
			h = a.requireManager(h)
		case localNetwork:
			h = a.requireLocalNetwork(h)
		}
		switch r.reply.(type) {
		case asFile, asStream:
		default:
			h = a.revalidate(h)
		}
		a.mux.Handle(r.pattern, h)
	}
	// Another node asking this one to open a remux, for its metrics, or whether it can read a
	// library's root, is no client's to call, and so in no description.
	a.mux.Handle("POST /api/v1/internal/playbacks/{id}/remux", a.svc.NodeKey.Verify(http.HandlerFunc(a.openRemote)))
	a.mux.Handle("GET "+metricsPath, a.svc.NodeKey.Verify(http.HandlerFunc(a.nodeMetrics)))
	a.mux.Handle("GET "+libraryCheckPath, a.svc.NodeKey.Verify(http.HandlerFunc(a.nodeCheckLibrary)))
	// A sign-in provider sends a browser back here with a query of its own, which a client never
	// calls.
	a.mux.HandleFunc("GET "+sso.CallbackPattern, a.signInCallback)
	a.mux.HandleFunc("/", a.unmatched)
	a.handler = a.mux
	if svc.Secure != nil {
		a.handler = a.requireHTTPS(a.mux)
	}
	return a
}

func (a *API) routes() []route {
	return slices.Concat(
		a.serverRoutes(),
		a.vocabularyRoutes(),
		a.openAPIRoutes(),
		a.metricsRoutes(),
		a.sessionRoutes(),
		a.setupRoutes(),
		a.pairingRoutes(),
		a.resetRoutes(),
		a.profilesRoutes(),
		a.preferencesRoutes(),
		a.catalogueRoutes(),
		a.avatarsRoutes(),
		a.devicesRoutes(),
		a.collectionsRoutes(),
		a.peopleRoutes(),
		a.historyRoutes(),
		a.playlistsRoutes(),
		a.editRoutes(),
		a.artworkEditRoutes(),
		a.markersRoutes(),
		a.watchRoutes(),
		a.subtitlesRoutes(),
		a.playRoutes(),
		a.hlsRoutes(),
		a.downloadsRoutes(),
		a.streamsRoutes(),
		a.styledRoutes(),
		a.previewsRoutes(),
		a.adminRoutes(),
		a.foldersRoutes(),
		a.accessRoutes(),
		a.profileAdminRoutes(),
		a.providersRoutes(),
		a.networkRoutes(),
		a.nodesRoutes(),
		a.storageRoutes(),
		a.keysRoutes(),
		a.pluginsRoutes(),
		a.workRoutes(),
		a.backupsRoutes(),
		a.maintenanceRoutes(),
		a.activityRoutes(),
		a.feedRoutes(),
		a.webhooksRoutes(),
		a.importsRoutes(),
		a.trackersRoutes(),
		a.signInRoutes(),
		a.calendarRoutes(),
		a.artworkRoutes(),
		a.themesRoutes(),
	)
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *API) checkQuery(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.URL.Query() {
			if !slices.ContainsFunc(rt.query, func(p param) bool { return p.name == name }) {
				writeProblem(w, a.logger, codeUnknownParameter, name)
				return
			}
		}
		rt.handle(w, r)
	})
}

// The catch-all "/" takes wrong-method requests too, so 405 is worked out by asking the mux.
func (a *API) unmatched(w http.ResponseWriter, r *http.Request) {
	if a.svc.Web != nil && !ownedByAPI(r.URL.Path) {
		a.serveWeb(w, r)
		return
	}
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

func (a *API) requireLocalNetwork(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		local, err := a.local(r)
		switch {
		case err != nil:
			a.internal(w, r, err)
		case !local:
			writeProblem(w, a.logger, codeNotFound, "")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// local is whether the client is on the server's local networks.
func (a *API) local(r *http.Request) (bool, error) {
	n, err := a.svc.Network.Network(r.Context())
	if err != nil {
		return false, err
	}
	return peer.LocalIn(n.LocalNetworks, a.svc.Reach.Client(r)), nil
}
