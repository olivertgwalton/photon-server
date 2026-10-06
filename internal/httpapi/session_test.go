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

// ChangePassword knows Oliver's password; the member is a household profile with none.
func (fakeAuth) ChangePassword(_ context.Context, s domain.Session, current, password string) error {
	switch {
	case s.Profile.ID != oliver.ID:
		return auth.ErrNoPassword
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
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, Limits: limits, Events: &fakeEvents{}})
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
		{"a household profile", memberToken, `{"current":"","new":"battery staple"}`, http.StatusConflict, codeConflict},
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
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}}))
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
