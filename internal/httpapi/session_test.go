package httpapi

import (
	"context"
	"encoding/json"
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
)

const goodToken = "pst_good"

var oliver = domain.Profile{ID: uuid.MustParse("0199b3c0-0000-7000-8000-000000000001"), Name: "Oliver", Role: domain.RoleAdmin}

type fakeAuth struct{}

func (fakeAuth) SignIn(_ context.Context, name, password string, _ auth.Device) (string, domain.Profile, error) {
	if name == "Oliver" && password == "correct horse" {
		return goodToken, oliver, nil
	}
	return "", domain.Profile{}, auth.ErrInvalidCredentials
}

func (fakeAuth) Authenticate(_ context.Context, token string) (domain.Session, error) {
	if token == goodToken {
		return domain.Session{ID: uuid.NewV7(), Profile: oliver}, nil
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

// Every route not marked public refuses a request without a valid token.
func TestSignedInRoutesRefuseStrangers(t *testing.T) {
	for _, r := range newAPI(nil).routes() {
		if r.access == public {
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
