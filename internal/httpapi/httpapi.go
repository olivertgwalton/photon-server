package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/playback"
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
	// refusals are problems with more to say than their code, by status.
	refusals map[int]any
	handle   http.HandlerFunc
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
	ChangePassword(ctx context.Context, session domain.Session, current, password string) error
	Devices(ctx context.Context, session domain.Session) ([]store.DeviceListing, error)
	SignOutDevice(ctx context.Context, session domain.Session, device uuid.UUID) error
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
	// Activity is the log of what has happened, and Events tells it, and more, as it happens.
	Activity activityLog
	Events   eventHub
	// Audience says which of the events each profile is told.
	Audience audience
	// Webhooks are the addresses told of events.
	Webhooks webhooks
	// NowPlaying is every playback going on, across the cluster.
	NowPlaying nowPlaying
	Pictures   pictures
	// Themes are titles' theme tunes, those from the theme host kept in Artwork.
	Themes    themes
	Watching  watching
	Playing   playing
	Playbacks playbacks
	// Downloads are each profile's, and Conversions make the ones not downloaded as they are.
	Downloads   downloads
	Conversions conversions
	Remuxing    remuxing
	HLS         hlsFiles
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
	// TrustedProxies are the peers whose X-Forwarded-For names the client. None by default.
	TrustedProxies []netip.Prefix
	// Setup is how this node was started, and Postgres and Valkey what it reaches.
	Setup    Setup
	Postgres versioned
	Valkey   cluster
}

type API struct {
	logger *slog.Logger
	info   domain.Info
	svc    Services
	mux    *http.ServeMux
	// description is the API's OpenAPI description, made once.
	description []byte
}

func New(logger *slog.Logger, info domain.Info, svc Services) *API {
	a := &API{logger: logger, info: info, svc: svc, mux: http.NewServeMux()}
	routes := a.routes()
	var err error
	if a.description, err = describe(info, routes); err != nil {
		panic("httpapi: " + err.Error())
	}
	for _, r := range routes {
		h := a.checkQuery(r)
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
		}
		switch r.reply.(type) {
		case asFile, asStream:
		default:
			h = compressJSON(h)
		}
		a.mux.Handle(r.pattern, h)
	}
	a.mux.HandleFunc("/", a.unmatched)
	return a
}

