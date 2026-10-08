package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// The header as each app sends it: Swiftfin unquoted and in any order, the Kotlin and TypeScript
// SDKs quoted and URL-encoded, the TypeScript one with an empty token before signing in.
func TestTheAuthorizationHeaderIsReadAsEachAppWritesIt(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		want         app
	}{
		{
			"Swiftfin", `MediaBrowser DeviceId=iOS_1A2B, Client=Swiftfin iOS, Version=1.6.1, Device=iPhone, Token=pst_abc`,
			app{Client: "Swiftfin iOS", Device: "iPhone", DeviceID: "iOS_1A2B", Version: "1.6.1", Token: "pst_abc"},
		},
		{
			"Kotlin SDK", `MediaBrowser Client="Jellyfin%20Android%20TV", Version="0.19.4", DeviceId="ZDk1", Device="Living%20room", Token="pst_abc"`,
			app{Client: "Jellyfin Android TV", Device: "Living room", DeviceID: "ZDk1", Version: "0.19.4", Token: "pst_abc"},
		},
		{
			"TypeScript SDK before signing in", `MediaBrowser Client="Jellyfin%20Web", Device="Firefox", DeviceId="TW96", Version="12.2.0", Token=""`,
			app{Client: "Jellyfin Web", Device: "Firefox", DeviceID: "TW96", Version: "12.2.0"},
		},
		{
			"a comma inside quotes, and + as a space", `MediaBrowser Client="Infuse, Direct", Device="Apple+TV"`,
			app{Client: "Infuse, Direct", Device: "Apple TV"},
		},
		{"keys are case-sensitive", `MediaBrowser client="x", Token="pst_abc"`, app{Token: "pst_abc"}},
		{"another scheme says nothing", `Bearer pst_abc`, app{}},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", tc.header)
		if got := appOf(r); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/socket?apikey=pst_q", nil)
	r.Header.Set("Authorization", `MediaBrowser Client="Jellyfin Web", Token=""`)
	if got := appOf(r).Token; got != "pst_q" {
		t.Errorf("an empty header token with ApiKey in the query: %q, want the query's", got)
	}
}

var (
	serverID = uuid.MustParse("3f2504e0-4f89-41d3-9a0c-0305e82c3301")
	ada      = domain.Profile{
		ID: uuid.MustParse("8d2b4c1e-0f6a-4b7c-9e2d-1a3b5c7d9e0f"), Name: "Ada", Role: domain.RoleAdmin,
		Avatar: uuid.MustParse("0199b3c0-0000-7000-8000-00000000a7a2"),
	}
)

type fakeAuth struct {
	signedOut *[]uuid.UUID
	// pairing is the one pairing, under the code 042517 and the device code 042517.secret.
	pairing *kv.PairingState
}

func (fakeAuth) SignIn(_ context.Context, name, password string, _ auth.Device) (string, domain.Profile, error) {
	if name == "Ada" && password == "correct horse" {
		return "pst_device", ada, nil
	}
	return "", domain.Profile{}, auth.ErrInvalidCredentials
}

func (fakeAuth) Authenticate(_ context.Context, token string) (domain.Session, error) {
	switch token {
	case "pst_device":
		return domain.Session{ID: uuid.MustParse("00000000-0000-0000-0000-00000000000d"), Kind: domain.SessionDevice, Profile: ada}, nil
	case "pst_key":
		return domain.Session{ID: uuid.MustParse("00000000-0000-0000-0000-00000000000e"), Kind: domain.SessionKey, Profile: ada}, nil
	}
	return domain.Session{}, auth.ErrUnauthenticated
}

func (f fakeAuth) SignOut(_ context.Context, id uuid.UUID) error {
	*f.signedOut = append(*f.signedOut, id)
	return nil
}

// StartPairing refuses photon's letters: Swiftfin takes six digits and nothing else.
func (f fakeAuth) StartPairing(_ context.Context, _ auth.Device, style auth.CodeStyle) (auth.PairingStart, error) {
	if style != auth.CodeDigits {
		return auth.PairingStart{}, errors.ErrUnsupported
	}
	*f.pairing = kv.PairingPending
	return auth.PairingStart{DeviceCode: "042517.secret", UserCode: "042517", ExpiresIn: 10 * time.Minute}, nil
}

