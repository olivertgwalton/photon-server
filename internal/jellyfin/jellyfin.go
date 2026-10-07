// Package jellyfin answers Jellyfin 12.2's API, on a port of its own, so the apps made for Jellyfin
// (Infuse, Swiftfin, Findroid, Jellyfin's own) can use the server. It speaks Jellyfin's words to
// them and photon's services underneath; none of its words reach photon's own API.
package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/peer"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// version is the Jellyfin the server answers as. Apps compare it as three numbers, and the
// Android ones refuse a server that is not "Jellyfin Server".
const (
	version = "12.2.0"
	product = "Jellyfin Server"
)

type authenticator interface {
	SignIn(ctx context.Context, name, password string, device auth.Device) (string, domain.Profile, error)
	Authenticate(ctx context.Context, token string) (domain.Session, error)
	SignOut(ctx context.Context, session uuid.UUID) error
}

type Services struct {
	Auth      authenticator
	Limits    kv.Limiter
	Raise     func(ctx context.Context, e domain.Event)
	Proxies   peer.Proxies
	Catalogue catalogue
	Pictures  pictureFiles
	Playing   playing
	Playbacks playbacks
	Watching  watching
	// HLS is this node's remuxes, which Placer opens on the node it chooses and Owners find on
	// whichever node runs them; Signer signs a TranscodingUrl's plan and the addresses of another
	// node's HLS.
	HLS    hlsFiles
	Placer placer
	Owners owners
	Signer playback.Signer
	// Encoding is what video is made with for an app: HEVC where it plays it, and styled
	// subtitles drawn in.
	Encoding playback.Encoding
	// Network is how the server is reached, of which its limit on a remote stream's bitrate.
	Network settings
}

type API struct {
	logger *slog.Logger
	svc    Services
	// id is the server's, as Jellyfin writes a Guid: 32 lowercase hex digits. Apps key the server
	// on it, so every node answers the same.
	id   string
	name string
	mux  *http.ServeMux
	// segments are the literal segments of the routes, by their lower case, for Jellyfin's routes
	// are matched whatever their case and ServeMux's are not.
	segments map[string]string
	// opening is held while a play session's HLS is started, so two fetches of its playlist start
	// it once.
	opening sync.Mutex
}

func New(logger *slog.Logger, info domain.Info, svc Services) *API {
	a := &API{
		logger: logger, svc: svc, name: info.Name,
		id:  strings.ReplaceAll(info.ID, "-", ""),
		mux: http.NewServeMux(), segments: map[string]string{},
	}
	a.handle("GET /System/Info/Public", a.publicInfo)
	a.handle("GET /System/Ping", ping)
	a.handle("POST /System/Ping", ping)
	a.handle("GET /Branding/Configuration", constant(`{"SplashscreenEnabled":false}`))
	a.handle("GET /Branding/Css", css)
	a.handle("GET /Branding/Css.css", css)
	a.handle("GET /QuickConnect/Enabled", constant(`false`))
	// No profile is listed to whoever asks: an app shows its form for a name and password.
	a.handle("GET /Users/Public", constant(`[]`))
	a.handle("POST /Users/AuthenticateByName", a.authenticateByName)
	a.handle("GET /System/Info", a.signedIn(a.systemInfo))
	a.handle("GET /Users/Me", a.signedIn(a.me))
	a.handle("GET /Users/{userId}", a.signedIn(a.user))
	a.handle("POST /Sessions/Logout", a.signedIn(a.logout))
	a.handle("GET /DisplayPreferences/{id}", a.signedIn(a.displayPreferences))
	// Browsing, under the routes Jellyfin 12.2 answers, and the /Users/{userId} forms apps still use.
	a.handle("GET /UserViews", a.signedIn(a.views))
	a.handle("GET /Users/{userId}/Views", a.signedIn(a.views))
	a.handle("GET /UserViews/GroupingOptions", a.signedIn(a.groupingOptions))
	a.handle("GET /Library/VirtualFolders", a.signedIn(a.virtualFolders))
	a.handle("GET /Items", a.signedIn(a.items))
	a.handle("GET /Users/{userId}/Items", a.signedIn(a.items))
	a.handle("GET /Items/{itemId}", a.signedIn(a.item))
	a.handle("GET /Users/{userId}/Items/{itemId}", a.signedIn(a.item))
	a.handle("GET /Shows/{seriesId}/Seasons", a.signedIn(a.seasons))
	a.handle("GET /Shows/{seriesId}/Episodes", a.signedIn(a.episodes))
	a.handle("GET /UserItems/Resume", a.signedIn(a.row(domain.RowContinueWatching)))
	a.handle("GET /Users/{userId}/Items/Resume", a.signedIn(a.row(domain.RowContinueWatching)))
	a.handle("GET /Shows/NextUp", a.signedIn(a.nextUp))
	a.handle("GET /Items/Latest", a.signedIn(a.latest))
	a.handle("GET /Users/{userId}/Items/Latest", a.signedIn(a.latest))
	a.handle("GET /Items/{itemId}/LocalTrailers", a.signedIn(none))
	a.handle("GET /Users/{userId}/Items/{itemId}/LocalTrailers", a.signedIn(none))
	a.handle("GET /Items/{itemId}/SpecialFeatures", a.signedIn(none))
	a.handle("GET /Users/{userId}/Items/{itemId}/SpecialFeatures", a.signedIn(none))
	a.handle("GET /Items/{itemId}/Images/{imageType}", a.image)
	a.handle("GET /Items/{itemId}/Images/{imageType}/{imageIndex}", a.image)
	// Playing: a title's copies, its file as it is, and where the app has got to.
	a.handle("GET /Items/{itemId}/PlaybackInfo", a.signedIn(a.playbackInfo))
	a.handle("POST /Items/{itemId}/PlaybackInfo", a.signedIn(a.playbackInfo))
	a.handle("GET /Videos/{itemId}/stream", a.signedIn(a.stream))
	a.handle("GET /Videos/{itemId}/{file}", a.signedIn(a.video))
	a.handle("DELETE /Videos/ActiveEncodings", a.signedIn(a.endEncoding))
	a.handle("GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/{file}", a.signedIn(a.subtitle))
	a.handle("GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/{start}/{file}", a.signedIn(a.subtitle))
	a.handle("GET /MediaSegments/{itemId}", a.signedIn(a.mediaSegments))
	a.handle("POST /Sessions/Playing", a.signedIn(a.reported(false)))
	a.handle("POST /Sessions/Playing/Progress", a.signedIn(a.reported(false)))
	a.handle("POST /Sessions/Playing/Stopped", a.signedIn(a.reported(true)))
	a.handle("POST /Sessions/Playing/Ping", a.signedIn(noContent))
	for _, prefix := range []string{"/UserPlayedItems/", "/Users/{userId}/PlayedItems/"} {
		a.handle("POST "+prefix+"{itemId}", a.signedIn(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
			return a.svc.Watching.MarkWatched(ctx, profile, item, nil)
		})))
		a.handle("DELETE "+prefix+"{itemId}", a.signedIn(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
			return a.svc.Watching.MarkUnwatched(ctx, profile, item)
		})))
	}
	for _, prefix := range []string{"/UserFavoriteItems/", "/Users/{userId}/FavoriteItems/"} {
		a.handle("POST "+prefix+"{itemId}", a.signedIn(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
			return a.svc.Watching.Favourite(ctx, profile, item)
		})))
		a.handle("DELETE "+prefix+"{itemId}", a.signedIn(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
			return a.svc.Watching.Unfavourite(ctx, profile, item)
		})))
	}
	return a
}