func (a *API) routes() []route {
	return []route{
		{
			pattern: "GET /api/v1/server", access: public, summary: "Say which server this is",
			status: http.StatusOK, reply: domain.Info{}, handle: a.server,
		},
		{
			pattern: "GET /api/v1/openapi.json", access: public, summary: "Describe the API in OpenAPI 3.1",
			status: http.StatusOK, reply: asFile{openAPIType}, handle: a.openAPI,
		},
		{
			pattern: "GET /readyz", access: public, summary: "Say whether Postgres and Valkey are reachable",
			status: http.StatusNoContent, handle: a.readyz,
		},
		{
			pattern: "POST /api/v1/auth/login", access: public, summary: "Sign a device in with a profile's password",
			body: loginRequest{}, status: http.StatusOK, reply: loginResponse{}, handle: a.login,
		},
		{
			pattern: "POST /api/v1/auth/logout", access: signedIn, summary: "Sign this device out",
			status: http.StatusNoContent, handle: a.logout,
		},
		{
			pattern: "GET /api/v1/me", access: signedIn, summary: "The profile this device is watching as",
			status: http.StatusOK, reply: profileJSON{}, handle: a.me,
		},
		{
			pattern: "POST /api/v1/auth/device/start", access: public,
			summary: "Start pairing a device by a code shown on it (RFC 8628)",
			body:    deviceJSON{}, status: http.StatusOK, reply: pairingStartJSON{}, handle: a.startPairing,
		},
		{
			pattern: "POST /api/v1/auth/device/approve", access: signedIn,
			summary: "Approve a pairing by its code, signing that device in as this profile",
			body:    approvalJSON{}, status: http.StatusOK, reply: deviceJSON{}, handle: a.approvePairing,
		},
		{
			pattern: "POST /api/v1/auth/device/poll", access: public,
			summary: "Ask whether a pairing is approved; until it is, the problem says why not",
			body:    pollJSON{}, status: http.StatusOK, reply: loginResponse{}, handle: a.pollPairing,
		},
		{
			pattern: "GET /api/v1/profiles", access: signedIn, summary: "List the household's profiles and their locks",
			status: http.StatusOK, reply: listJSON[profileListingJSON]{}, handle: a.profiles,
		},
		{
			pattern: "PUT /api/v1/session/profile", access: signedIn, summary: "Switch the profile this device watches as",
			body: switchJSON{}, status: http.StatusOK, reply: profileJSON{}, handle: a.switchProfile,
		},
		{
			pattern: "PATCH /api/v1/me", access: signedIn,
			summary: "Rename the profile; names are unique, and every device shows the new one at once",
			body:    nameJSON{}, status: http.StatusOK, reply: profileJSON{}, handle: a.renameSelf,
		},
		{
			pattern: "PUT /api/v1/me/pin", access: signedIn, summary: "Set the profile's PIN",
			body: pinJSON{}, status: http.StatusNoContent, handle: a.setPIN,
		},
		{
			pattern: "DELETE /api/v1/me/pin", access: signedIn, summary: "Clear the profile's PIN",
			status: http.StatusNoContent, handle: a.clearPIN,
		},
		{
			pattern: "PUT /api/v1/me/password", access: signedIn,
			summary: "Change the profile's password, signing out its other devices",
			body:    passwordChangeJSON{}, status: http.StatusNoContent, handle: a.changePassword,
		},
		{
			pattern: "GET /api/v1/me/preferences", access: signedIn,
			summary: "How the profile plays on every device: Jellyfin's defaults until it changes them",
			status:  http.StatusOK, reply: preferencesJSON{}, handle: a.ownPreferences,
		},
		{
			pattern: "PATCH /api/v1/me/preferences", access: signedIn,
			summary: "Change how the profile plays on every device; what is left out stays",
			body:    preferencesChangeJSON{}, status: http.StatusOK, reply: preferencesJSON{}, handle: a.setOwnPreferences,
		},
		{
			pattern: "POST /api/v1/me/avatar", access: signedIn,
			summary: "Give the profile a picture: a JPEG, PNG, GIF or WebP of at most 32 MiB and 50 megapixels",
			body:    avatarTypes, status: http.StatusOK, reply: profileJSON{}, handle: a.setOwnAvatar,
		},
		{
			pattern: "DELETE /api/v1/me/avatar", access: signedIn, summary: "Take the profile's picture away",
			status: http.StatusNoContent, handle: a.clearOwnAvatar,
		},
		{
			pattern: "GET /api/v1/auth/devices", access: signedIn, summary: "List the devices signed in",
			status: http.StatusOK, reply: listJSON[deviceListingJSON]{}, handle: a.devices,
		},
		{
			pattern: "DELETE /api/v1/auth/devices/{id}", access: signedIn, summary: "Sign a device out",
			status: http.StatusNoContent, handle: a.signOutDevice,
		},
		{
			pattern: "GET /api/v1/libraries", access: signedIn, summary: "List the libraries the profile sees, with how many of each kind of title it may see in each",
			status: http.StatusOK, reply: listJSON[libraryJSON]{}, handle: a.libraries,
		},
		{
			pattern: "GET /api/v1/libraries/{id}/titles", access: signedIn, summary: "Page a library's titles",
			query: slices.Concat([]param{
				{"sort", domain.SortTitle, "The order, title by default."},
				{"order", domain.Ascending, "Its direction; titles from A, anything else the newest first, by default."},
			}, pageParams, wallFilterParameters),
			status: http.StatusOK, reply: pageJSON[cardJSON]{}, handle: a.wall,
		},
		{
			pattern: "GET /api/v1/libraries/{id}/letters", access: signedIn,
			summary: "Count a library's titles under each letter, in title order",
			query:   wallFilterParameters, status: http.StatusOK, reply: listJSON[letterJSON]{}, handle: a.letters,
		},
		{
			pattern: "GET /api/v1/libraries/{id}/facets", access: signedIn,
			summary: "The values a library's titles can be narrowed to",
			status:  http.StatusOK, reply: facetsJSON{}, handle: a.facets,
		},
		{
			pattern: "GET /api/v1/libraries/{id}/collections", access: signedIn, summary: "Page a library's collections",
			query: pageParams, status: http.StatusOK, reply: pageJSON[cardJSON]{}, handle: a.libraryCollections,
		},
		{
			pattern: "GET /api/v1/titles/{id}/members", access: signedIn, summary: "A collection's titles",
			status: http.StatusOK, reply: listJSON[cardJSON]{}, handle: a.members,
		},
		{
			pattern: "GET /api/v1/titles/{id}/next", access: signedIn,
			summary: "The episode to play next: after an episode, or where the profile is in a show or season",
			status:  http.StatusOK, reply: cardJSON{}, handle: a.next,
		},
		{
			pattern: "GET /api/v1/titles/{id}/similar", access: signedIn, summary: "The titles most like one",
			status: http.StatusOK, reply: listJSON[cardJSON]{}, handle: a.similar,
		},
		{
			pattern: "GET /api/v1/people/{id}", access: signedIn, summary: "Someone's page and their titles here",
			status: http.StatusOK, reply: personJSON{}, handle: a.person,
		},
		{
			pattern: "GET /api/v1/history", access: signedIn, summary: "Page the profile's plays, the latest first",
			query: pageParams, status: http.StatusOK, reply: pageJSON[historyEntryJSON]{}, handle: a.ownHistory,
		},
		{
			pattern: "GET /api/v1/admin/history", access: admin, summary: "Page everyone's plays, or one profile's",
			query:  append([]param{{"profile", uuid.UUID{}, "Only this profile's plays."}}, pageParams...),
			status: http.StatusOK, reply: pageJSON[historyEntryJSON]{}, handle: a.adminHistory,
		},
		{
			pattern: "GET /api/v1/playlists", access: signedIn, summary: "List the profile's playlists, by name",
			status: http.StatusOK, reply: listJSON[playlistJSON]{}, handle: a.playlistsOf,
		},
		{
			pattern: "POST /api/v1/playlists", access: signedIn, summary: "Make a playlist",
			body: addPlaylistJSON{}, status: http.StatusCreated, reply: createdJSON{}, handle: a.addPlaylist,
		},
		{
			pattern: "PATCH /api/v1/playlists/{id}", access: signedIn, summary: "Rename a playlist",
			body: nameJSON{}, status: http.StatusNoContent, handle: a.setPlaylist,
		},
		{
			pattern: "DELETE /api/v1/playlists/{id}", access: signedIn, summary: "Remove a playlist",
			status: http.StatusNoContent, handle: a.removePlaylist,
		},
		{
			pattern: "GET /api/v1/playlists/{id}/entries", access: signedIn, summary: "Page a playlist, in its order",
			query: pageParams, status: http.StatusOK, reply: pageJSON[entryJSON]{}, handle: a.playlistEntries,
		},
		{
			pattern: "POST /api/v1/playlists/{id}/entries", access: signedIn,
			summary: "Put titles at the end of a playlist: a show or season as its episodes",
			body:    itemIDsJSON{}, status: http.StatusNoContent, handle: a.addToPlaylist,
		},
		{
			pattern: "PUT /api/v1/playlists/{id}/entries/{entry}/position", access: signedIn,
			summary: "Move a playlist's entry", body: moveJSON{}, status: http.StatusNoContent, handle: a.moveEntry,
		},
		{
			pattern: "DELETE /api/v1/playlists/{id}/entries/{entry}", access: signedIn,
			summary: "Take an entry out of a playlist", status: http.StatusNoContent, handle: a.removeEntry,
		},
		{
			pattern: "PATCH /api/v1/admin/titles/{id}", access: admin,
			summary: "Edit a title's fields, locking those named against its sources",
			body:    editJSON{}, status: http.StatusNoContent, handle: a.editTitle,
		},
		{
			pattern: "DELETE /api/v1/admin/titles/{id}/edits", access: admin,
			summary: "Give a title's edited fields back to its sources",
			query:   []param{{"field", []domain.Field{}, "The fields to give back; every one where none are named."}},
			status:  http.StatusAccepted, handle: a.resetEdits,
		},
		{
			pattern: "GET /api/v1/admin/titles/{id}/candidates", access: admin,
			summary: "List what a provider has by a title's name, to match it to",
			query: []param{
				{"provider", domain.FieldSource(""), "A provider that searches."},
				{"title", "", "The name to search for, the title's own by default."},
				{"year", 0, "The year to search in, the title's own by default."},
			},
			status: http.StatusOK, reply: listJSON[candidateJSON]{}, handle: a.candidates,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/match", access: admin,
			summary: "Match a film or show to a provider's title by its id",
			body:    pinMatchJSON{}, status: http.StatusAccepted, handle: a.pinMatch,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/episode-order", access: admin,
			summary: "Say the order a show's episode files are numbered in",
			body:    setEpisodeOrderJSON{}, status: http.StatusAccepted, handle: a.setEpisodeOrder,
		},
		{
			pattern: "POST /api/v1/admin/titles/{id}/refresh", access: admin,
			summary: "Ask a title's providers about it again now: a season or episode as its show",
			body:    refreshJSON{}, status: http.StatusAccepted, handle: a.refresh,
		},
		{
			pattern: "GET /api/v1/admin/titles/{id}/artwork/candidates", access: admin,
			summary: "List the pictures of a kind each provider has for a title, to choose from",
			query:   []param{artworkKindParam},
			status:  http.StatusOK, reply: listJSON[artworkCandidateJSON]{}, handle: a.artworkCandidates,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/artwork/{kind}", access: admin,
			summary: "Choose a title's picture of a kind from its candidates, over every source",
			path:    []param{artworkKindParam}, body: chooseArtworkJSON{}, status: http.StatusNoContent, handle: a.chooseArtwork,
		},
		{
			pattern: "DELETE /api/v1/admin/titles/{id}/artwork/{kind}", access: admin,
			summary: "Give a title's picture of a kind back to its sources",
			path:    []param{artworkKindParam}, status: http.StatusNoContent, handle: a.forgetArtwork,
		},
		{
			pattern: "PUT /api/v1/admin/versions/{id}/markers", access: admin,
			summary: "Say where a copy's intro, credits, recap and preview are, or that a part has none, over what was found",
			body:    markersJSON{}, status: http.StatusNoContent, handle: a.setMarkers,
		},
		{
			pattern: "POST /api/v1/admin/collections", access: admin, summary: "Make a collection in a library",
			body: addCollectionJSON{}, status: http.StatusCreated, reply: createdJSON{}, handle: a.addCollection,
		},
		{
			pattern: "PUT /api/v1/admin/collections/{id}/members", access: admin,
			summary: "Replace an admin's collection's titles, in order",
			body:    itemIDsJSON{}, status: http.StatusNoContent, handle: a.setMembers,
		},
		{
			pattern: "DELETE /api/v1/admin/collections/{id}", access: admin,
			summary: "Remove an admin's collection, leaving its titles", status: http.StatusNoContent,
			handle: a.removeCollection,
		},
		{
			pattern: "GET /api/v1/titles/{id}", access: signedIn, summary: "A title's page",
			status: http.StatusOK, reply: store.TitlePage{}, handle: a.title,
		},
		{
			pattern: "PUT /api/v1/titles/{id}/progress", access: signedIn,
			summary: "Record where the profile stopped a film or episode",
			body:    positionJSON{}, status: http.StatusOK, reply: reachedJSON{}, handle: a.progress,
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/progress", access: signedIn,
			summary: "Remove a title, or a show's or season's episodes, from Continue Watching, keeping what was watched",
			status:  http.StatusNoContent, handle: a.mark(watching.ClearProgress),
		},
		{
			pattern: "PUT /api/v1/titles/{id}/watched", access: signedIn, summary: "Mark a title watched",
			status: http.StatusNoContent, handle: a.mark(watching.MarkWatched),
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/watched", access: signedIn, summary: "Mark a title unwatched",
			status: http.StatusNoContent, handle: a.mark(watching.MarkUnwatched),
		},
		{
			pattern: "PUT /api/v1/titles/{id}/favourite", access: signedIn, summary: "Make a title a favourite",
			status: http.StatusNoContent, handle: a.mark(watching.Favourite),
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/favourite", access: signedIn, summary: "Take a title from the favourites",
			status: http.StatusNoContent, handle: a.mark(watching.Unfavourite),
		},
		{
			pattern: "POST /api/v1/titles/{id}/play", access: signedIn,
			summary: "Open a playback of a film or episode, as the client's profile can play it",
			body:    playJSON{}, status: http.StatusOK, reply: playbackJSON{},
			refusals: map[int]any{codeNoCompatibleStream.status(): refusalJSON{}}, handle: a.play,
		},
		{
			pattern: "POST /api/v1/playback/{id}/progress", access: signedIn, summary: "Say where a playback has got to, paused too: one unheard from for two minutes is stopped",
			body: playbackProgressJSON{}, status: http.StatusOK, reply: reachedJSON{}, handle: a.playbackProgress,
		},
		{
			pattern: "POST /api/v1/playback/{id}/stop", access: signedIn, summary: "Stop a playback, and say where",
			body: positionJSON{}, status: http.StatusOK, reply: reachedJSON{},
			handle: a.routeToOwner("id", http.HandlerFunc(a.playbackStop)).ServeHTTP,
		},
		{
			pattern: "GET /api/v1/hls/{playback}/{exp}/{sig}/{file}", access: signedPath,
			summary: "A remux's playlist, initialisation, segment or subtitle segment, at the address play answered",
			path: []param{
				{"exp", "", "When the address lapses, as the server signed it."},
				{"sig", "", "The server's signature of the playback and exp."},
				{"file", "", "main.m3u8, and what it names."},
			},
			status: http.StatusOK, reply: asFile{"application/vnd.apple.mpegurl", "text/vtt", "video/mp4", "video/iso.segment"},
			handle: a.hlsFile,
		},
		{
			pattern: "POST /api/v1/downloads", access: signedIn,
			summary: "Download a film or episode no larger than a bitrate: its file as it is, else converted to the video the device plays",
			body:    downloadRequestJSON{}, status: http.StatusOK, reply: downloadJSON{}, handle: a.addDownload,
		},
		{
			pattern: "GET /api/v1/downloads", access: signedIn,
			summary: "List this device's downloads, or the profile's on every device, the newest first",
			query:   []param{{"scope", scopeDevice, "device, the default, is this device's; profile is the profile's on every device."}},
			status:  http.StatusOK, reply: listJSON[downloadJSON]{}, handle: a.ownDownloads,
		},
		{
			pattern: "GET /api/v1/downloads/{id}", access: signedIn, summary: "A download, as far as its conversion has got",
			status: http.StatusOK, reply: downloadJSON{}, handle: a.download,
		},
		{
			pattern: "DELETE /api/v1/downloads/{id}", access: signedIn,
			summary: "Remove a download, and its conversion where no other download needs it",
			status:  http.StatusNoContent, handle: a.removeDownload,
		},
		{
			pattern: "GET /api/v1/downloads/{id}/file", access: signedAddress,
			summary: "A download's conversion, in byte ranges, at the address the download answered",
			query:   signatureParams, status: http.StatusOK, reply: asFile{"video/mp4"}, handle: a.downloadFile,
		},
		{
			pattern: "GET /api/v1/parts/{id}/stream", access: signedAddress,
			summary: "A copy's file as it is, in byte ranges, at the address play answered",
			query:   signatureParams, status: http.StatusOK, reply: asFile{"video/*"}, handle: a.partStream,
		},
		{
			pattern: "GET /api/v1/parts/{id}/sample", access: signedIn,
			summary: "The first " + strconv.Itoa(sampleBytes>>20) + " MiB of a part's file, in byte ranges, to time the connection; no playback",
			status:  http.StatusOK, reply: asFile{"video/*"}, handle: a.partSample,
		},
		{
			pattern: "GET /api/v1/parts/{id}/trickplay", access: signedIn,
			summary: "How a part's trickplay sheets are laid out, to find the thumbnail for a time",
			status:  http.StatusOK, reply: store.Trickplay{}, handle: a.trickplay,
		},
		{
			pattern: "GET /api/v1/parts/{id}/trickplay/{n}", access: signedIn, summary: "A part's trickplay sheet",
			path:   []param{{"n", 0, "The sheet, counted from 0."}},
			status: http.StatusOK, reply: asFile{"image/jpeg"}, handle: a.trickplaySheet,
		},
		{
			pattern: "GET /api/v1/parts/{id}/chapters/{idx}/image", access: signedIn,
			summary: "A picture of a chapter, at the address the title's page gives",
			path:    []param{{"idx", 0, "The chapter, counted from 0 in its part."}},
			status:  http.StatusOK, reply: asFile{"image/jpeg"}, handle: a.chapterImage,
		},
		{
			pattern: "GET /api/v1/parts/{id}/chapter-images/{idx}", access: signedAddress,
			summary: "A picture of a chapter, at the signed address the title's page gives, for a player with no token",
			path:    []param{{"idx", 0, "The chapter, counted from 0 in its part."}},
			query:   signatureParams, status: http.StatusOK, reply: asFile{"image/jpeg"}, handle: a.signedChapterImage,
		},
		{
			pattern: "GET /api/v1/admin/libraries", access: admin, summary: "List the libraries as an admin keeps them, with everything each holds",
			status: http.StatusOK, reply: listJSON[adminLibraryListingJSON]{}, handle: a.adminLibraries,
		},
		{
			pattern: "POST /api/v1/admin/libraries", access: admin, summary: "Add a library of a folder and scan it",
			body: addLibraryJSON{}, status: http.StatusCreated, reply: adminLibraryJSON{}, handle: a.addLibrary,
		},
		{
			pattern: "GET /api/v1/admin/folders", access: admin,
			summary: "List a folder's subfolders on the server, or the folders to start from, for choosing a library's",
			query: []param{
				{"path", "", "An absolute path; without one, the folders to start from."},
				{"hidden", hideHidden, "show lists folders whose names start with a dot, hidden by default."},
			},
			status: http.StatusOK, reply: folderListJSON{}, handle: a.adminFolders,
		},
		{
			pattern: "PATCH /api/v1/admin/libraries/{id}", access: admin, summary: "Change how a library is kept",
			body: libraryChangeJSON{}, status: http.StatusOK, reply: adminLibraryJSON{}, handle: a.setLibrary,
		},
		{
			pattern: "DELETE /api/v1/admin/libraries/{id}", access: admin, summary: "Remove a library",
			status: http.StatusNoContent, handle: a.removeLibrary,
		},
		{
			pattern: "POST /api/v1/admin/libraries/{id}/scan", access: admin,
			summary: "Scan a library now, or only the folder of it a path is in",
			query:   []param{{"path", "", "An absolute path inside the library: the folder it is, or the nearest folder above it that is there, is scanned with everything under it."}},
			status:  http.StatusAccepted, handle: a.scanLibrary,
		},
		{
			pattern: "POST /api/v1/admin/libraries/{id}/refresh", access: admin,
			summary: "Ask the providers about a library's films and shows again: those not yet described, or all",
			body:    refreshJSON{}, status: http.StatusAccepted, handle: a.refreshLibrary,
		},
		{
			pattern: "GET /api/v1/subtitles/{id}/file", access: signedAddress,
			summary: "A subtitle file beside a copy, as it is or as WebVTT, at the address play answered",
			query: append([]param{
				{"format", subtitleOriginal, "webvtt converts a text subtitle to WebVTT; original, the default, is the file as it is."},
			}, signatureParams...),
			status: http.StatusOK, reply: asFile{"application/x-subrip", "text/vtt", "text/x-ssa"}, handle: a.subtitleFile,
		},
		{
			pattern: "POST /api/v1/admin/profiles", access: admin, summary: "Add a profile",
			body: addProfileJSON{}, status: http.StatusCreated, reply: profileJSON{}, handle: a.addProfile,
		},
		{
			pattern: "PATCH /api/v1/admin/profiles/{id}", access: admin, summary: "Change a profile",
			body: profileChangeJSON{}, status: http.StatusOK, reply: profileJSON{}, handle: a.setProfile,
		},
		{
			pattern: "DELETE /api/v1/admin/profiles/{id}", access: admin,
			summary: "Remove a profile, its devices and what it has watched", status: http.StatusNoContent,
			handle: a.removeProfile,
		},
		{
			pattern: "POST /api/v1/admin/profiles/{id}/avatar", access: admin,
			summary: "Give any profile a picture: a JPEG, PNG, GIF or WebP of at most 32 MiB and 50 megapixels",
			body:    avatarTypes, status: http.StatusOK, reply: profileJSON{}, handle: a.setProfileAvatar,
		},
		{
			pattern: "DELETE /api/v1/admin/profiles/{id}/avatar", access: admin, summary: "Take any profile's picture away",
			status: http.StatusNoContent, handle: a.clearProfileAvatar,
		},
		{
			pattern: "GET /api/v1/admin/profiles/{id}/access", access: admin, summary: "What a profile may see",
			status: http.StatusOK, reply: accessJSON{}, handle: a.profileAccess,
		},
		{
			pattern: "PUT /api/v1/admin/profiles/{id}/access", access: admin, summary: "Replace what a profile may see",
			body: accessJSON{}, status: http.StatusNoContent, handle: a.setProfileAccess,
		},
		{
			pattern: "GET /api/v1/admin/providers", access: admin,
			summary: "List the metadata providers, what each can do and needs set",
			status:  http.StatusOK, reply: listJSON[metadataProviderJSON]{}, handle: a.adminProviders,
		},
		{
			pattern: "PATCH /api/v1/admin/providers/{id}", access: admin, summary: "Change a provider's settings",
			path: []param{{"id", domain.FieldSource(""), "The provider."}},
			body: providerChangeJSON{}, status: http.StatusOK, reply: metadataProviderJSON{}, handle: a.setProvider,
		},
		{
			pattern: "GET /api/v1/admin/plugins", access: admin, summary: "List the registered metadata plugins",
			status: http.StatusOK, reply: listJSON[pluginJSON]{}, handle: a.adminPlugins,
		},
		{
			pattern: "POST /api/v1/admin/plugins", access: admin,
			summary: "Register the metadata plugin at an address, once its manifest is read",
			body:    addPluginJSON{}, status: http.StatusCreated, reply: pluginJSON{}, handle: a.addPlugin,
		},
		{
			pattern: "DELETE /api/v1/admin/plugins/{slug}", access: admin,
			summary: "Forget a plugin; what it said about titles stays",
			path:    []param{slugParam}, status: http.StatusNoContent, handle: a.removePlugin,
		},
		{
			pattern: "POST /api/v1/admin/plugins/{slug}/refresh", access: admin, summary: "Read a plugin's manifest again",
			path: []param{slugParam}, status: http.StatusOK, reply: pluginJSON{}, handle: a.refreshPlugin,
		},
		{
			pattern: "GET /api/v1/admin/tasks", access: admin, summary: "List the scheduled tasks and how each last ran",
			status: http.StatusOK, reply: listJSON[taskJSON]{}, handle: a.adminTasks,
		},
		{
			pattern: "POST /api/v1/admin/tasks/{key}/run", access: admin, summary: "Run a task now",
			path:   []param{{"key", domain.TaskKey(""), "The task."}},
			status: http.StatusAccepted, handle: a.runTask,
		},
		{
			pattern: "GET /api/v1/admin/jobs", access: admin, summary: "Count the job queue and list the dead jobs",
			status: http.StatusOK, reply: jobQueueJSON{}, handle: a.adminJobs,
		},
		{
			pattern: "POST /api/v1/admin/jobs/{id}/retry", access: admin, summary: "Retry a dead job",
			path:   []param{{"id", int64(0), "The job."}},
			status: http.StatusAccepted, handle: a.retryJob,
		},
		{
			pattern: "GET /api/v1/admin/playbacks", access: admin,
			summary: "List who is playing what, and how many videos this node is transcoding",
			status:  http.StatusOK, reply: nowPlayingListJSON{}, handle: a.adminPlaybacks,
		},
		{
			pattern: "DELETE /api/v1/admin/playbacks/{id}", access: admin,
			summary: "Stop anyone's playback, keeping where it had got to", status: http.StatusNoContent,
			handle: a.routeToOwner("id", http.HandlerFunc(a.stopPlayback)).ServeHTTP,
		},
		{
			pattern: "GET /api/v1/admin/server", access: admin,
			summary: "Say how this node was set up, what it reaches, and the cluster's nodes",
			status:  http.StatusOK, reply: serverJSON{}, handle: a.adminServer,
		},
		{
			pattern: "GET /api/v1/admin/activity", access: admin, summary: "Page the activity log, the newest first",
			query:  append([]param{{"kind", domain.EventKind(""), "Only entries of this kind, one the log keeps."}}, pageParams...),
			status: http.StatusOK, reply: pageJSON[eventJSON]{}, handle: a.adminActivity,
		},
		{
			pattern: "GET /api/v1/admin/events", access: admin,
			summary: "Stream a snapshot of what is going on, then each event as it happens, as Server-Sent Events",
			status:  http.StatusOK, reply: eventStream(), handle: a.adminEvents,
		},
		{
			pattern: "GET /api/v1/events", access: signedIn,
			summary: "Stream what changes of the libraries, titles and state the profile sees, as Server-Sent Events",
			status:  http.StatusOK, reply: feedStream(), handle: a.events,
		},
		{
			pattern: "GET /api/v1/admin/webhooks", access: admin, summary: "List the webhooks, without their secrets",
			status: http.StatusOK, reply: listJSON[webhookJSON]{}, handle: a.adminWebhooks,
		},
		{
			pattern: "POST /api/v1/admin/webhooks", access: admin,
			summary: "Add a webhook, answering the secret its bodies are signed with, this once",
			body:    addWebhookJSON{}, status: http.StatusCreated, reply: addedWebhookJSON{}, handle: a.addWebhook,
		},
		{
			pattern: "DELETE /api/v1/admin/webhooks/{id}", access: admin, summary: "Remove a webhook and what waits to be sent to it",
			status: http.StatusNoContent, handle: a.removeWebhook,
		},
		{
			pattern: "POST /api/v1/admin/webhooks/{id}/test", access: admin, summary: "Send a webhook.test event to one webhook",
			status: http.StatusAccepted, handle: a.testWebhook,
		},
		{
			pattern: "GET /api/v1/home", access: signedIn, summary: "The profile's home rows, in order",
			query:  []param{{"limit", 0, "How many titles a row holds, from 1 to " + strconv.Itoa(maxWallLimit) + "."}},
			status: http.StatusOK, reply: homeJSON{}, handle: a.home,
		},
		{
			pattern: "GET /api/v1/upcoming", access: signedIn,
			summary: "Page the shows the profile sees with an episode due to air today or later, the soonest first, and that episode",
			query:   append([]param{{"library", uuid.UUID{}, "Only this library's shows."}}, pageParams...),
			status:  http.StatusOK, reply: pageJSON[airingJSON]{}, handle: a.upcoming,
		},
		{
			pattern: "GET /api/v1/search", access: signedIn, summary: "Page the titles, episodes among them, and the people a search finds",
			query: append([]param{
				{"q", "", "What to search for; required."},
				{"library", uuid.UUID{}, "Only this library's titles."},
			}, pageParams...),
			status: http.StatusOK, reply: searchJSON{}, handle: a.search,
		},
		{
			pattern: "GET /api/v1/artwork/{id}", access: public,
			summary: "A picture, kept for good: its id changes when it does",
			query:   []param{{"width", 0, "A copy this many pixels wide."}},
			status:  http.StatusOK, reply: asFile{"image/*"}, handle: a.artwork,
		},
		{
			pattern: "GET /api/v1/themes/{id}", access: public,
			summary: "A theme tune, in byte ranges, kept for good: its id changes when it does",
			status:  http.StatusOK, reply: asFile{"audio/*"}, handle: a.theme,
		},
	}
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
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
