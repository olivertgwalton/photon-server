// Package jellyfin answers Jellyfin 12.2's API, on a port of its own, so the apps made for Jellyfin
// (Infuse, Swiftfin, Findroid, Jellyfin's own) can use the server. It speaks Jellyfin's words to
// them and photon's services underneath; none of its words reach photon's own API.
package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
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
	auth.Authenticator
	PairingStatus(ctx context.Context, deviceCode string) (kv.PairingState, auth.Pairing, error)
}

type Services struct {
	Auth      authenticator
	Limits    kv.Limiter
	Raise     func(ctx context.Context, e domain.Event)
	Proxies   peer.Proxies
	Catalogue catalogue
	Playlists playlists
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
	// Sent counts the media this node sends.
	Sent *playback.Sent
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
	// Ping answers as Jellyfin does: its product's name, not the server's.
	a.anyone(a.constant(`"`+product+`"`), "GET /System/Ping", "POST /System/Ping")
	a.anyone(a.publicInfo, "GET /System/Info/Public")
	a.anyone(a.constant(`{"SplashscreenEnabled":false}`), "GET /Branding/Configuration")
	a.anyone(css, "GET /Branding/Css", "GET /Branding/Css.css")
	a.anyone(a.constant(`true`), "GET /QuickConnect/Enabled")
	a.anyone(a.initiateQuickConnect, "POST /QuickConnect/Initiate")
	a.anyone(a.quickConnectState, "GET /QuickConnect/Connect")
	a.anyone(a.authenticateWithQuickConnect, "POST /Users/AuthenticateWithQuickConnect")
	// No profile is listed to whoever asks: an app shows its form for a name and password.
	a.anyone(a.constant(`[]`), "GET /Users/Public")
	a.anyone(a.authenticateByName, "POST /Users/AuthenticateByName")
	a.anyone(a.image, "GET /Items/{itemId}/Images/{imageType}", "GET /Items/{itemId}/Images/{imageType}/{imageIndex}")
	a.handle(a.authorizeQuickConnect, "POST /QuickConnect/Authorize")
	a.handle(a.systemInfo, "GET /System/Info")
	a.handle(a.me, "GET /Users/Me")
	a.handle(a.user, "GET /Users/{userId}")
	a.handle(a.logout, "POST /Sessions/Logout")
	// What an app says it can do, sent as it signs in: the media it plays and the commands it
	// takes from another app. Photon sends no app commands, and chooses how a copy plays from the
	// device profile PlaybackInfo is sent, so none of it is kept.
	a.handle(noContent, "POST /Sessions/Capabilities", "POST /Sessions/Capabilities/Full")
	a.handle(a.displayPreferences, "GET /DisplayPreferences/{id}")
	// Browsing, under the routes Jellyfin 12.2 answers, and the /Users/{userId} forms apps still use.
	a.handle(a.views, "GET /UserViews", "GET /Users/{userId}/Views")
	a.handle(a.groupingOptions, "GET /UserViews/GroupingOptions")
	a.handle(a.virtualFolders, "GET /Library/VirtualFolders")
	a.handle(a.items, "GET /Items", "GET /Users/{userId}/Items")
	a.handle(a.item, "GET /Items/{itemId}", "GET /Users/{userId}/Items/{itemId}")
	a.handle(a.seasons, "GET /Shows/{seriesId}/Seasons")
	a.handle(a.episodes, "GET /Shows/{seriesId}/Episodes")
	a.handle(a.row(domain.RowContinueWatching), "GET /UserItems/Resume", "GET /Users/{userId}/Items/Resume")
	a.handle(a.nextUp, "GET /Shows/NextUp")
	a.handle(a.upcoming, "GET /Shows/Upcoming")
	a.handle(a.latest, "GET /Items/Latest", "GET /Users/{userId}/Items/Latest")
	// Someone is found by name and opened by id: /Persons/{name} is not answered, as two people
	// here may share a name, and apps open a cast member by the id an item gives.
	a.handle(a.persons, "GET /Persons")
	a.handle(a.genres, "GET /Genres")
	a.handle(a.studios, "GET /Studios")
	a.handle(a.filters, "GET /Items/Filters")
	a.handle(a.filters2, "GET /Items/Filters2")
	a.handle(a.searchHints, "GET /Search/Hints")
	a.handle(a.similar, "GET /Items/{itemId}/Similar", "GET /Movies/{itemId}/Similar", "GET /Shows/{itemId}/Similar")
	a.handle(a.none,
		"GET /Items/{itemId}/LocalTrailers", "GET /Users/{userId}/Items/{itemId}/LocalTrailers",
		"GET /Items/{itemId}/SpecialFeatures", "GET /Users/{userId}/Items/{itemId}/SpecialFeatures")
	a.handle(a.createPlaylist, "POST /Playlists")
	a.handle(a.playlist, "GET /Playlists/{playlistId}")
	a.handle(a.updatePlaylist, "POST /Playlists/{playlistId}")
	a.handle(a.playlistItems, "GET /Playlists/{playlistId}/Items")
	a.handle(a.addToPlaylist, "POST /Playlists/{playlistId}/Items")
	a.handle(a.removeFromPlaylist, "DELETE /Playlists/{playlistId}/Items")
	a.handle(a.moveInPlaylist, "POST /Playlists/{playlistId}/Items/{itemId}/Move/{newIndex}")
	// Playing: a title's copies, its file as it is, and where the app has got to.
	a.handle(a.playbackInfo, "GET /Items/{itemId}/PlaybackInfo", "POST /Items/{itemId}/PlaybackInfo")
	a.handle(a.sending(playback.DeliveryFile, a.stream), "GET /Videos/{itemId}/stream")
	a.handle(a.video, "GET /Videos/{itemId}/{file}")
	a.handle(a.sending(playback.DeliveryFile, a.download), "GET /Items/{itemId}/Download")
	a.handle(a.endEncoding, "DELETE /Videos/ActiveEncodings")
	a.handle(a.subtitle,
		"GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/{file}",
		"GET /Videos/{itemId}/{sourceId}/Subtitles/{index}/{start}/{file}")
	a.handle(a.mediaSegments, "GET /MediaSegments/{itemId}")
	a.handle(a.reported(reportProgress), "POST /Sessions/Playing", "POST /Sessions/Playing/Progress")
	a.handle(a.reported(reportStopped), "POST /Sessions/Playing/Stopped")
	a.handle(noContent, "POST /Sessions/Playing/Ping")
	a.handle(a.userDataOf, "GET /UserItems/{itemId}/UserData", "GET /Users/{userId}/Items/{itemId}/UserData")
	a.handle(a.changeUserData, "POST /UserItems/{itemId}/UserData", "POST /Users/{userId}/Items/{itemId}/UserData")
	a.handle(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
		return a.svc.Watching.MarkWatched(ctx, profile, item, nil)
	}), "POST /UserPlayedItems/{itemId}", "POST /Users/{userId}/PlayedItems/{itemId}")
	a.handle(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
		return a.svc.Watching.MarkUnwatched(ctx, profile, item)
	}), "DELETE /UserPlayedItems/{itemId}", "DELETE /Users/{userId}/PlayedItems/{itemId}")
	a.handle(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
		return a.svc.Watching.Favourite(ctx, profile, item)
	}), "POST /UserFavoriteItems/{itemId}", "POST /Users/{userId}/FavoriteItems/{itemId}")
	a.handle(a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
		return a.svc.Watching.Unfavourite(ctx, profile, item)
	}), "DELETE /UserFavoriteItems/{itemId}", "DELETE /Users/{userId}/FavoriteItems/{itemId}")
	return a
}

