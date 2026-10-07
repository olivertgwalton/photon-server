package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/peer"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	goodToken   = "pst_good"
	memberToken = "pst_member"
)

var oliver = domain.Profile{ID: uuid.MustParse("0199b3c0-0000-7000-8000-000000000001"), Name: "Oliver", Role: domain.RoleAdmin}

type fakeAuth struct{}

func (fakeAuth) SignIn(_ context.Context, name, password string, _ auth.Device) (string, domain.Profile, error) {
	if name == "Oliver" && password == "correct horse" {
		return goodToken, oliver, nil
	}
	return "", domain.Profile{}, auth.ErrInvalidCredentials
}

func (fakeAuth) Authenticate(_ context.Context, token string) (domain.Session, error) {
	switch token {
	case goodToken:
		return domain.Session{ID: uuid.NewV7(), Profile: oliver, Device: "Living room", Client: "Photon Web 1.0"}, nil
	case memberToken:
		return domain.Session{ID: uuid.NewV7(), Profile: domain.Profile{ID: uuid.NewV7(), Name: "Kid", Role: domain.RoleMember}}, nil
	}
	return domain.Session{}, auth.ErrUnauthenticated
}

func (fakeAuth) SignOut(context.Context, uuid.UUID) error { return nil }

func (fakeAuth) StartPairing(context.Context, auth.Device) (auth.PairingStart, error) {
	return auth.PairingStart{DeviceCode: "BCDFGHJK.secret", UserCode: "BCDF-GHJK", ExpiresIn: 10 * time.Minute}, nil
}

func (fakeAuth) ApprovePairing(context.Context, domain.Session, string) (auth.Device, error) {
	return auth.Device{}, auth.ErrPairingNotFound
}

func (fakeAuth) SwitchProfile(_ context.Context, _ domain.Session, target uuid.UUID, secret string) (domain.Profile, error) {
	if target == oliver.ID && secret != "correct horse" {
		return domain.Profile{}, auth.ErrWrongSecret
	}
	return oliver, nil
}

func (fakeAuth) SetPIN(_ context.Context, _ uuid.UUID, pin string) error {
	if pin != "" && len(pin) < 4 {
		return auth.ErrPINNotDigits
	}
	return nil
}

// ChangePassword knows Oliver's password.
func (fakeAuth) ChangePassword(_ context.Context, _ domain.Session, current, password string) error {
	switch {
	case current != "correct horse":
		return auth.ErrWrongSecret
	case len(password) < 8:
		return auth.ErrPasswordTooShort
	}
	return nil
}

func (fakeAuth) Devices(context.Context, domain.Session) ([]store.DeviceListing, error) {
	return nil, nil
}

func (fakeAuth) SignOutDevice(context.Context, domain.Session, uuid.UUID) error {
	return auth.ErrDeviceNotFound
}

func (fakeAuth) CreateKey(context.Context, domain.Session, string) (uuid.UUID, string, error) {
	return uuid.NewV7(), "pst_key", nil
}

func (fakeAuth) Keys(context.Context) ([]store.KeyListing, error) { return nil, nil }

func (fakeAuth) RevokeKey(context.Context, uuid.UUID) error { return auth.ErrKeyNotFound }

func (fakeAuth) PollPairing(_ context.Context, deviceCode string) (kv.PairingState, string, domain.Profile, error) {
	if deviceCode == "BCDFGHJK.secret" {
		return kv.PairingPending, "", domain.Profile{}, nil
	}
	return kv.PairingExpired, "", domain.Profile{}, nil
}

func serve(t *testing.T, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	newAPI(nil).ServeHTTP(rec, req)
	return rec
}

