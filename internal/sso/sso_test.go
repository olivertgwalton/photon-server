//go:build integration

package sso

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/coreos/go-oidc/v3/oidc/oidctest"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// fakeProvider is an OpenID Connect provider, as authentik answers: it signs in whoever the test
// says is at it, hands the code it issues out once for the verifier it was challenged with, signs ID
// tokens for the client photon, and grants a refresh token, rotated as it is used, to a sign-in
// that asked for offline access. A refresh answers the account as it is now.
type fakeProvider struct {
	*httptest.Server
	t   *testing.T
	key *rsa.PrivateKey

	mu sync.Mutex
	// subject is who signs in next, and accounts each account's claims now, beside its subject.
	subject  string
	accounts map[string]map[string]any
	// disabled accounts are refused a refresh; down answers every request 503, secret is the client
	// secret it takes, and noRefresh refuses the client the refresh grant, as Authelia does a client
	// registered without it. proxied answers a refresh 400 from a proxy before it, with no OAuth
	// error, and forged signs refreshed ID tokens with a key it never published.
	disabled  map[string]bool
	down      bool
	secret    string
	noRefresh bool
	proxied   bool
	forged    bool
	// nonce, when set, is the nonce its ID tokens carry in place of the one asked for.
	nonce   string
	codes   map[string]authorization
	refresh map[string]string
}

type authorization struct {
	challenge, nonce, redirect, subject string
	offline                             bool
}

func newProvider(t *testing.T) *fakeProvider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProvider{
		t: t, key: key, secret: "secret", accounts: map[string]map[string]any{}, disabled: map[string]bool{},
		codes: map[string]authorization{}, refresh: map[string]string{},
	}
	keys := &oidctest.Server{PublicKeys: []oidctest.PublicKey{{PublicKey: key.Public(), KeyID: "key", Algorithm: "RS256"}}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		down := f.down
		f.mu.Unlock()
		switch {
		case down:
			http.Error(w, "down", http.StatusServiceUnavailable)
		case r.URL.Path == "/.well-known/openid-configuration":
			f.discovery(w)
		case r.URL.Path == "/token":
			f.token(w, r)
		default:
			keys.ServeHTTP(w, r)
		}
	}))
	keys.SetIssuer(f.URL)
	t.Cleanup(f.Close)
	f.as("sub-ada", map[string]any{"preferred_username": "ada", "email": "ada@example.com", "groups": []string{"photon"}})
	return f
}

func (f *fakeProvider) discovery(w http.ResponseWriter) {
	f.reply(w, http.StatusOK, map[string]any{
		"issuer": f.URL, "authorization_endpoint": f.URL + "/auth", "token_endpoint": f.URL + "/token",
		"jwks_uri": f.URL + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups", "offline_access"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
	})
}

// as signs in subject next, its account's claims now claims.
func (f *fakeProvider) as(subject string, claims map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subject = subject
	f.accounts[subject] = maps.Clone(claims)
}

func (f *fakeProvider) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

