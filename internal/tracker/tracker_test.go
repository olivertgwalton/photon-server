//go:build integration

package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// fakeTracker is Trakt or Simkl, as each answers a device signing in through the app clientID:
// its code is pending until entered, or gone once expired. It grants access, refreshed by refresh,
// and keeps what it is told of plays.
type fakeTracker struct {
	t        *testing.T
	tracker  domain.Tracker
	clientID string

	mu               sync.Mutex
	entered, expired bool
	revoked          []string
	access, refresh  string
	refreshes        int
	scrobbled        []scrobbled
}

type scrobbled struct {
	action string
	body   map[string]any
}

func newFake(t *testing.T, tracker domain.Tracker, clientID string) *fakeTracker {
	return &fakeTracker{t: t, tracker: tracker, clientID: clientID, access: "access", refresh: "refresh"}
}

func (f *fakeTracker) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeTracker) serve() string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body map[string]any
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Errorf("%s %s: %v", f.tracker, r.URL.Path, err)
			}
		}
		app := r.Header.Get("trakt-api-key")
		if f.tracker == domain.TrackerSimkl {
			app = r.URL.Query().Get("client_id")
		}
		named := r.Header.Get("User-Agent") == "Photon/1.2.3"
		if f.tracker == domain.TrackerSimkl {
			named = named && r.URL.Query().Get("app-name") == "Photon" && r.URL.Query().Get("app-version") == "1.2.3"
		}
		if app != f.clientID || !named {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		answer := func(code int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			if err := json.NewEncoder(w).Encode(v); err != nil {
				f.t.Error(err)
			}
		}
		granted := map[string]any{"access_token": f.access, "refresh_token": f.refresh, "expires_in": 604800}
		if action, ok := strings.CutPrefix(r.URL.Path, "/scrobble/"); ok {
			if r.Header.Get("Authorization") != "Bearer "+f.access {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f.scrobbled = append(f.scrobbled, scrobbled{action, body})
			answer(http.StatusCreated, map[string]any{"action": action})
			return
		}
		switch string(f.tracker) + " " + r.Method + " " + r.URL.Path {
		case "trakt POST /oauth/device/code":
			answer(http.StatusOK, map[string]any{
				"device_code": "device", "user_code": "TRAKT123", "verification_url": "https://trakt.tv/activate",
				"expires_in": 600, "interval": 0,
			})
		case "trakt POST /oauth/device/token":
			switch {
			case body["code"] != "device" || f.expired:
				w.WriteHeader(http.StatusGone)
			case !f.entered:
				w.WriteHeader(http.StatusBadRequest)
			default:
				answer(http.StatusOK, granted)
			}
		case "trakt GET /users/settings", "simkl GET /users/settings":
			if r.Header.Get("Authorization") != "Bearer "+f.access {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			answer(http.StatusOK, map[string]any{"user": map[string]string{"username": "oliver", "name": "oliver"}})
		case "trakt POST /oauth/revoke":
			f.revoked = append(f.revoked, fmt.Sprint(body["token"]))
		case "trakt POST /oauth/token":
			// Trakt's refresh tokens are each used once.
			if body["refresh_token"] != f.refresh || body["grant_type"] != "refresh_token" || body["redirect_uri"] != deviceRedirect {
				answer(http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
				return
			}
			f.refreshes++
			f.access, f.refresh = "refreshed", "refresh-again"
			answer(http.StatusOK, map[string]any{"access_token": f.access, "refresh_token": f.refresh, "expires_in": 604800})
		case "simkl POST /oauth2/device":
			if !strings.Contains(fmt.Sprint(body["scope"]), "media:write") {
				f.t.Errorf("simkl asked for %q, which writes nothing", body["scope"])
			}
			answer(http.StatusOK, map[string]any{
				"device_code": "device", "user_code": "BDWP-HQPK", "verification_uri": "https://simkl.com/pin",
				"verification_uri_complete": "https://simkl.com/pin?user_code=BDWP-HQPK", "expires_in": 900, "interval": 0,
			})
		case "simkl POST /oauth2/token":
			switch {
			case body["grant_type"] == "refresh_token" && body["refresh_token"] == f.refresh:
				// Simkl's refresh token stays, and its answer may leave it out.
				f.refreshes++
				f.access = "refreshed"
				answer(http.StatusOK, map[string]any{"access_token": f.access, "expires_in": 604800})
			case body["grant_type"] == "refresh_token":
				answer(http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			case body["device_code"] != "device" || f.expired:
				answer(http.StatusBadRequest, map[string]string{"error": "expired_token"})
			case !f.entered:
				answer(http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
			default:
				answer(http.StatusOK, granted)
			}
		case "simkl POST /oauth2/revoke":
			f.revoked = append(f.revoked, fmt.Sprint(body["token"]))
		default:
			f.t.Errorf("%s was asked %s %s", f.tracker, r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	f.t.Cleanup(srv.Close)
	return srv.URL
}

func TestAProfileLinksItsAccountOnEachTrackerByACode(t *testing.T) {
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	profile, err := st.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	var told []domain.Event
	links := New(st, k, func(_ context.Context, e domain.Event) { told = append(told, e) }, "1.2.3", log)
	// The tracker is asked after a code each time a node looks, its interval being none.
	follow := func() {
		t.Helper()
		if err := links.follow(ctx); err != nil {
			t.Fatal(err)
		}
	}
	toldOf := func(tr domain.Tracker) bool {
		t.Helper()
		was := slices.ContainsFunc(told, func(e domain.Event) bool {
			return e.Kind == domain.EventTrackerChanged && e.Profile == profile.ID && e.Details == domain.TrackerDetails{Tracker: tr}
		})
		told = nil
		return was
	}
	trakt := newFake(t, domain.TrackerTrakt, "trakt-app")
	simkl := newFake(t, domain.TrackerSimkl, "simkl-app")
	links.services[domain.TrackerTrakt] = trakt.at(trakt.serve())
	links.services[domain.TrackerSimkl] = simkl.at(simkl.serve())

	for _, tc := range []struct {
		fake         *fakeTracker
		code, enter  string
		revokedToken string
	}{
		{trakt, "TRAKT123", "https://trakt.tv/activate/TRAKT123", "access"},
		{simkl, "BDWP-HQPK", "https://simkl.com/pin?user_code=BDWP-HQPK", "refresh"},
	} {
		tr := tc.fake.tracker
		state := func() Status {
			t.Helper()
			all, err := links.Trackers(ctx, profile.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range all {
				if s.Tracker == tr {
					return s
				}
			}
			t.Fatalf("%s is not among the trackers", tr)
			return Status{}
		}
		if s := state(); s.State != StateUnavailable {
			t.Errorf("%s before an admin sets it up: %s", tr, s.State)
		}
		if _, err := links.Link(ctx, profile.ID, tr); !errors.Is(err, ErrRefused) {
			t.Errorf("%s linked before an admin set it up: %v", tr, err)
		}
		if err := st.SetTrackerClient(ctx, tr, "not-an-app"); err != nil {
			t.Fatal(err)
		}
		if _, err := links.Link(ctx, profile.ID, tr); !errors.Is(err, ErrRefused) {
			t.Errorf("%s linked through an app it does not know: %v", tr, err)
		}
		if err := st.SetTrackerClient(ctx, tr, tc.fake.clientID); err != nil {
			t.Fatal(err)
		}
		if s := state(); s.State != StateUnlinked {
			t.Errorf("%s set up: %s, want unlinked", tr, s.State)
		}

		code, err := links.Link(ctx, profile.ID, tr)
		if err != nil || code.UserCode != tc.code || code.VerificationURIComplete != tc.enter || code.VerificationURI == "" || code.DeviceCode != "" {
			t.Fatalf("%s's code: %+v, %v; want %s to enter at %s, and no device code", tr, code, err, tc.code, tc.enter)
		}
		follow()
		if s := state(); s.State != StateLinking || s.Link.UserCode != tc.code || s.Link.DeviceCode != "" || toldOf(tr) {
			t.Errorf("%s before the code is entered: %+v, want linking by %s, and nothing told", tr, s, tc.code)
		}
		tc.fake.set(func() { tc.fake.entered = true })
		follow()
		if s := state(); s.State != StateLinked || s.Account.Username != "oliver" || !toldOf(tr) {
			t.Errorf("%s once the code is entered: %+v, want linked as oliver, and the profile told", tr, s)
		}
		if _, err := links.Link(ctx, profile.ID, tr); !errors.Is(err, ErrRefused) {
			t.Errorf("%s linked twice: %v", tr, err)
		}

		if err := links.Unlink(ctx, profile.ID, tr); err != nil {
			t.Fatal(err)
		}
		if s := state(); s.State != StateUnlinked || len(tc.fake.revoked) != 1 || tc.fake.revoked[0] != tc.revokedToken {
			t.Errorf("%s unlinked: %s, revoked %v; want unlinked and its %s token revoked", tr, s.State, tc.fake.revoked, tc.revokedToken)
		}
		if err := links.Unlink(ctx, profile.ID, tr); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s unlinked twice: %v, want ErrNotFound", tr, err)
		}

		// A code that expires unentered ends the link, as one given up on does.
		tc.fake.set(func() { tc.fake.entered, tc.fake.expired = false, true })
		if _, err := links.Link(ctx, profile.ID, tr); err != nil {
			t.Fatal(err)
		}
		follow()
		if s := state(); s.State != StateUnlinked || !toldOf(tr) {
			t.Errorf("%s once its code expired: %s, want unlinked, and the profile told", tr, s.State)
		}
		if _, err := links.Link(ctx, profile.ID, tr); err != nil {
			t.Fatal(err)
		}
		if err := links.Unlink(ctx, profile.ID, tr); err != nil {
			t.Errorf("%s given up on: %v", tr, err)
		}
	}
}

// at is the fake's tracker as the server asks it, at base.
func (f *fakeTracker) at(base string) service {
	if f.tracker == domain.TrackerSimkl {
		return simkl{api: provider.Client{Name: "simkl", Base: base}, version: "1.2.3"}
	}
	return trakt{api: provider.Client{Name: "trakt", Base: base}, auth: provider.Client{Name: "trakt", Base: base}, version: "1.2.3"}
}
