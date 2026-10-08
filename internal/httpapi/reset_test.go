package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func resetAPI(log *bytes.Buffer) *API {
	return New(slog.New(slog.NewTextHandler(log, nil)), domain.Info{}, Services{
		Auth: fakeAuth{}, Network: fakeNetwork{}, Limits: &fakeLimiter{}, Placer: alone(nil, false),
	})
}

func askReset(t *testing.T, api *API, target, from, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.RemoteAddr = from + ":50000"
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

// A reset is asked for from the server's network alone, and answered the same whether or not a
// profile has the name; only the server's log has the code.
func TestAResetsCodeIsWrittenToTheLogAlone(t *testing.T) {
	var log bytes.Buffer
	api := resetAPI(&log)
	if rec := askReset(t, api, "/api/v1/auth/password-resets", "203.0.113.9", `{"name":"Oliver"}`); rec.Code != http.StatusForbidden {
		t.Errorf("asked from the internet: %d, want 403", rec.Code)
	}
	known := askReset(t, api, "/api/v1/auth/password-resets", "192.168.1.20", `{"name":"Oliver"}`)
	unknown := askReset(t, api, "/api/v1/auth/password-resets", "192.168.1.20", `{"name":"Nobody"}`)
	if known.Code != http.StatusOK || known.Body.String() != unknown.Body.String() {
		t.Errorf("a known name answered %d %q, an unknown one %q; want the same", known.Code, known.Body, unknown.Body)
	}
	if strings.Contains(known.Body.String(), "BCDF") || !strings.Contains(log.String(), "code=BCDF-GHJK") {
		t.Errorf("the code was answered, or not logged:\n%s", log.String())
	}
}

func TestAResetIsRedeemedByItsCode(t *testing.T) {
	api := resetAPI(&bytes.Buffer{})
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"the code, as typed", `{"code":"bcdf-ghjk","password":"new horse battery"}`, http.StatusNoContent},
		{"another code", `{"code":"ZZZZ-ZZZZ","password":"new horse battery"}`, http.StatusNotFound},
		{"a short password", `{"code":"bcdf-ghjk","password":"short"}`, http.StatusBadRequest},
	} {
		if rec := askReset(t, api, "/api/v1/auth/password-resets/redemptions", "203.0.113.9", tc.body); rec.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}