// Every route for the signed-in refuses a request without a valid token.
func TestSignedInRoutesRefuseStrangers(t *testing.T) {
	for _, r := range newAPI(nil).routes() {
		if r.access != signedIn {
			continue
		}
		method, path, _ := strings.Cut(r.pattern, " ")
		for _, token := range []string{"", "pst_forged", "not even a token"} {
			rec := serve(t, method, path, token, "{}")
			if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("%s with token %q: status %d, want 401 asking for a bearer token", r.pattern, token, rec.Code)
			}
		}
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
		code problemCode
	}{
		{"right password", `{"name":"Oliver","password":"correct horse","device":"Living room","client":"Photon"}`, http.StatusOK, ""},
		{"wrong password", `{"name":"Oliver","password":"guess","device":"Living room","client":"Photon"}`, http.StatusUnauthorized, codeInvalidCredentials},
		{"an unknown field", `{"name":"Oliver","password":"correct horse","device":"d","client":"c","admin":true}`, http.StatusBadRequest, codeInvalidBody},
		{"no device", `{"name":"Oliver","password":"correct horse"}`, http.StatusBadRequest, codeInvalidBody},
		{"not json", `name=Oliver`, http.StatusBadRequest, codeInvalidBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, http.MethodPost, "/api/v1/auth/login", "", tt.body)
			if rec.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			if tt.code != "" {
				var p problem
				if err := json.NewDecoder(rec.Body).Decode(&p); err != nil || p.Code != tt.code {
					t.Errorf("problem code %q (err %v), want %q", p.Code, err, tt.code)
				}
				return
			}
			var got loginResponse
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			want := loginResponse{Token: goodToken, Profile: profileJSON{ID: oliver.ID.String(), Name: "Oliver", Role: domain.RoleAdmin}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMeIsTheSessionsProfile(t *testing.T) {
	rec := serve(t, http.MethodGet, "/api/v1/me", goodToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got profileJSON
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Oliver" || got.Role != domain.RoleAdmin {
		t.Errorf("me = %+v", got)
	}
}

func TestPollingAnswersInRFC8628Terms(t *testing.T) {
	for code, want := range map[string]problemCode{"BCDFGHJK.secret": codeAuthorizationPending, "guessed": codeExpiredToken} {
		rec := serve(t, http.MethodPost, "/api/v1/auth/device/poll", "", `{"device_code":"`+code+`"}`)
		var p problem
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil || rec.Code != http.StatusBadRequest || p.Code != want {
			t.Errorf("poll %q: %d %q (err %v), want 400 %q", code, rec.Code, p.Code, err, want)
		}
	}
}

func TestSwitchingNeedsTheLocksSecret(t *testing.T) {
	body := `{"profile_id":"` + oliver.ID.String() + `","secret":"guess"}`
	if rec := serve(t, http.MethodPut, "/api/v1/session/profile", goodToken, body); rec.Code != http.StatusForbidden {
		t.Errorf("a wrong secret: status %d, want 403", rec.Code)
	}
	body = `{"profile_id":"` + oliver.ID.String() + `","secret":"correct horse"}`
	if rec := serve(t, http.MethodPut, "/api/v1/session/profile", goodToken, body); rec.Code != http.StatusOK {
		t.Errorf("the right secret: status %d, want 200", rec.Code)
	}
	if rec := serve(t, http.MethodPut, "/api/v1/me/pin", goodToken, `{"pin":"12"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a two-digit PIN: status %d, want 400", rec.Code)
	}
}

func TestChangingYourOwnPassword(t *testing.T) {
	limits := &fakeLimiter{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Limits: limits, Events: &fakeEvents{}})
	change := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		name, token, body string
		want              int
		code              problemCode
	}{
		{"the wrong current password", goodToken, `{"current":"guess","new":"battery staple"}`, http.StatusForbidden, codeWrongSecret},
		{"a new one too short", goodToken, `{"current":"correct horse","new":"short"}`, http.StatusBadRequest, codeInvalidBody},
		{"the right one", goodToken, `{"current":"correct horse","new":"battery staple"}`, http.StatusNoContent, ""},
	} {
		rec := change(tc.token, tc.body)
		var p problem
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		if rec.Code != tc.want || p.Code != tc.code {
			t.Errorf("%s: %d %q, want %d %q", tc.name, rec.Code, p.Code, tc.want, tc.code)
		}
	}
	for range signInsPerName.Burst {
		change(goodToken, `{"current":"guess","new":"battery staple"}`)
	}
	if rec := change(goodToken, `{"current":"correct horse","new":"battery staple"}`); rec.Code != http.StatusTooManyRequests {
		t.Errorf("guessing past the limit: %d, want 429", rec.Code)
	}
}

// A client sending its body a byte at a time, or not at all, is answered rather than waited on for
// good; signing in needs no token, so anyone could hold requests open so.
func TestABodyThatNeverArrivesIsRefused(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "POST /api/v1/auth/login HTTP/1.1\r\nHost: photon\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"name\":"); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * bodyWithin)); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("no answer to a body that never came: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("a body that never came: %d, want 400", res.StatusCode)
	}
}

func TestABrowserKeepsItsSessionInACookie(t *testing.T) {
	a := newAPI(nil)
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
		`{"name":"Oliver","password":"correct horse","device":"Firefox on macOS","client":"Photon Web","keep":"cookie"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), goodToken) {
		t.Errorf("the token was answered to the page: %s", rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	c := cookies[0]
	if c.Name != "photon_session" || c.Value != goodToken || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure {
		t.Errorf("cookie %+v", c)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(c)
	logout.Header.Set("Origin", "http://example.com")
	rec = httptest.NewRecorder()
	a.ServeHTTP(rec, logout)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status %d", rec.Code)
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Errorf("logout left the cookie: %v", cookies)
	}
}

func TestTheCookieIsSecureBehindAnHTTPSProxy(t *testing.T) {
	a := newAPI(nil)
	a.svc.TrustedProxies, _ = peer.Parse("192.0.2.1")
	for peer, want := range map[string]bool{"192.0.2.1:4000": true, "198.51.100.7:4000": false} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(
			`{"name":"Oliver","password":"correct horse","device":"d","client":"c","keep":"cookie"}`))
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		if c := rec.Result().Cookies(); len(c) != 1 || c[0].Secure != want {
			t.Errorf("from %s: cookies %v, want Secure %v", peer, c, want)
		}
	}
}

func TestTheCookieIsRefusedForAnotherSitesWrites(t *testing.T) {
	cookie := &http.Cookie{Name: "photon_session", Value: goodToken}
	tests := []struct {
		name, method, origin string
		cookie               bool
		want                 int
	}{
		{"a read from anywhere", http.MethodGet, "https://evil.example", true, http.StatusOK},
		{"a write from this server's page", http.MethodPut, "http://photon.test", true, http.StatusNoContent},
		{"a write from another site", http.MethodPut, "https://evil.example", true, http.StatusForbidden},
		{"a write from another port", http.MethodPut, "http://photon.test:3000", true, http.StatusForbidden},
		{"an app's write with its token", http.MethodPut, "https://evil.example", false, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, body := "/api/v1/me", ""
			if tt.method == http.MethodPut {
				target, body = "/api/v1/me/pin", `{"pin":"2468"}`
			}
			req := httptest.NewRequest(tt.method, "http://photon.test"+target, strings.NewReader(body))
			req.Header.Set("Origin", tt.origin)
			if tt.cookie {
				req.AddCookie(cookie)
			} else {
				req.Header.Set("Authorization", "Bearer "+goodToken)
			}
			rec := httptest.NewRecorder()
			newAPI(nil).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

// An enum's value outside its list is refused as the body is read, wherever in the body it sits and
// whether or not the handler looks at it.
func TestABodyIsRefusedAValueNoneOfItsEnums(t *testing.T) {
	tests := []struct {
		path, body, detail string
	}{
		{"/api/v1/auth/login", `{"name":"Oliver","password":"correct horse","device":"d","client":"c","keep":"forever"}`, "keep is one of token, cookie"},
		{"/api/v1/titles/" + uuid.New().String() + "/play", `{"profile":{"containers":[],"video":[{"codec":"hevc","ranges":["sdr","hdr11"]}],"audio":[],"max_bitrate_kbps":0}}`, "profile.video.ranges is one of sdr, hlg, hdr10, hdr10plus, dv"},
	}
	for _, tt := range tests {
		rec := serve(t, http.MethodPost, tt.path, goodToken, tt.body)
		var p problem
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil || rec.Code != http.StatusBadRequest || p.Code != codeInvalidBody || p.Detail != tt.detail {
			t.Errorf("%s: status %d, problem %+v (err %v), want invalid_body saying %q", tt.path, rec.Code, p, err, tt.detail)
		}
	}
}

func TestAnAPIKeyNeedsAName(t *testing.T) {
	for body, want := range map[string]int{
		`{"name":"Sonarr"}`: http.StatusCreated,
		`{"name":"  "}`:     http.StatusBadRequest,
		`{"name":"` + strings.Repeat("k", 65) + `"}`: http.StatusBadRequest,
	} {
		if rec := serve(t, http.MethodPost, "/api/v1/admin/keys", goodToken, body); rec.Code != want {
			t.Errorf("%s: status %d, want %d", body, rec.Code, want)
		}
	}
}
