package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/sso"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeSignIns is one provider, pocket-id, whose answers sign Oliver in, or are refused as refusal
// says.
type fakeSignIns struct {
	provider domain.SignInProvider
	refusal  sso.Refusal
	// started is the flow the last browser left with, and finished the browser's bound state and
	// the profile it was signed in as.
	started  kv.SignInFlow
	bound    string
	signedIn uuid.UUID
}

func (f *fakeSignIns) Providers(context.Context) ([]domain.SignInProvider, error) {
	return []domain.SignInProvider{f.provider}, nil
}

func (f *fakeSignIns) Provider(_ context.Context, slug string) (domain.SignInProvider, error) {
	if slug != f.provider.Slug {
		return domain.SignInProvider{}, store.ErrNotFound
	}
	return f.provider, nil
}

func (f *fakeSignIns) SetProvider(_ context.Context, p domain.SignInProvider) error {
	f.provider = p
	return nil
}

func (f *fakeSignIns) RemoveProvider(context.Context, string) error { return nil }

func (f *fakeSignIns) Callback(slug string) *url.URL {
	return &url.URL{Scheme: "https", Host: "photon.example.com", Path: "/api/v1/auth/sign-in-providers/" + slug + "/callback"}
}

func (f *fakeSignIns) Start(_ context.Context, slug string, flow kv.SignInFlow) (string, string, error) {
	if slug != f.provider.Slug {
		return "", "", store.ErrNotFound
	}
	f.started = flow
	return "the-state", "https://id.example.com/authorize?state=the-state", nil
}

func (f *fakeSignIns) Finish(_ context.Context, _, _, bound string, signedIn uuid.UUID, _ url.Values) (sso.Outcome, error) {
	f.bound, f.signedIn = bound, signedIn
	if f.refusal != "" {
		return sso.Outcome{Flow: f.started}, &sso.Refused{Reason: f.refusal, Account: "ada"}
	}
	return sso.Outcome{Flow: f.started, Profile: oliver, Identity: domain.SignInIdentity{Provider: "pocket-id", Subject: "sub-oliver"}}, nil
}

func (f *fakeSignIns) Accounts(context.Context, uuid.UUID) ([]domain.SignInAccount, error) {
	return []domain.SignInAccount{{Provider: "pocket-id", Username: "oliver"}}, nil
}

func (f *fakeSignIns) Unlink(context.Context, uuid.UUID, string) error { return store.ErrLastWayIn }

func signInAPI(t *testing.T) (*API, *fakeSignIns, *fakeEvents) {
	a := newAPI(nil)
	f := &fakeSignIns{provider: domain.SignInProvider{
		Slug: "pocket-id", Name: "Pocket ID", Issuer: "https://id.example.com", ClientID: "photon", ClientSecret: "secret",
		Provisioning: domain.ProvisionLink,
	}}
	events := &fakeEvents{}
	a.svc.SignIns, a.svc.Events = f, events
	a.svc.Reach = reaching(t, domain.Network{PublicURL: "https://photon.example.com"})
	return a, f, events
}

func answer(a *API, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec
}

// A browser leaves for its provider bound to the sign-in by a cookie sent only back to the
// callback, and comes back with its query, whatever the provider puts in it, signed in in its
// session's cookie and sent on to where it asked.
func TestABrowserSignsInAtItsProvider(t *testing.T) {
	a, f, events := signInAPI(t)
	start := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in-providers/pocket-id/sign-in",
		strings.NewReader(`{"device":"Firefox","client":"Photon Web","to":"/titles/1"}`))
	start.Header.Set("Origin", "https://photon.example.com:443")
	rec := answer(a, start)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var left signInRedirectJSON
	if err := json.NewDecoder(rec.Body).Decode(&left); err != nil || !strings.HasPrefix(left.URL, "https://id.example.com/") {
		t.Fatalf("sent to %q, %v", left.URL, err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	bound := cookies[0]
	if bound.Value != "the-state" || !bound.HttpOnly || bound.SameSite != http.SameSiteLaxMode ||
		bound.Path != "/api/v1/auth/sign-in-providers/pocket-id/callback" {
		t.Errorf("binding cookie %+v", bound)
	}
	if f.started.Device != "Firefox" || f.started.To != "/titles/1" {
		t.Errorf("left with %+v", f.started)
	}

	back := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sign-in-providers/pocket-id/callback?code=c&state=the-state&iss=x&session_state=y&scope=openid", nil)
	back.AddCookie(bound)
	rec = answer(a, back)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/titles/1" {
		t.Fatalf("came back to %d %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if f.bound != "the-state" {
		t.Errorf("finished bound to %q", f.bound)
	}
	var session, cleared bool
	for _, c := range rec.Result().Cookies() {
		session = session || c.Name == sessionCookie && c.Value == goodToken && c.HttpOnly
		cleared = cleared || c.Name == flowCookie && c.MaxAge < 0
	}
	if !session || !cleared {
		t.Errorf("cookies %v; want the session kept and the binding cleared", rec.Result().Cookies())
	}
	if len(events.raised) != 1 || events.raised[0].Kind != domain.EventSignedIn || events.raised[0].Profile != oliver.ID {
		t.Errorf("raised %+v; want Oliver signed in", events.raised)
	}
}

// A refused sign-in goes to the login page saying why, still meaning to go on where it asked, and
// is raised; a refused link goes back to where it started.
func TestARefusedSignInSaysWhy(t *testing.T) {
	a, f, events := signInAPI(t)
	f.refusal = sso.RefusalNotLinked
	f.started = kv.SignInFlow{Device: "Firefox", Client: "Photon Web", To: "/titles/1"}
	rec := answer(a, httptest.NewRequest(http.MethodGet, "/api/v1/auth/sign-in-providers/pocket-id/callback?state=s", nil))
	if want := "/auth/login?refused=not_linked&to=%2Ftitles%2F1"; rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Errorf("sent to %d %q; want %q", rec.Code, rec.Header().Get("Location"), want)
	}
	if len(events.raised) != 1 || events.raised[0].Kind != domain.EventSignInRefused {
		t.Errorf("raised %+v; want a refused sign-in", events.raised)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			t.Errorf("a refused sign-in kept a session: %v", c)
		}
	}

	f.refusal = sso.RefusalLinkedElsewhere
	f.started = kv.SignInFlow{Linking: oliver.ID, To: "/settings/sign-in?tab=1"}
	rec = answer(a, httptest.NewRequest(http.MethodGet, "/api/v1/auth/sign-in-providers/pocket-id/callback?state=s", nil))
	if want := "/settings/sign-in?refused=linked_elsewhere&tab=1"; rec.Header().Get("Location") != want {
		t.Errorf("a refused link sent to %q; want %q", rec.Header().Get("Location"), want)
	}
}

