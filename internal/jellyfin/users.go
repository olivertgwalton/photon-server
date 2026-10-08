package jellyfin

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
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
	Name                      string          `json:"Name"`
	ServerID                  string          `json:"ServerId"`
	ID                        string          `json:"Id"`
	HasPassword               bool            `json:"HasPassword"`
	HasConfiguredPassword     bool            `json:"HasConfiguredPassword"`
	HasConfiguredEasyPassword bool            `json:"HasConfiguredEasyPassword"`
	EnableAutoLogin           bool            `json:"EnableAutoLogin"`
	Configuration             json.RawMessage `json:"Configuration"`
	Policy                    json.RawMessage `json:"Policy"`
}

// configuration is Jellyfin's UserConfiguration as a new user has it. An app that decodes one
// needs every field: the Kotlin SDK refuses one short of any.
var configuration = json.RawMessage(`{"PlayDefaultAudioTrack":true,"SubtitleLanguagePreference":"",` +
	`"DisplayMissingEpisodes":false,"GroupedFolders":[],"SubtitleMode":"Default","DisplayCollectionsView":false,` +
	`"EnableLocalPassword":false,"OrderedViews":[],"LatestItemsExcludes":[],"MyMediaExcludes":[],` +
	`"HidePlayedInLatest":true,"RememberAudioSelections":true,"RememberSubtitleSelections":true,` +
	`"EnableNextEpisodeAutoPlay":true,"CastReceiverId":"F007D354"}`)

// policy is Jellyfin's UserPolicy, complete for the same reason. No profile is an administrator
// here, even photon's admins: an app would offer Jellyfin's dashboard, which is not served, so
// the server is run from photon's own app. What photon has no part of, Live TV, SyncPlay, sharing,
// deleting and downloading through this API, is turned off so apps do not offer it.
var policy = json.RawMessage(`{"IsAdministrator":false,"IsHidden":true,"EnableCollectionManagement":false,` +
	`"EnableSubtitleManagement":false,"EnableLyricManagement":false,"IsDisabled":false,"BlockedTags":[],` +
	`"AllowedTags":[],"EnableUserPreferenceAccess":true,"AccessSchedules":[],"BlockUnratedItems":[],` +
	`"EnableRemoteControlOfOtherUsers":false,"EnableSharedDeviceControl":false,"EnableRemoteAccess":true,` +
	`"EnableLiveTvManagement":false,"EnableLiveTvAccess":false,"EnableMediaPlayback":true,` +
	`"EnableAudioPlaybackTranscoding":true,"EnableVideoPlaybackTranscoding":true,"EnablePlaybackRemuxing":true,` +
	`"ForceRemoteSourceTranscoding":false,"EnableContentDeletion":false,"EnableContentDeletionFromFolders":[],` +
	`"EnableContentDownloading":false,"EnableSyncTranscoding":false,"EnableMediaConversion":false,` +
	`"EnabledDevices":[],"EnableAllDevices":true,"EnabledChannels":[],"EnableAllChannels":true,` +
	`"EnabledFolders":[],"EnableAllFolders":true,"InvalidLoginAttemptCount":0,"LoginAttemptsBeforeLockout":-1,` +
	`"MaxActiveSessions":0,"EnablePublicSharing":false,"BlockedMediaFolders":[],"BlockedChannels":[],` +
	`"RemoteClientBitrateLimit":0,` +
	`"AuthenticationProviderId":"Jellyfin.Server.Implementations.Users.DefaultAuthenticationProvider",` +
	`"PasswordResetProviderId":"Jellyfin.Server.Implementations.Users.DefaultPasswordResetProvider",` +
	`"SyncPlayAccess":"None"}`)

func (a *API) userOf(p domain.Profile) user {
	return user{
		Name: p.Name, ServerID: a.id, ID: guid(p.ID),
		HasPassword: true, HasConfiguredPassword: true,
		Configuration: configuration, Policy: policy,
	}
}

// authenticationResult is Jellyfin's, but its SessionInfo, which no app reads.
type authenticationResult struct {
	User        user   `json:"User"`
	AccessToken string `json:"AccessToken"`
	ServerID    string `json:"ServerId"`
}

// authenticateByName signs a device in by a profile's name and password, as photon's own sign-in
// does: the same check, the same limits on guessing, the same record of who tried.
func (a *API) authenticateByName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
	}
	a.readWithin(w)
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSignIn)).Decode(&req); err != nil {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	app := appOf(r)
	if req.Username == "" || app.Client == "" || app.Device == "" || app.DeviceID == "" || app.Version == "" {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	addr := a.svc.Proxies.Client(r)
	byAddress, byName := auth.SignInKeys(addr, req.Username)
	if !a.allowed(w, r, auth.SignInsPerAddress, byAddress) || !a.allowed(w, r, auth.SignInsPerName, byName) {
		return
	}
	token, profile, err := a.svc.Auth.SignIn(r.Context(), req.Username, req.Pw, auth.Device{Name: app.Device, Client: app.Client})
	details := map[string]any{"name": req.Username, "device": app.Device, "client": app.Client, "address": addr.String()}
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
	a.writeJSON(w, authenticationResult{User: a.userOf(profile), AccessToken: token, ServerID: a.id})
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

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, a.userOf(auth.SessionOf(r.Context()).Profile))
}

// user answers the signed-in profile by its id, and no other, so no profile learns of another.
func (a *API) user(w http.ResponseWriter, r *http.Request) {
	p := auth.SessionOf(r.Context()).Profile
	if id, err := uuid.Parse(r.PathValue("userId")); err != nil || id != p.ID {
		a.refuse(w, http.StatusNotFound)
		return
	}
	a.writeJSON(w, a.userOf(p))
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
	case domain.SessionKey:
	}
	w.WriteHeader(http.StatusNoContent)
}

// displayPreferences is Jellyfin's DisplayPreferencesDto as Jellyfin answers one never saved,
// which is every one here. Jellyfin's web app finishes signing in only once it has one.
type displayPreferences struct {
	SortBy             string            `json:"SortBy"`
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

func (a *API) displayPreferences(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, displayPreferences{
		SortBy: "SortName", PrimaryImageHeight: 250, PrimaryImageWidth: 250, CustomPrefs: map[string]string{},
		ScrollDirection: "Horizontal", ShowBackdrop: true, SortOrder: "Ascending", Client: query(r, "client"),
	})
}