func (a *API) handle(pattern string, h http.HandlerFunc) {
	_, path, _ := strings.Cut(pattern, " ")
	for seg := range strings.SplitSeq(path, "/") {
		if seg != "" && !strings.HasPrefix(seg, "{") {
			a.segments[strings.ToLower(seg)] = seg
		}
	}
	a.mux.HandleFunc(pattern, h)
}

// ServeHTTP answers any origin, as Jellyfin does, so a Jellyfin web app served elsewhere can sign
// in: its token travels in a header, never a cookie, so another site gains nothing by it.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		w.Header().Set("Access-Control-Allow-Methods", r.Header.Get("Access-Control-Request-Method"))
		w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	r.URL.Path, r.URL.RawPath = a.canonical(r.URL.Path), ""
	a.mux.ServeHTTP(w, r)
}

// canonical writes each literal segment of a path as its route does, leaving the rest, an id or a
// name, as it came, and drops a trailing slash.
func (a *API) canonical(path string) string {
	segs := strings.Split(strings.TrimSuffix(path, "/"), "/")
	for i, seg := range segs {
		if c, ok := a.segments[strings.ToLower(seg)]; ok {
			segs[i] = c
		}
	}
	return strings.Join(segs, "/")
}

type sessionKey struct{}

// signedIn lets a request through with a token a profile holds. Only a missing or unknown token is
// answered 401, which some apps take to mean signed out.
func (a *API) signedIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := appOf(r).Token
		if strings.TrimSpace(token) == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s, err := a.svc.Auth.Authenticate(r.Context(), token)
		switch {
		case errors.Is(err, auth.ErrUnauthenticated):
			w.WriteHeader(http.StatusUnauthorized)
			return
		case err != nil:
			a.internal(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
	}
}

func sessionOf(r *http.Request) domain.Session {
	s, _ := r.Context().Value(sessionKey{}).(domain.Session)
	return s
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func constant(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

// ping answers as Jellyfin does: its product's name, not the server's.
var ping = constant(`"` + product + `"`)

func css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css")
}

// refuse answers as Jellyfin answers what it will not do: its status, and the same words whatever
// the reason, so an app learns nothing it should not.
func refuse(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("Error processing request."))
}

func (a *API) internal(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.ErrorContext(r.Context(), "jellyfin request failed", slog.String("path", r.URL.Path), slog.Any("err", err))
	refuse(w, http.StatusInternalServerError)
}