func (f fakeAuth) ApprovePairing(_ context.Context, _ domain.Session, userCode string) (auth.Device, error) {
	if userCode != "042517" || *f.pairing != kv.PairingPending {
		return auth.Device{}, auth.ErrPairingNotFound
	}
	*f.pairing = kv.PairingApproved
	return auth.Device{Name: "Living room", Client: "Jellyfin Android TV"}, nil
}

func (f fakeAuth) PairingStatus(_ context.Context, deviceCode string) (kv.PairingState, auth.Pairing, error) {
	if deviceCode != "042517.secret" || *f.pairing == "" {
		return kv.PairingExpired, auth.Pairing{}, nil
	}
	return *f.pairing, auth.Pairing{
		Device: auth.Device{Name: "Living room", Client: "Jellyfin Android TV"}, UserCode: "042517", Started: time.Now(),
	}, nil
}

func (f fakeAuth) PollPairing(_ context.Context, deviceCode string) (kv.PairingState, string, domain.Profile, error) {
	if deviceCode != "042517.secret" || *f.pairing == "" {
		return kv.PairingExpired, "", domain.Profile{}, nil
	}
	if *f.pairing != kv.PairingApproved {
		return *f.pairing, "", domain.Profile{}, nil
	}
	*f.pairing = kv.PairingExpired
	return kv.PairingApproved, "pst_device", ada, nil
}

type fakeLimits struct{ spent bool }

func (f *fakeLimits) Allow(context.Context, string, kv.Limit) (time.Duration, error) {
	if f.spent {
		return 3 * time.Second, nil
	}
	return 0, nil
}

func newAPI() (*API, *[]uuid.UUID, *fakeLimits, *[]domain.EventKind) {
	signedOut, limits, raised := &[]uuid.UUID{}, &fakeLimits{}, &[]domain.EventKind{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: serverID.String(), Name: "Den"}, Services{
		Auth: fakeAuth{signedOut: signedOut, pairing: new(kv.PairingState)}, Limits: limits,
		Raise: func(_ context.Context, e domain.Event) { *raised = append(*raised, e.Kind) },
	})
	return api, signedOut, limits, raised
}

const kotlin = `MediaBrowser Client="Jellyfin%20Android%20TV", Version="0.19.4", DeviceId="ZDk1", Device="Living%20room"`

func serve(api http.Handler, method, target, header, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w
}

func object(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("%d %q: %v", w.Code, w.Body, err)
	}
	return m
}

// requireKeys fails for each key an app's decoder needs and m lacks, or holds as null.
func requireKeys(t *testing.T, what string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := m[k]; !ok || v == nil {
			t.Errorf("%s has no %s: the app cannot decode it", what, k)
		}
	}
}

var hexID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// An app adding the server reads its public information first, whatever case it asks in, and
// keeps the server only if it is a "Jellyfin Server" it can compare the version of.
func TestAnAppFindsAJellyfinServer(t *testing.T) {
	api, _, _, _ := newAPI()
	for _, path := range []string{"/System/Info/Public", "/system/info/public", "/System/Info/Public/"} {
		w := serve(api, http.MethodGet, path, "", "")
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%s: %d %s", path, w.Code, w.Header().Get("Content-Type"))
		}
		info := object(t, w)
		if info["ProductName"] != "Jellyfin Server" || info["Version"] != "12.2.0" || info["ServerName"] != "Den" ||
			info["StartupWizardCompleted"] != true || info["LocalAddress"] != "http://example.com" {
			t.Errorf("%s: %v", path, info)
		}
		if id, _ := info["Id"].(string); !hexID.MatchString(id) {
			t.Errorf("%s: Id %q, want 32 lowercase hex digits", path, id)
		}
	}
	for path, want := range map[string]string{
		"/System/Ping": `"Jellyfin Server"`, "/QuickConnect/Enabled": "true", "/Users/Public": "[]",
		"/Branding/Configuration": `{"SplashscreenEnabled":false}`, "/Branding/Css.css": "",
	} {
		if w := serve(api, http.MethodGet, path, "", ""); w.Code != http.StatusOK || w.Body.String() != want {
			t.Errorf("%s: %d %q, want 200 %q", path, w.Code, w.Body, want)
		}
	}
	// An app takes 401 to mean it was signed out, so what is not served is 404.
	if w := serve(api, http.MethodGet, "/Streamyfin/config", "MediaBrowser Token=pst_device", ""); w.Code != http.StatusNotFound {
		t.Errorf("an unserved route: %d, want 404", w.Code)
	}
}