// authorize is the person signing in at the provider, which answers the code it sends the browser
// back with.
func (f *fakeProvider) authorize(to string) string {
	f.t.Helper()
	u, err := url.Parse(to)
	if err != nil {
		f.t.Fatal(err)
	}
	q := u.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "photon" {
		f.t.Fatalf("asked for %v; want a code for photon, challenged by S256", q)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	code := rand.Text()
	f.codes[code] = authorization{
		challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"), subject: f.subject,
		offline: slices.Contains(strings.Fields(q.Get("scope")), "offline_access"),
	}
	return code
}

func (f *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, secret, _ := r.BasicAuth(); id != "photon" || secret != f.secret {
		f.reply(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
		return
	}
	var subject, nonce string
	var offline bool
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		a, ok := f.codes[r.PostForm.Get("code")]
		delete(f.codes, r.PostForm.Get("code"))
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != a.challenge || r.PostForm.Get("redirect_uri") != a.redirect {
			f.reply(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		subject, nonce, offline = a.subject, cmpOr(f.nonce, a.nonce), a.offline
	case "refresh_token":
		if f.proxied {
			http.Error(w, "<html>Bad Request</html>", http.StatusBadRequest)
			return
		}
		if f.noRefresh {
			f.reply(w, http.StatusBadRequest, map[string]any{"error": "unauthorized_client"})
			return
		}
		var ok bool
		subject, ok = f.refresh[r.PostForm.Get("refresh_token")]
		delete(f.refresh, r.PostForm.Get("refresh_token"))
		if !ok || f.disabled[subject] {
			f.reply(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		offline = true
	}
	claims := maps.Clone(f.accounts[subject])
	maps.Copy(claims, map[string]any{
		"iss": f.URL, "sub": subject, "aud": "photon", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	})
	if nonce != "" {
		claims["nonce"] = nonce
	}
	b, err := json.Marshal(claims)
	if err != nil {
		f.t.Error(err)
	}
	key := f.key
	if f.forged && nonce == "" {
		if key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			f.t.Error(err)
		}
	}
	answer := map[string]any{
		"access_token": "access", "token_type": "Bearer", "expires_in": 3600,
		"id_token": oidctest.SignIDToken(key, "key", "RS256", string(b)),
	}
	if offline {
		refresh := rand.Text()
		f.refresh[refresh] = subject
		answer["refresh_token"] = refresh
	}
	f.reply(w, http.StatusOK, answer)
}

func (f *fakeProvider) reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		f.t.Error(err)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var public = &url.URL{Scheme: "https", Host: "photon.example.com"}

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dsn := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), dsn, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), dsn, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	return New(st, k, func() *url.URL { return public }, log), st
}

func register(t *testing.T, svc *Service, f *fakeProvider, provisioning domain.Provisioning, group string) {
	t.Helper()
	err := svc.SetProvider(t.Context(), domain.SignInProvider{
		Slug: "pocket-id", Name: "Pocket ID", Issuer: f.URL, ClientID: "photon", ClientSecret: "secret",
		Provisioning: provisioning, Group: group, Recheck: domain.RecheckHourly,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// signIn is a browser leaving for the provider and coming back with answer, which may change
// what the provider sent; it answers the profile signed in as.
func signIn(t *testing.T, svc *Service, f *fakeProvider, linking uuid.UUID, answer func(q url.Values)) (domain.Profile, error) {
	t.Helper()
	state, to, err := svc.Start(t.Context(), "pocket-id", kv.SignInFlow{Linking: linking, Device: "Firefox", Client: "Photon Web", To: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://photon.example.com/api/v1/auth/sign-in-providers/pocket-id/callback"; !strings.Contains(to, url.QueryEscape(want)) {
		t.Fatalf("sent to %s; want back to %s", to, want)
	}
	q := url.Values{"code": {f.authorize(to)}, "state": {state}}
	if answer != nil {
		answer(q)
	}
	out, err := svc.Finish(t.Context(), "pocket-id", q.Get("state"), state, linking, q)
	if err == nil && (out.Flow.Device != "Firefox" || out.Flow.To != "/") {
		t.Errorf("finished %+v; want the flow started", out.Flow)
	}
	if err == nil && out.Identity.Provider != "pocket-id" {
		t.Errorf("signed in as %+v; want an account at pocket-id", out.Identity)
	}
	return out.Profile, err
}

func refusal(err error) Refusal {
	if r, ok := errors.AsType[*Refused](err); ok {
		return r.Reason
	}
	return ""
}

// An account no profile linked signs in as no one, until a profile links it; then it signs in as
// that profile, and no other profile can link it too.
func TestALinkedAccountSignsInAsItsProfile(t *testing.T) {
	svc, st := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionLink, "")
	ada, err := st.AddProfile(t.Context(), "Ada", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.AddProfile(t.Context(), "Bob", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := signIn(t, svc, f, uuid.UUID{}, nil); refusal(err) != RefusalNotLinked {
		t.Fatalf("an unlinked account signed in: %v", err)
	}
	if _, err := signIn(t, svc, f, ada.ID, nil); err != nil {
		t.Fatalf("linking: %v", err)
	}
	if got, err := signIn(t, svc, f, uuid.UUID{}, nil); err != nil || got.ID != ada.ID {
		t.Errorf("signed in as %q, %v; want Ada", got.Name, err)
	}
	if _, err := signIn(t, svc, f, bob.ID, nil); refusal(err) != RefusalLinkedElsewhere {
		t.Errorf("Bob linked Ada's account: %v", err)
	}
	accounts, err := svc.Accounts(t.Context(), ada.ID)
	if err != nil || len(accounts) != 1 || accounts[0].Username != "ada" || accounts[0].Provider != "pocket-id" {
		t.Errorf("Ada's accounts are %+v, %v; want ada at pocket-id", accounts, err)
	}
}

// A provider that makes profiles makes one for an account the first time it signs in, named as the
// account is, and signs it in as that one after.
func TestAProviderThatMakesProfilesMakesOneForANewAccount(t *testing.T) {
	svc, _ := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionCreate, "photon")
	made, err := signIn(t, svc, f, uuid.UUID{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if made.Name != "ada" || made.Role != domain.RoleUser {
		t.Errorf("made %q, a %s; want ada, a user", made.Name, made.Role)
	}
	if again, err := signIn(t, svc, f, uuid.UUID{}, nil); err != nil || again.ID != made.ID {
		t.Errorf("signed in again as %q, %v; want the profile made", again.Name, err)
	}
}

// Where a provider requires a group, only an account in it signs in, its groups read as a list or
// as the one string some providers send.
func TestAProvidersGroupKeepsOthersOut(t *testing.T) {
	svc, _ := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionCreate, "photon")
	f.as("sub-eve", map[string]any{"preferred_username": "eve"})
	if _, err := signIn(t, svc, f, uuid.UUID{}, nil); refusal(err) != RefusalNotInGroup {
		t.Errorf("an account in no group: %v; want %s", err, RefusalNotInGroup)
	}
	f.as("sub-ada", map[string]any{"preferred_username": "ada", "groups": []string{"family", "photon"}})
	if _, err := signIn(t, svc, f, uuid.UUID{}, nil); err != nil {
		t.Errorf("an account in the group: %v", err)
	}
	f.as("sub-bob", map[string]any{"preferred_username": "bob", "groups": "photon"})
	if _, err := signIn(t, svc, f, uuid.UUID{}, nil); err != nil {
		t.Errorf("an account whose one group is a string: %v", err)
	}
}

// A provider's answer signs in no one unless it is to the browser that left, once, from the
// provider it was sent to, carrying the nonce it was sent with.
func TestAnAnswerIsCheckedBeforeItSignsAnyoneIn(t *testing.T) {
	svc, _ := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionCreate, "photon")
	for _, c := range []struct {
		name   string
		answer func(q url.Values)
		want   Refusal
	}{
		{"from another browser", func(q url.Values) { q.Set("state", rand.Text()) }, RefusalExpired},
		{"refused at the provider", func(q url.Values) { q.Set("error", "access_denied") }, RefusalDenied},
		{"from another provider", func(q url.Values) { q.Set("iss", "https://evil.example.com") }, RefusalFailed},
		{"with a code it did not issue", func(q url.Values) { q.Set("code", "forged") }, RefusalFailed},
	} {
		if _, err := signIn(t, svc, f, uuid.UUID{}, c.answer); refusal(err) != c.want {
			t.Errorf("an answer %s: %v; want %s", c.name, err, c.want)
		}
	}

	f.nonce = "another sign-in's"
	if _, err := signIn(t, svc, f, uuid.UUID{}, nil); refusal(err) != RefusalFailed {
		t.Errorf("an ID token with another nonce: %v; want %s", err, RefusalFailed)
	}
	f.nonce = ""

	state, to, err := svc.Start(t.Context(), "pocket-id", kv.SignInFlow{To: "/"})
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"code": {f.authorize(to)}, "state": {state}}
	if _, err := svc.Finish(t.Context(), "pocket-id", state, "a stranger's", uuid.UUID{}, q); refusal(err) != RefusalExpired {
		t.Errorf("a stranger's browser finished it: %v", err)
	}
	if _, err := svc.Finish(t.Context(), "pocket-id", state, state, uuid.UUID{}, q); err != nil {
		t.Errorf("the stranger spent the browser's sign-in: %v", err)
	}
	if _, err := svc.Finish(t.Context(), "pocket-id", state, state, uuid.UUID{}, q); refusal(err) != RefusalExpired {
		t.Errorf("an answer taken twice: %v", err)
	}
}

// A profile links an account only from the browser still signed in as it.
func TestLinkingFinishesOnlyAsTheProfileThatStarted(t *testing.T) {
	svc, st := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionLink, "")
	ada, err := st.AddProfile(t.Context(), "Ada", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, to, err := svc.Start(t.Context(), "pocket-id", kv.SignInFlow{Linking: ada.ID, To: "/settings"})
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"code": {f.authorize(to)}, "state": {state}}
	out, err := svc.Finish(t.Context(), "pocket-id", state, state, uuid.UUID{}, q)
	if refusal(err) != RefusalExpired || out.Flow.To != "/settings" {
		t.Errorf("linking from a browser signed out: %+v, %v; want refused, back to /settings", out.Flow, err)
	}
}

// An admin registers a provider only once the server has an address for it to send browsers back
// to, at an issuer reached over HTTPS that answers as a provider calling itself so.
func TestAnAdminRegistersOnlyAProviderThatAnswers(t *testing.T) {
	svc, st := newService(t)
	f := newProvider(t)
	at := func(issuer string) domain.SignInProvider {
		return domain.SignInProvider{
			Slug: "id", Name: "ID", Issuer: issuer, ClientID: "photon", Provisioning: domain.ProvisionLink, Recheck: domain.RecheckHourly,
		}
	}
	for _, c := range []struct{ name, issuer string }{
		{"in the clear", "http://id.example.com"},
		{"with a trailing slash it does not call itself by", f.URL + "/"},
		{"that is not a provider", f.URL + "/nothing"},
	} {
		if err := svc.SetProvider(t.Context(), at(c.issuer)); !errors.Is(err, ErrRefused) {
			t.Errorf("an issuer %s: %v; want ErrRefused", c.name, err)
		}
	}
	everyone := at(f.URL)
	everyone.Provisioning = domain.ProvisionCreate
	if err := svc.SetProvider(t.Context(), everyone); !errors.Is(err, ErrRefused) {
		t.Errorf("a provider making profiles for anyone it signs in: %v; want ErrRefused", err)
	}
	unset := New(st, svc.kv, func() *url.URL { return nil }, svc.log)
	if err := unset.SetProvider(t.Context(), at(f.URL)); !errors.Is(err, ErrRefused) {
		t.Errorf("with no public address: %v; want ErrRefused", err)
	}
	if err := svc.SetProvider(t.Context(), at(f.URL)); err != nil {
		t.Errorf("a provider that answers: %v", err)
	}
}

// session signs a device in as subject's account at pocket-id, answering whether it is still
// signed in.
func session(t *testing.T, st *store.Store, profile uuid.UUID, subject string) func() bool {
	t.Helper()
	token := []byte(rand.Text())
	_, err := st.CreateSession(t.Context(), store.NewSession{
		Kind: domain.SessionDevice, ProfileID: profile, TokenHash: token, DeviceName: "TV", Client: "Photon",
		ExpiresAt: new(time.Now().Add(time.Hour)), Identity: domain.SignInIdentity{Provider: "pocket-id", Subject: subject},
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() bool {
		_, _, err := st.SessionByToken(t.Context(), token, time.Now())
		return err == nil
	}
}

// A provider that rechecks keeps an account's devices signed in while it lets the account refresh,
// through its outages and its refusals of the client, and signs them out once it disables the
// account or the account leaves its group.
func TestAProviderThatRechecksSignsOutWhomItNoLongerLetsIn(t *testing.T) {
	svc, st := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionCreate, "photon")
	recheck := func() {
		t.Helper()
		if err := svc.recheckDue(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
	}
	ada, err := signIn(t, svc, f, uuid.UUID{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	alive := session(t, st, ada.ID, "sub-ada")
	for _, c := range []struct {
		name  string
		while func()
	}{
		{"with the provider letting it in", func() {}},
		{"while the provider is down", func() { f.down = true }},
		{"while the provider refuses the client", func() { f.secret = "rotated" }},
		{"while the provider refuses the client the refresh grant", func() { f.noRefresh = true }},
		{"while a proxy before the provider answers 400", func() { f.proxied = true }},
		// The refresh is spent, so the next is asked by the token it rotated to.
		{"while the provider's refreshed ID tokens fail verification", func() { f.forged = true }},
		// The provider rotates its refresh tokens, each used once, so this is asked by the last.
		{"once the provider is back", func() {}},
	} {
		f.set(c.while)
		recheck()
		f.set(func() { f.down, f.secret, f.noRefresh, f.proxied, f.forged = false, "secret", false, false, false })
		if !alive() {
			t.Fatalf("signed out %s", c.name)
		}
	}

	f.as("sub-ada", map[string]any{"preferred_username": "ada", "groups": []string{"family"}})
	recheck()
	if alive() {
		t.Error("an account that left the group is still signed in")
	}

	f.as("sub-ada", map[string]any{"preferred_username": "ada", "groups": []string{"photon"}})
	if _, err := signIn(t, svc, f, uuid.UUID{}, nil); err != nil {
		t.Fatal(err)
	}
	alive = session(t, st, ada.ID, "sub-ada")
	f.set(func() { f.disabled["sub-ada"] = true })
	recheck()
	if alive() {
		t.Error("an account the provider disabled is still signed in")
	}
}

// A provider checked only at sign-in is asked for no offline access, and keeps an account's devices
// signed in whatever becomes of the account; one changed to it forgets the tokens it held.
func TestAProviderCheckedAtSignInSignsNoOneOutAfter(t *testing.T) {
	svc, st := newService(t)
	f := newProvider(t)
	register(t, svc, f, domain.ProvisionCreate, "photon")
	ada, err := signIn(t, svc, f, uuid.UUID{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	alive := session(t, st, ada.ID, "sub-ada")
	p, err := st.SignInProvider(t.Context(), "pocket-id")
	if err != nil {
		t.Fatal(err)
	}
	p.Recheck = domain.RecheckAtSignIn
	if err := svc.SetProvider(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	f.set(func() { f.disabled["sub-ada"] = true })
	if err := svc.recheckDue(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
	if !alive() {
		t.Error("an account was signed out at a provider checked only at sign-in")
	}

	state, to, err := svc.Start(t.Context(), "pocket-id", kv.SignInFlow{To: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(to, "offline_access") {
		t.Errorf("asked for offline access: %s", to)
	}
	f.set(func() { f.disabled["sub-ada"] = false })
	q := url.Values{"code": {f.authorize(to)}, "state": {state}}
	if _, err := svc.Finish(t.Context(), "pocket-id", state, state, uuid.UUID{}, q); err != nil {
		t.Fatal(err)
	}
	if due, err := st.SignInChecksDue(t.Context(), 0); err != nil || len(due) != 0 {
		t.Errorf("due a check: %v, %v; want none", due, err)
	}
}
