package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func setupAPI(profiles listedProfiles) *API {
	return New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Profiles: profiles, Network: fakeNetwork{}, Limits: &fakeLimiter{}, Events: &fakeEvents{},
	})
}

func askSetup(t *testing.T, api *API, method, from, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/setup", strings.NewReader(body))
	// A reverse proxy on the network passes on a client from anywhere.
	if proxy, ok := strings.CutSuffix(from, " via proxy"); ok {
		from = proxy
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
	}
	req.RemoteAddr = from + ":50000"
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

const setupBody = `{"name":"Oliver","password":"correct horse","device":"Laptop","client":"Photon Web 1.0","keep":"cookie"}`

// A new server is set up once, from its local network: a client elsewhere is told to set it up
// from there, and a server with a profile is set up already.
func TestANewServerIsSetUpOnceFromItsLocalNetwork(t *testing.T) {
	for _, tc := range []struct {
		name     string
		profiles listedProfiles
		from     string
		state    setupState
		status   int
	}{
		{"a new server, asked from its network", nil, "192.168.1.20", setupOpen, http.StatusOK},
		{"a new server, asked from the server itself", nil, "127.0.0.1", setupOpen, http.StatusOK},
		{"a new server, asked from the internet", nil, "203.0.113.9", setupLocalOnly, http.StatusForbidden},
		{"a new server, asked through a proxy it does not trust", nil, "192.168.1.2 via proxy", setupLocalOnly, http.StatusForbidden},
		{"a server set up", listedProfiles{oliver}, "192.168.1.20", setupDone, http.StatusConflict},
	} {
		api := setupAPI(tc.profiles)
		var got setupJSON
		if err := json.NewDecoder(askSetup(t, api, http.MethodGet, tc.from, "").Body).Decode(&got); err != nil || got.State != tc.state {
			t.Errorf("%s: state %q (%v), want %q", tc.name, got.State, err, tc.state)
		}
		if rec := askSetup(t, api, http.MethodPost, tc.from, setupBody); rec.Code != tc.status {
			t.Errorf("%s: setting up answered %d, want %d", tc.name, rec.Code, tc.status)
		}
	}
}

// Setting up signs the browser in as the new admin, by the cookie it asked for.
func TestSettingUpSignsInAsTheAdmin(t *testing.T) {
	rec := askSetup(t, setupAPI(nil), http.MethodPost, "192.168.1.20", setupBody)
	var got loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Profile.Role != domain.RoleAdmin || got.Token != "" || !strings.Contains(rec.Header().Get("Set-Cookie"), sessionCookie+"="+goodToken) {
		t.Errorf("answered %+v with cookie %q; want the admin, its token in the cookie alone", got, rec.Header().Get("Set-Cookie"))
	}
}

func TestSettingUpRefusesAShortPassword(t *testing.T) {
	body := strings.Replace(setupBody, "correct horse", "short", 1)
	if rec := askSetup(t, setupAPI(nil), http.MethodPost, "192.168.1.20", body); rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}