// sending counts what h sends as media delivered as d.
func (a *API) sending(d playback.Delivery, h http.HandlerFunc) http.HandlerFunc {
	return a.svc.Sent.Counting(d, h).ServeHTTP
}

// handle answers the patterns with h for a signed-in profile alone; anyone answers them for
// anyone.
func (a *API) handle(h http.HandlerFunc, patterns ...string) {
	a.anyone(a.signedIn(h), patterns...)
}

func (a *API) anyone(h http.HandlerFunc, patterns ...string) {
	for _, pattern := range patterns {
		_, path, _ := strings.Cut(pattern, " ")
		for seg := range strings.SplitSeq(path, "/") {
			if seg != "" && !strings.HasPrefix(seg, "{") {
				a.segments[strings.ToLower(seg)] = seg
			}
		}
		a.mux.HandleFunc(pattern, h)
	}
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
		next(w, r.WithContext(auth.WithSession(r.Context(), s)))
	}
}

func (a *API) writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		a.logger.Error("reply not encoded", slog.Any("err", err))
		a.refuse(w, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	a.write(w, append(body, '\n'))
}

// write writes a reply's body. A write fails only once the app has gone, or stopped reading for
// longer than the server waits, and then there is no one left to tell: the failure is kept for
// debugging alone.
func (a *API) write(w io.Writer, body []byte) {
	if _, err := w.Write(body); err != nil {
		a.logger.Debug("reply not written", slog.Any("err", err))
	}
}

// readWithin gives the app bodyWithin to send its body. Not every ResponseWriter has a connection
// to time: a test's recorder has none. Any other failure is of a connection already gone, whose
// reads fail anyway.
func (a *API) readWithin(w http.ResponseWriter) {
	if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyWithin)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		a.logger.Debug("read deadline not set", slog.Any("err", err))
	}
}

func (a *API) constant(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		a.write(w, []byte(body))
	}
}

func css(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css")
}

// refuse answers as Jellyfin answers what it will not do: its status, and the same words whatever
// the reason, so an app learns nothing it should not.
func (a *API) refuse(w http.ResponseWriter, status int) {
	// A refusal is never saved as the file a download asked for.
	w.Header().Del("Content-Disposition")
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(status)
	a.write(w, []byte("Error processing request."))
}

func (a *API) internal(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.ErrorContext(r.Context(), "jellyfin request failed", slog.String("path", r.URL.Path), slog.Any("err", err))
	a.refuse(w, http.StatusInternalServerError)
}
