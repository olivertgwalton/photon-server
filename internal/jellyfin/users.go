package jellyfin

import (
	"context"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// maxSignIn is the most a sign-in's body may be: a name and a password.
	maxSignIn = 8 << 10
	// bodyWithin is how long a client has to send a body, as photon's own API gives it.
	bodyWithin = 10 * time.Second
)

// guid writes an id as Jellyfin writes a Guid: 32 lowercase hex digits.
func guid(id uuid.UUID) string {
	return hex.EncodeToString(id[:])
}

// user is Jellyfin's UserDto. The Has*Password fields are obsolete in Jellyfin 12, which always
// sends them so, but the Kotlin SDK the Android apps ship refuses a user without them.
type user struct {
	Name                      string         `json:"Name"`
	ServerID                  string         `json:"ServerId"`
	ID                        string         `json:"Id"`
	PrimaryImageTag           string         `json:"PrimaryImageTag,omitempty"`
	HasPassword               bool           `json:"HasPassword"`
	HasConfiguredPassword     bool           `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool           `json:"HasConfiguredEasyPassword"`
	EnableAutoLogin           bool           `json:"EnableAutoLogin"`
	Configuration             configuration  `json:"Configuration"`
	Policy                    jsontext.Value `json:"Policy"`
}

// policy is Jellyfin's UserPolicy, complete for the same reason. No profile is an administrator
// here, even photon's admins: an app would offer Jellyfin's dashboard, which is not served, so
// the server is run from photon's own app. What photon has no part of, Live TV, SyncPlay, sharing
// and deleting through this API, is turned off so apps do not offer it. Downloading is on: a
// profile downloads a title's file as it is, of what it may play.
var policy = jsontext.Value(`{"IsAdministrator":false,"IsHidden":true,"EnableCollectionManagement":false,` +
	`"EnableSubtitleManagement":false,"EnableLyricManagement":false,"IsDisabled":false,"BlockedTags":[],` +
	`"AllowedTags":[],"EnableUserPreferenceAccess":true,"AccessSchedules":[],"BlockUnratedItems":[],` +
	`"EnableRemoteControlOfOtherUsers":false,"EnableSharedDeviceControl":false,"EnableRemoteAccess":true,` +
	`"EnableLiveTvManagement":false,"EnableLiveTvAccess":false,"EnableMediaPlayback":true,` +
	`"EnableAudioPlaybackTranscoding":true,"EnableVideoPlaybackTranscoding":true,"EnablePlaybackRemuxing":true,` +
	`"ForceRemoteSourceTranscoding":false,"EnableContentDeletion":false,"EnableContentDeletionFromFolders":[],` +
	`"EnableContentDownloading":true,"EnableSyncTranscoding":false,"EnableMediaConversion":false,` +
	`"EnabledDevices":[],"EnableAllDevices":true,"EnabledChannels":[],"EnableAllChannels":true,` +
	`"EnabledFolders":[],"EnableAllFolders":true,"InvalidLoginAttemptCount":0,"LoginAttemptsBeforeLockout":-1,` +
	`"MaxActiveSessions":0,"EnablePublicSharing":false,"BlockedMediaFolders":[],"BlockedChannels":[],` +
	`"RemoteClientBitrateLimit":0,` +
	`"AuthenticationProviderId":"Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",` +
	`"PasswordResetProviderId":"Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",` +
	`"SyncPlayAccess":"None"}`)

func (a *API) userOf(ctx context.Context, p domain.Profile) (user, error) {
	prefs, libs, err := a.configured(ctx, p.ID)
	return user{
		Name: p.Name, ServerID: a.id, ID: guid(p.ID), PrimaryImageTag: tag(p.Avatar),
		HasPassword: true, HasConfiguredPassword: true,
		Configuration: configurationOf(prefs, libs), Policy: policy,
	}, err
}

// authenticationResult is Jellyfin's AuthenticationResult.
type authenticationResult struct {
	User        user        `json:"User"`
	SessionInfo sessionInfo `json:"SessionInfo"`
	AccessToken string      `json:"AccessToken"`
	ServerID    string      `json:"ServerId"`
}

// sessionInfo is Jellyfin's SessionInfoDto of a session just signed in, with every field Jellyfin
// fills: Discrete refuses a sign-in whose answer has none.
type sessionInfo struct {
	PlayState struct {
		CanSeek       bool   `json:"CanSeek"`
		IsPaused      bool   `json:"IsPaused"`
		IsMuted       bool   `json:"IsMuted"`
		RepeatMode    string `json:"RepeatMode"`
		PlaybackOrder string `json:"PlaybackOrder"`
	} `json:"PlayState"`
	AdditionalUsers []struct{} `json:"AdditionalUsers"`
	Capabilities    struct {
		PlayableMediaTypes           []string `json:"PlayableMediaTypes"`
		SupportedCommands            []string `json:"SupportedCommands"`
		SupportsMediaControl         bool     `json:"SupportsMediaControl"`
		SupportsPersistentIdentifier bool     `json:"SupportsPersistentIdentifier"`
	} `json:"Capabilities"`
	RemoteEndPoint           string     `json:"RemoteEndPoint"`
	PlayableMediaTypes       []string   `json:"PlayableMediaTypes"`
	ID                       string     `json:"Id"`
	UserID                   string     `json:"UserId"`
	UserName                 string     `json:"UserName"`
	Client                   string     `json:"Client"`
	LastActivityDate         time.Time  `json:"LastActivityDate"`
	LastPlaybackCheckIn      time.Time  `json:"LastPlaybackCheckIn"`
	DeviceName               string     `json:"DeviceName"`
	DeviceID                 string     `json:"DeviceId"`
	ApplicationVersion       string     `json:"ApplicationVersion"`
	IsActive                 bool       `json:"IsActive"`
	SupportsMediaControl     bool       `json:"SupportsMediaControl"`
	SupportsRemoteControl    bool       `json:"SupportsRemoteControl"`
	NowPlayingQueue          []struct{} `json:"NowPlayingQueue"`
	NowPlayingQueueFullItems []struct{} `json:"NowPlayingQueueFullItems"`
	HasCustomDeviceName      bool       `json:"HasCustomDeviceName"`
	ServerID                 string     `json:"ServerId"`
	SupportedCommands        []string   `json:"SupportedCommands"`
}

// sessionOf is the session token signs an app in to, as the app says of itself.
func (a *API) sessionOf(r *http.Request, p domain.Profile, token string) (sessionInfo, error) {
	s, err := a.svc.Auth.Authenticate(r.Context(), token)
	if err != nil {
		return sessionInfo{}, err
	}
	app := appOf(r)
	out := sessionInfo{
		AdditionalUsers: []struct{}{}, PlayableMediaTypes: []string{}, ID: guid(s.ID), UserID: guid(p.ID), UserName: p.Name,
		Client: app.Client, LastActivityDate: time.Now().UTC(), DeviceName: app.Device, DeviceID: app.DeviceID,
		ApplicationVersion: app.Version, IsActive: true, NowPlayingQueue: []struct{}{}, NowPlayingQueueFullItems: []struct{}{},
		ServerID: a.id, SupportedCommands: []string{}, RemoteEndPoint: a.svc.Reach.Client(r).String(),
	}
	out.PlayState.RepeatMode, out.PlayState.PlaybackOrder = "RepeatNone", "Default"
	out.Capabilities.PlayableMediaTypes, out.Capabilities.SupportedCommands = []string{}, []string{}
	out.Capabilities.SupportsPersistentIdentifier = true
	return out, nil
}

// authenticateByName signs a device in by a profile's name and password, as photon's own sign-in
// does: the same check, the same limits on guessing, the same record of who tried.
func (a *API) authenticateByName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
	}
	a.readWithin(w)
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxSignIn), &req, anyCase); err != nil {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	app := appOf(r)
	if req.Username == "" || app.Client == "" || app.Device == "" || app.DeviceID == "" || app.Version == "" {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	addr := a.svc.Reach.Client(r)
	byAddress, byName := auth.SignInKeys(addr, req.Username)
	if !a.allowed(w, r, auth.SignInsPerAddress, byAddress) || !a.allowed(w, r, auth.SignInsPerName, byName) {
		return
	}
	token, profile, err := a.svc.Auth.SignIn(r.Context(), req.Username, req.Pw, auth.Device{Name: app.Device, Client: app.Client})
	details := domain.SignInDetails{Name: req.Username, Device: app.Device, Client: app.Client, Address: addr.String()}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		a.svc.Raise(r.Context(), domain.Event{Kind: domain.EventSignInRefused, Details: details})
		a.refuse(w, http.StatusUnauthorized)
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	a.svc.Raise(r.Context(), domain.Event{Kind: domain.EventSignedIn, Profile: profile.ID, Details: details})
	a.writeUser(w, r, profile, token)
}

// allowed spends one attempt from key's allowance, answering 429 with Retry-After when it is
// spent, and refusing when the limits cannot be checked rather than letting attempts through.
func (a *API) allowed(w http.ResponseWriter, r *http.Request, limit kv.Limit, key string) bool {
	wait, err := a.svc.Limits.Allow(r.Context(), key, limit)
	if err != nil {
		a.internal(w, r, err)
		return false
	}
	if wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		a.refuse(w, http.StatusTooManyRequests)
		return false
	}
	return true
}

// writeUser answers a profile as Jellyfin's user, and as signed in by token where there is one.
func (a *API) writeUser(w http.ResponseWriter, r *http.Request, p domain.Profile, token string) {
	u, err := a.userOf(r.Context(), p)
	var session sessionInfo
	if err == nil && token != "" {
		session, err = a.sessionOf(r, p, token)
	}
	switch {
	case err != nil:
		a.internal(w, r, err)
	case token != "":
		a.writeJSON(w, authenticationResult{User: u, SessionInfo: session, AccessToken: token, ServerID: a.id})
	default:
		a.writeJSON(w, u)
	}
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	a.writeUser(w, r, auth.SessionOf(r.Context()).Profile, "")
}

// user answers the signed-in profile by its id, and no other, so no profile learns of another.
func (a *API) user(w http.ResponseWriter, r *http.Request) {
	p := auth.SessionOf(r.Context()).Profile
	if id, err := uuid.Parse(r.PathValue("userId")); err != nil || id != p.ID {
		a.refuse(w, http.StatusNotFound)
		return
	}
	a.writeUser(w, r, p, "")
}

// logout signs the device out. An API key is not a device and stays, revoked only by an admin.
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	s := auth.SessionOf(r.Context())
	switch s.Kind {
	case domain.SessionDevice:
		if err := a.svc.Auth.SignOut(r.Context(), s.ID); err != nil {
			a.internal(w, r, err)
			return
		}
		a.svc.Raise(r.Context(), domain.Event{Kind: domain.EventSessionsChanged, Profile: s.Profile.ID})
	case domain.SessionKey:
	}
	w.WriteHeader(http.StatusNoContent)
}

type displays interface {
	DisplayPreferences(ctx context.Context, profile uuid.UUID, client, view string) (jsontext.Value, error)
	SetDisplayPreferences(ctx context.Context, profile uuid.UUID, client, view string, prefs jsontext.Value) error
}

// maxDisplayPreferences is the most a view's display preferences may be. Jellyfin's web app keeps
// its home's sections and other settings in their CustomPrefs, which come to a few kilobytes.
const maxDisplayPreferences = 64 << 10

// displayPreferences is Jellyfin's DisplayPreferencesDto: how an app lays out a view, which it
// keeps on the server per profile and per app. Jellyfin's web app finishes signing in only once it
// has one.
type displayPreferences struct {
	ID                 string            `json:"Id"`
	ViewType           string            `json:"ViewType,omitempty"`
	SortBy             string            `json:"SortBy"`
	IndexBy            string            `json:"IndexBy,omitempty"`
	RememberIndexing   bool              `json:"RememberIndexing"`
	PrimaryImageHeight int               `json:"PrimaryImageHeight"`
	PrimaryImageWidth  int               `json:"PrimaryImageWidth"`
	CustomPrefs        map[string]string `json:"CustomPrefs"`
	ScrollDirection    string            `json:"ScrollDirection"`
	ShowBackdrop       bool              `json:"ShowBackdrop"`
	RememberSorting    bool              `json:"RememberSorting"`
	SortOrder          string            `json:"SortOrder"`
	ShowSidebar        bool              `json:"ShowSidebar"`
	Client             string            `json:"Client"`
}

// displayPreferences answers a view's display preferences as the app last saved them, else as
// Jellyfin answers ones never saved.
func (a *API) displayPreferences(w http.ResponseWriter, r *http.Request) {
	out := displayPreferences{
		SortBy: "SortName", PrimaryImageHeight: 250, PrimaryImageWidth: 250, CustomPrefs: map[string]string{},
		ScrollDirection: "Horizontal", ShowBackdrop: true, SortOrder: "Ascending",
	}
	saved, err := a.svc.Displays.DisplayPreferences(r.Context(), auth.SessionOf(r.Context()).Profile.ID, query(r, "client"), r.PathValue("id"))
	switch {
	case err == nil:
		if err := json.Unmarshal(saved, &out); err != nil {
			a.internal(w, r, err)
			return
		}
	case !isNotFound(err):
		a.internal(w, r, err)
		return
	}
	out.ID, out.Client = r.PathValue("id"), query(r, "client")
	a.writeJSON(w, out)
}

// setDisplayPreferences keeps a view's display preferences for the profile and the app it names,
// as Jellyfin keys them: by the client in the query, not the one in the body.
func (a *API) setDisplayPreferences(w http.ResponseWriter, r *http.Request) {
	client := query(r, "client")
	var prefs displayPreferences
	a.readWithin(w)
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxDisplayPreferences), &prefs, anyCase); err != nil || client == "" {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	prefs.ID, prefs.Client = r.PathValue("id"), client
	saved, err := json.Marshal(prefs)
	if err == nil {
		err = a.svc.Displays.SetDisplayPreferences(r.Context(), auth.SessionOf(r.Context()).Profile.ID, client, prefs.ID, saved)
	}
	switch {
	case errors.Is(err, store.ErrTooManyViews):
		a.refuse(w, http.StatusBadRequest)
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