// A sign-in answers what every app needs of it, a user and a token, in a shape the Kotlin SDK the
// Android apps ship decodes in full; and is refused as Jellyfin refuses one.
func TestAnAppSignsIn(t *testing.T) {
	api, _, limits, raised := newAPI()
	signIn := func(header, body string) *httptest.ResponseRecorder {
		return serve(api, http.MethodPost, "/users/authenticatebyname", header, body)
	}
	if w := signIn(`MediaBrowser Client="Jellyfin Web"`, `{"Username":"Ada","Pw":"correct horse"}`); w.Code != http.StatusBadRequest {
		t.Errorf("without its device: %d, want 400", w.Code)
	}
	if w := signIn(kotlin, `{"Username":"Ada","Pw":"guess"}`); w.Code != http.StatusUnauthorized || w.Body.String() != "Error processing request." {
		t.Errorf("a wrong password: %d %q, want 401", w.Code, w.Body)
	}
	w := signIn(kotlin, `{"username":"Ada","pw":"correct horse"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("signing in: %d %q", w.Code, w.Body)
	}
	result := object(t, w)
	public := object(t, serve(api, http.MethodGet, "/System/Info/Public", "", ""))
	u, _ := result["User"].(map[string]any)
	if result["AccessToken"] != "pst_device" || result["ServerId"] != public["Id"] || u["ServerId"] != public["Id"] ||
		u["Id"] != "8d2b4c1e0f6a4b7c9e2d1a3b5c7d9e0f" || u["Name"] != "Ada" {
		t.Errorf("signed in as %v", result)
	}
	checkUser(t, u)
	limits.spent = true
	if w := signIn(kotlin, `{"Username":"Ada","Pw":"correct horse"}`); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "3" {
		t.Errorf("past the limit: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if want := []domain.EventKind{domain.EventSignInRefused, domain.EventSignedIn}; strings.Join(kinds(*raised), " ") != strings.Join(kinds(want), " ") {
		t.Errorf("recorded %v, want %v", *raised, want)
	}
}

func kinds(ks []domain.EventKind) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}

// checkUser fails for what the Kotlin SDK 1.8.12, which the Android apps ship, needs of a
// UserDto, and for an enum value Swift's decoder would refuse.
func checkUser(t *testing.T, u map[string]any) {
	t.Helper()
	requireKeys(t, "UserDto", u, "Id", "HasPassword", "HasConfiguredPassword", "HasConfiguredEasyPassword")
	c, _ := u["Configuration"].(map[string]any)
	requireKeys(t, "UserConfiguration", c, "PlayDefaultAudioTrack", "DisplayMissingEpisodes", "GroupedFolders",
		"SubtitleMode", "DisplayCollectionsView", "EnableLocalPassword", "OrderedViews", "LatestItemsExcludes",
		"MyMediaExcludes", "HidePlayedInLatest", "RememberAudioSelections", "RememberSubtitleSelections",
		"EnableNextEpisodeAutoPlay")
	p, _ := u["Policy"].(map[string]any)
	requireKeys(t, "UserPolicy", p, "IsAdministrator", "IsHidden", "IsDisabled", "EnableUserPreferenceAccess",
		"EnableRemoteControlOfOtherUsers", "EnableSharedDeviceControl", "EnableRemoteAccess", "EnableLiveTvManagement",
		"EnableLiveTvAccess", "EnableMediaPlayback", "EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding",
		"EnablePlaybackRemuxing", "ForceRemoteSourceTranscoding", "EnableContentDeletion", "EnableContentDownloading",
		"EnableSyncTranscoding", "EnableMediaConversion", "EnableAllDevices", "EnableAllChannels", "EnableAllFolders",
		"InvalidLoginAttemptCount", "LoginAttemptsBeforeLockout", "MaxActiveSessions", "RemoteClientBitrateLimit",
		"EnablePublicSharing", "AuthenticationProviderId", "PasswordResetProviderId", "SyncPlayAccess")
	if c["SubtitleMode"] != "Default" || p["SyncPlayAccess"] != "None" {
		t.Errorf("SubtitleMode %v, SyncPlayAccess %v: an enum value the apps know", c["SubtitleMode"], p["SyncPlayAccess"])
	}
	// An app offers to download a title only where the policy lets it, and deleting nowhere.
	if p["EnableContentDownloading"] != true || p["EnableContentDeletion"] != false {
		t.Errorf("downloading %v, deleting %v: want downloading alone", p["EnableContentDownloading"], p["EnableContentDeletion"])
	}
}

// A signed-in app reads who it is and the server it is on, by the token in its header or in
// ApiKey; a missing or unknown token is 401, and no profile reads another.
func TestASignedInAppReadsItsUserAndServer(t *testing.T) {
	api, _, _, _ := newAPI()
	header := kotlin + `, Token="pst_device"`
	for _, tc := range []struct {
		target, header string
		want           int
	}{
		{"/Users/Me", header, http.StatusOK},
		{"/users/me?api_key=pst_device", "", http.StatusUnauthorized},
		{"/Users/Me?ApiKey=pst_device", "", http.StatusOK},
		{"/Users/Me", "", http.StatusUnauthorized},
		{"/Users/Me", kotlin + `, Token="pst_unknown"`, http.StatusUnauthorized},
		{"/Users/8d2b4c1e0f6a4b7c9e2d1a3b5c7d9e0f", header, http.StatusOK},
		{"/Users/" + uuid.NewV7().String(), header, http.StatusNotFound},
		{"/System/Info", header, http.StatusOK},
		{"/DisplayPreferences/usersettings?userId=8d2b4c1e0f6a4b7c9e2d1a3b5c7d9e0f&client=emby", header, http.StatusOK},
	} {
		w := serve(api, http.MethodGet, tc.target, tc.header, "")
		if w.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.target, w.Code, tc.want)
			continue
		}
		if w.Code != http.StatusOK {
			continue
		}
		m := object(t, w)
		switch {
		case strings.HasPrefix(tc.target, "/Users/"):
			checkUser(t, m)
		case tc.target == "/System/Info":
			requireKeys(t, "SystemInfo", m, "HasPendingRestart", "IsShuttingDown", "SupportsLibraryMonitor",
				"WebSocketPortNumber", "Id", "Version", "ProductName")
		default:
			requireKeys(t, "DisplayPreferencesDto", m, "RememberIndexing", "PrimaryImageHeight", "PrimaryImageWidth",
				"ScrollDirection", "ShowBackdrop", "RememberSorting", "SortOrder", "ShowSidebar", "CustomPrefs")
			if m["Client"] != "emby" {
				t.Errorf("display preferences for %v, want emby's", m["Client"])
			}
		}
	}
}

// An app says what it can do straight after signing in, and goes on only once that is taken.
func TestAnAppSaysWhatItCanDo(t *testing.T) {
	api, _, _, _ := newAPI()
	for _, tc := range []struct{ target, body string }{
		{"/Sessions/Capabilities?playableMediaTypes=Video&supportedCommands=DisplayMessage&supportsMediaControl=false", ""},
		{"/Sessions/Capabilities/Full", `{"PlayableMediaTypes":["Video"],"SupportedCommands":["DisplayMessage"],"SupportsMediaControl":false}`},
	} {
		if w := serve(api, http.MethodPost, tc.target, kotlin+`, Token="pst_device"`, tc.body); w.Code != http.StatusNoContent {
			t.Errorf("%s: %d, want 204", tc.target, w.Code)
		}
		if w := serve(api, http.MethodPost, tc.target, kotlin, tc.body); w.Code != http.StatusUnauthorized {
			t.Errorf("%s signed out: %d, want 401", tc.target, w.Code)
		}
	}
}

// Signing out ends the device's session, and only a device's: an API key stays until an admin
// revokes it.
func TestAnAppSignsOut(t *testing.T) {
	api, signedOut, _, _ := newAPI()
	for _, token := range []string{"pst_key", "pst_device"} {
		if w := serve(api, http.MethodPost, "/Sessions/Logout", `MediaBrowser Token="`+token+`"`, ""); w.Code != http.StatusNoContent {
			t.Errorf("%s: %d, want 204", token, w.Code)
		}
	}
	if len(*signedOut) != 1 || (*signedOut)[0] != uuid.MustParse("00000000-0000-0000-0000-00000000000d") {
		t.Errorf("signed out %v, want the device's session alone", *signedOut)
	}
}

// Jellyfin's web app, served from another origin, may ask before it signs in.
func TestAWebAppElsewhereMayAsk(t *testing.T) {
	api, _, _, _ := newAPI()
	r := httptest.NewRequest(http.MethodOptions, "/Users/AuthenticateByName", nil)
	r.Header.Set("Origin", "https://jellyfin.example")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" ||
		w.Header().Get("Access-Control-Allow-Headers") != "authorization,content-type" {
		t.Errorf("preflight: %d %v", w.Code, w.Header())
	}
}

// An app signs in by Quick Connect: it shows a code, asks after it every few seconds while a
// signed-in app approves it, and then takes its token, once.
func TestAnAppSignsInByQuickConnect(t *testing.T) {
	api, _, _, _ := newAPI()
	signedIn := `MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="TW96", Version="12.2.0", Token="pst_device"`
	connect := func() *httptest.ResponseRecorder {
		return serve(api, http.MethodGet, "/QuickConnect/Connect?secret=042517.secret", kotlin, "")
	}
	authenticate := func() *httptest.ResponseRecorder {
		return serve(api, http.MethodPost, "/Users/AuthenticateWithQuickConnect", kotlin, `{"Secret":"042517.secret"}`)
	}

	if w := serve(api, http.MethodPost, "/QuickConnect/Initiate", `MediaBrowser Client="Jellyfin Web"`, ""); w.Code != http.StatusBadRequest {
		t.Errorf("initiating without its device: %d, want 400", w.Code)
	}
	w := serve(api, http.MethodPost, "/QuickConnect/Initiate", kotlin, "")
	if w.Code != http.StatusOK {
		t.Fatalf("initiating: %d %q", w.Code, w.Body)
	}
	started := object(t, w)
	requireKeys(t, "QuickConnectResult", started, "Authenticated", "Secret", "Code", "DeviceId", "DeviceName",
		"AppName", "AppVersion", "DateAdded")
	if started["Authenticated"] != false || started["Secret"] != "042517.secret" || started["Code"] != "042517" ||
		started["DeviceId"] != "ZDk1" || started["DeviceName"] != "Living room" || started["AppName"] != "Jellyfin Android TV" {
		t.Errorf("initiated %v", started)
	}

	if w := serve(api, http.MethodGet, "/QuickConnect/Connect?secret=nobody.secret", kotlin, ""); w.Code != http.StatusNotFound {
		t.Errorf("an unknown secret: %d, want 404", w.Code)
	}
	if w := connect(); w.Code != http.StatusOK || object(t, w)["Authenticated"] != false {
		t.Errorf("before approval: %d %q", w.Code, w.Body)
	}
	if w := authenticate(); w.Code != http.StatusNotFound {
		t.Errorf("authenticating before approval: %d, want 404", w.Code)
	}

	authorize := func(query, header string) *httptest.ResponseRecorder {
		return serve(api, http.MethodPost, "/QuickConnect/Authorize?"+query, header, "")
	}
	if w := authorize("code=042517", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("approving signed out: %d, want 401", w.Code)
	}
	if w := authorize("code=042517&userId="+guid(uuid.NewV7()), signedIn); w.Code != http.StatusForbidden {
		t.Errorf("approving for another profile: %d, want 403", w.Code)
	}
	if w := authorize("code=999999", signedIn); w.Code != http.StatusNotFound {
		t.Errorf("approving a code nobody was shown: %d, want 404", w.Code)
	}
	if w := authorize("code=042517&userId="+guid(ada.ID), signedIn); w.Code != http.StatusOK || w.Body.String() != "true\n" {
		t.Fatalf("approving: %d %q", w.Code, w.Body)
	}

	for range 3 {
		if w := connect(); w.Code != http.StatusOK || object(t, w)["Authenticated"] != true {
			t.Errorf("after approval: %d %q", w.Code, w.Body)
		}
	}
	w = authenticate()
	if w.Code != http.StatusOK {
		t.Fatalf("authenticating: %d %q", w.Code, w.Body)
	}
	result := object(t, w)
	token, _ := result["AccessToken"].(string)
	if u, _ := result["User"].(map[string]any); u["Id"] != guid(ada.ID) {
		t.Errorf("signed in as %v", result)
	}
	if w := serve(api, http.MethodGet, "/Users/Me", kotlin+`, Token="`+token+`"`, ""); w.Code != http.StatusOK {
		t.Errorf("the token: %d, want it to sign the app in", w.Code)
	}
	if w := authenticate(); w.Code != http.StatusNotFound {
		t.Errorf("authenticating twice: %d, want 404", w.Code)
	}
}

// onePicture is a server holding one picture, kept in file.
type onePicture struct {
	catalogue
	id   uuid.UUID
	file string
}

func (p onePicture) Picture(_ context.Context, id uuid.UUID) (domain.Picture, error) {
	if id != p.id {
		return domain.Picture{}, store.ErrNotFound
	}
	return domain.Picture{Kept: true}, nil
}

func (p onePicture) Open(context.Context, uuid.UUID, domain.Picture, int, int) (blob.Object, string, error) {
	f, err := os.Open(p.file)
	if err != nil {
		return blob.Object{}, "", err
	}
	o, err := blob.OfFile(f)
	return o, "avatar.png", err
}

// Kept finds no theme tune: the server holds a picture alone.
func (onePicture) Kept(context.Context, uuid.UUID) (blob.Object, error) {
	return blob.Object{}, os.ErrNotExist
}

// An app shows its profile's picture by the tag its user carries, under either route, as a page
// shows one: without a token.
func TestAnAppShowsAProfilesPicture(t *testing.T) {
	file := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(file, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pictures := onePicture{id: ada.Avatar, file: file}
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: serverID.String(), Name: "Den"}, Services{
		Auth: fakeAuth{}, Catalogue: pictures, Pictures: pictures,
	})
	tag, _ := object(t, serve(api, http.MethodGet, "/Users/Me", kotlin+`, Token="pst_device"`, ""))["PrimaryImageTag"].(string)
	if tag != guid(ada.Avatar) {
		t.Fatalf("PrimaryImageTag %q, want the picture's", tag)
	}
	for _, target := range []string{
		"/Users/" + guid(ada.ID) + "/Images/Primary?tag=" + tag + "&maxWidth=96",
		"/UserImage?userId=" + guid(ada.ID) + "&tag=" + tag,
	} {
		if w := serve(api, http.MethodGet, target, "", ""); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d %s, want the PNG", target, w.Code, w.Header().Get("Content-Type"))
		}
	}
	for _, target := range []string{"/UserImage?userId=" + guid(ada.ID), "/UserImage?tag=" + guid(uuid.NewV7())} {
		if w := serve(api, http.MethodGet, target, "", ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, w.Code)
		}
	}
}