// A browser is sent on only to a path on this server, and only leaves from the server's public
// address, which the provider sends it back to.
func TestASignInLeavesOnlyFromThePublicAddressForThisServer(t *testing.T) {
	a, _, _ := signInAPI(t)
	for _, c := range []struct{ name, origin, to string }{
		{"to another site", "https://photon.example.com", "https://evil.example.com/"},
		{"to another site by a scheme-relative path", "https://photon.example.com", "//evil.example.com/"},
		{"to another site by a backslash", "https://photon.example.com", `/\evil.example.com`},
		{"from another address", "http://192.168.1.10:8096", "/"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in-providers/pocket-id/sign-in",
			strings.NewReader(`{"device":"d","client":"c","to":`+strconv.Quote(c.to)+`}`))
		req.Header.Set("Origin", c.origin)
		if rec := answer(a, req); rec.Code < 400 || len(rec.Result().Cookies()) != 0 {
			t.Errorf("a sign-in %s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

// An admin's change that leaves the client secret out keeps the one kept for the same client, and
// the secret is never answered.
func TestAnAdminKeepsAProvidersSecretUnlessTheClientChanges(t *testing.T) {
	a, f, _ := signInAPI(t)
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/sign-in-providers/pocket-id", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		return answer(a, req)
	}
	rec := put(`{"name":"Pocket ID","issuer":"https://id.example.com","client_id":"photon","provisioning":"create"}`)
	if rec.Code != http.StatusOK || f.provider.ClientSecret != "secret" || f.provider.Provisioning != domain.ProvisionCreate {
		t.Fatalf("%d %s; kept %+v", rec.Code, rec.Body, f.provider)
	}
	if strings.Contains(rec.Body.String(), "secret\"") || !strings.Contains(rec.Body.String(), `"client_secret_set":true`) {
		t.Errorf("answered %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"callback_url":"https://photon.example.com/api/v1/auth/sign-in-providers/pocket-id/callback"`) {
		t.Errorf("no redirect URI to register: %s", rec.Body)
	}
	if rec := put(`{"name":"Pocket ID","issuer":"https://id.example.com","client_id":"another","provisioning":"link"}`); rec.Code != http.StatusOK || f.provider.ClientSecret != "" {
		t.Errorf("a new client kept the old one's secret: %+v", f.provider)
	}
	if rec := put(`{"name":"Pocket ID","issuer":"https://id.example.com","client_id":"photon","provisioning":"sometimes"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("provisioning none of its values: %d", rec.Code)
	}
	rec = put(`{"name":"Pocket ID","issuer":"https://id.example.com","client_id":"photon","provisioning":"create","group":"kids",` +
		`"access":{"max_age":12,"unrated":"block","libraries":[]}}`)
	if rec.Code != http.StatusOK || f.provider.MaxAge == nil || *f.provider.MaxAge != 12 || f.provider.Unrated != domain.UnratedBlock ||
		!strings.Contains(rec.Body.String(), `"access":{"max_age":12,"unrated":"block","libraries":[]}`) {
		t.Errorf("the access a made profile gets: %d %s; kept %+v", rec.Code, rec.Body, f.provider)
	}
	if rec := put(`{"name":"Pocket ID","issuer":"https://id.example.com","client_id":"photon","provisioning":"create","group":"kids",` +
		`"access":{"max_age":40,"unrated":"allow","libraries":[]}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an age past 21: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/sign-in-providers/Not%20A%20Slug", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	if rec := answer(a, req); rec.Code != http.StatusBadRequest {
		t.Errorf("a slug of capitals and spaces: %d", rec.Code)
	}
}

// The one account a profile with no password signs in with is not unlinked.
func TestTheLastWayInIsNotUnlinked(t *testing.T) {
	a, _, _ := signInAPI(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/profile/sign-in-providers/pocket-id", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	if rec := answer(a, req); rec.Code != http.StatusConflict {
		t.Errorf("%d %s; want 409", rec.Code, rec.Body)
	}
}
