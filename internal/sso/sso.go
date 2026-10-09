// Package sso signs the household in through OpenID Connect providers an admin registered the server
// on: Authelia, Authentik, Keycloak, Pocket ID and the like. The server is a confidential client
// using the authorization code flow with PKCE (RFC 7636), a state bound to the browser and a nonce,
// as RFC 9700 asks. An account is known by its provider and subject, never by its email, and signs
// in as the profile that linked it, or one made for it.
package sso

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// FlowTTL is how long a browser has at its provider before its sign-in is forgotten.
	FlowTTL = 10 * time.Minute
	// discoveryTTL is how long a provider's metadata is kept before it is read again, so a
	// provider that moves its endpoints is followed.
	discoveryTTL = time.Hour
	// askWithin bounds each request to a provider.
	askWithin = 15 * time.Second
	// CallbackPattern is where a provider sends a browser back to, as the API routes it.
	CallbackPattern = "/api/v1/auth/sign-in-providers/{slug}/callback"
	// defaultName is a profile made for an account that gives no name of its own.
	defaultName = "User"
)

var (
	// ErrRefused is a provider an admin cannot register as asked, or a sign-in that cannot start.
	ErrRefused = errors.New("sign-in provider refused")
	// ErrUnreachable is a provider that did not answer as one.
	ErrUnreachable = errors.New("the sign-in provider did not answer")
)

// Refusal is why a provider's answer signed no one in.
type Refusal string

const (
	// RefusalExpired is an answer to no sign-in this browser started, or to one forgotten.
	RefusalExpired Refusal = "expired"
	// RefusalDenied is the provider's own refusal, or the person turning it down there.
	RefusalDenied Refusal = "denied"
	// RefusalFailed is an answer the server could not check, or that failed its checks.
	RefusalFailed Refusal = "failed"
	// RefusalNotLinked is an account no profile linked, at a provider that makes none.
	RefusalNotLinked Refusal = "not_linked"
	// RefusalNotInGroup is an account outside the group the provider requires.
	RefusalNotInGroup Refusal = "not_in_group"
	// RefusalLinkedElsewhere is linking an account another profile linked.
	RefusalLinkedElsewhere Refusal = "linked_elsewhere"
)

func Refusals() []Refusal {
	return []Refusal{RefusalExpired, RefusalDenied, RefusalFailed, RefusalNotLinked, RefusalNotInGroup, RefusalLinkedElsewhere}
}

// Refused is a sign-in refused for Reason, by the account the provider called Account when it got
// that far.
type Refused struct {
	Reason  Refusal
	Account string
	Err     error
}

func (r *Refused) Error() string { return fmt.Sprintf("sign-in %s: %v", r.Reason, r.Err) }
func (r *Refused) Unwrap() error { return r.Err }

func refuse(reason Refusal, account string, err error) *Refused {
	return &Refused{Reason: reason, Account: account, Err: err}
}

type Service struct {
	st     *store.Store
	kv     *kv.KV
	public func() *url.URL
	client *http.Client
	log    *slog.Logger

	mu    sync.Mutex
	known map[string]discovered
}

// New answers providers' callbacks at public, the server's address outside.
func New(st *store.Store, k *kv.KV, public func() *url.URL, log *slog.Logger) *Service {
	return &Service{
		st: st, kv: k, public: public, log: log,
		client: &http.Client{Timeout: askWithin},
		known:  map[string]discovered{},
	}
}

// Callback is the address a provider sends a browser back to, which an admin registers there: nil
// until the server has an address outside.
func (s *Service) Callback(slug string) *url.URL {
	base := s.public()
	if base == nil {
		return nil
	}
	return base.JoinPath(strings.Replace(CallbackPattern, "{slug}", slug, 1))
}

// discovered is a provider's metadata, as read at.
type discovered struct {
	provider *oidc.Provider
	scopes   []string
	// issParameter is a provider that names itself in its every answer (RFC 9207).
	issParameter bool
	// authStyle is how the client proves itself at the token endpoint.
	authStyle oauth2.AuthStyle
	at        time.Time
}

func (s *Service) ask(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, s.client)
}

// discover reads an issuer's metadata, kept a while once read; one that answers as no provider, or
// as another, is not kept.
func (s *Service) discover(ctx context.Context, issuer string) (discovered, error) {
	s.mu.Lock()
	d, ok := s.known[issuer]
	s.mu.Unlock()
	if ok && time.Since(d.at) < discoveryTTL {
		return d, nil
	}
	p, err := oidc.NewProvider(s.ask(ctx), issuer)
	if err != nil {
		return discovered{}, err
	}
	var meta struct {
		Scopes       []string `json:"scopes_supported"`
		IssParameter bool     `json:"authorization_response_iss_parameter_supported"`
		AuthMethods  []string `json:"token_endpoint_auth_methods_supported"`
	}
	if err := p.Claims(&meta); err != nil {
		return discovered{}, err
	}
	d = discovered{provider: p, scopes: meta.Scopes, issParameter: meta.IssParameter, at: time.Now()}
	// client_secret_basic where the provider lists it, or lists none, as OIDC Discovery §3 has it,
	// so x/oauth2 never guesses: its guess retries a refused grant the other way, and a provider
	// that takes only the one answers the retry invalid_client, hiding why it refused.
	d.authStyle = oauth2.AuthStyleInHeader
	if len(meta.AuthMethods) > 0 && !slices.Contains(meta.AuthMethods, "client_secret_basic") &&
		slices.Contains(meta.AuthMethods, "client_secret_post") {
		d.authStyle = oauth2.AuthStyleInParams
	}
	s.mu.Lock()
	s.known[issuer] = d
	s.mu.Unlock()
	return d, nil
}

func (s *Service) Providers(ctx context.Context) ([]domain.SignInProvider, error) {
	return s.st.SignInProviders(ctx)
}

// SetProvider registers a provider, or changes one, once its issuer answers as an OpenID Connect
// provider calling itself by that issuer.
func (s *Service) SetProvider(ctx context.Context, p domain.SignInProvider) error {
	if s.public() == nil {
		return fmt.Errorf("%w: set the server's public address under Network first: a provider sends browsers back to it", ErrRefused)
	}
	if p.Provisioning == domain.ProvisionCreate && p.Group == "" {
		return fmt.Errorf("%w: a provider that makes profiles needs a group to make them for, or anyone it signs in gets one", ErrRefused)
	}
	if err := secureIssuer(p.Issuer); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.known, p.Issuer)
	s.mu.Unlock()
	_, err := s.discover(ctx, p.Issuer)
	if mismatch, ok := errors.AsType[*oidc.IssuerMismatchError](err); ok {
		return fmt.Errorf("%w: the provider calls itself %s: use that as the issuer", ErrRefused, mismatch.Discovered)
	}
	if err != nil {
		return fmt.Errorf("%w: %s did not answer as an OpenID Connect provider: %w", ErrRefused, p.Issuer, err)
	}
	return s.st.SetSignInProvider(ctx, p)
}

// secureIssuer refuses an issuer the server would send a client secret and tokens to in the clear:
// anywhere but HTTPS, or this machine.
func secureIssuer(issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: the issuer is the provider's https address, as its discovery document names it", ErrRefused)
	}
	if u.Scheme == "https" {
		return nil
	}
	if ip := net.ParseIP(u.Hostname()); u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("%w: the issuer is reached over https, so its tokens and the client secret are not sent in the clear", ErrRefused)
}

// Provider is the provider of a slug. ErrNotFound for none.
func (s *Service) Provider(ctx context.Context, slug string) (domain.SignInProvider, error) {
	return s.st.SignInProvider(ctx, slug)
}

func (s *Service) RemoveProvider(ctx context.Context, slug string) error {
	return s.st.RemoveSignInProvider(ctx, slug)
}

// Accounts answers the accounts a profile linked.
func (s *Service) Accounts(ctx context.Context, profile uuid.UUID) ([]domain.SignInAccount, error) {
	return s.st.SignInAccounts(ctx, profile)
}

func (s *Service) Unlink(ctx context.Context, profile uuid.UUID, slug string) error {
	return s.st.UnlinkSignIn(ctx, profile, slug)
}

// config is how the server asks a provider, as the client an admin registered there, sending a
// browser back to redirect.
func config(p domain.SignInProvider, d discovered, redirect string) *oauth2.Config {
	scopes := []string{oidc.ScopeOpenID}
	for _, scope := range []string{oidc.ScopeProfile, oidc.ScopeEmail, scopeGroups, oidc.ScopeOfflineAccess} {
		// A provider that lists none is asked for the standard scopes; groups are asked for only
		// where one is required, and offline access, for a refresh token that outlasts the
		// provider's own session, only where its accounts are rechecked, each where offered. No
		// prompt=consent goes with it (OIDC Core §11): an admin registered the client, which is
		// the consent a self-hosted provider is configured to take for it.
		wanted, standard := true, true
		switch scope {
		case scopeGroups:
			wanted, standard = p.Group != "", false
		case oidc.ScopeOfflineAccess:
			wanted, standard = p.Recheck == domain.RecheckHourly, false
		}
		if wanted && (slices.Contains(d.scopes, scope) || len(d.scopes) == 0 && standard) {
			scopes = append(scopes, scope)
		}
	}
	endpoint := d.provider.Endpoint()
	endpoint.AuthStyle = d.authStyle
	if p.ClientSecret == "" {
		// A public client names itself in the request, with no secret to prove it by.
		endpoint.AuthStyle = oauth2.AuthStyleInParams
	}
	return &oauth2.Config{
		ClientID: p.ClientID, ClientSecret: p.ClientSecret, Endpoint: endpoint, RedirectURL: redirect, Scopes: scopes,
	}
}

// admitted is whether an account's claims let it sign in at the provider: in its group, where it
// requires one.
func admitted(p domain.SignInProvider, c claims) bool {
	return p.Group == "" || slices.Contains(c.Groups, p.Group)
}

// scopeGroups is the scope Authelia, Kanidm and Pocket ID put an account's groups under.
const scopeGroups = "groups"

func hashState(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

// Start sends a browser to a provider for f, answering the state the browser is bound to it by and
// the address to send it to.
func (s *Service) Start(ctx context.Context, slug string, f kv.SignInFlow) (state, to string, err error) {
	p, err := s.st.SignInProvider(ctx, slug)
	if err != nil {
		return "", "", err
	}
	callback := s.Callback(slug)
	if callback == nil {
		return "", "", fmt.Errorf("%w: the server has no public address for %s to send you back to: an admin sets one under Network", ErrRefused, p.Name)
	}
	d, err := s.discover(ctx, p.Issuer)
	if err != nil {
		s.log.WarnContext(ctx, "sign-in provider not reached", slog.String("provider", slug), slog.Any("err", err))
		return "", "", fmt.Errorf("%w: %s", ErrUnreachable, p.Name)
	}
	// rand.Text is 128 random bits: no two states meet.
	state = rand.Text()
	f.Provider, f.Issuer, f.Verifier, f.Nonce = slug, p.Issuer, oauth2.GenerateVerifier(), rand.Text()
	started, err := s.kv.StartSignIn(ctx, hashState(state), f, FlowTTL)
	if err == nil && !started {
		err = errors.New("a sign-in's state was in use")
	}
	if err != nil {
		return "", "", err
	}
	return state, config(p, d, callback.String()).AuthCodeURL(state, oauth2.S256ChallengeOption(f.Verifier), oidc.Nonce(f.Nonce)), nil
}

// claims are what the server reads of an account, from its ID token and its provider's userinfo.
type claims struct {
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	Groups            groups `json:"groups"`
}

// groups are an account's groups, which a provider with one sends as a string.
type groups []string

func (g *groups) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*g = groups{one}
		return nil
	}
	return json.Unmarshal(b, (*[]string)(g))
}

// account is what the provider calls the account, for a profile to show it by.
func (c claims) account(subject string) string {
	return cmp.Or(c.PreferredUsername, c.Email, c.Name, subject)
}

// profileName is the first name the account gives that a profile may have.
func (c claims) profileName() string {
	local, _, _ := strings.Cut(c.Email, "@")
	for _, n := range []string{c.PreferredUsername, c.Name, local} {
		if name, ok := domain.ProfileName(n); ok {
			return name
		}
	}
	return defaultName
}

// Outcome is a sign-in finished: the flow the browser left with, and the profile its account signed
// in as or was linked to.
type Outcome struct {
	Flow     kv.SignInFlow
	Profile  domain.Profile
	Identity domain.SignInIdentity
}

// Finish checks a provider's answer q to the browser bound to state by its cookie bound, and signs
// the account in as its profile, or links it to the profile signedIn as the browser started to.
// The flow is answered with any refusal it got far enough to find, for where to send the browser.
func (s *Service) Finish(ctx context.Context, slug, state, bound string, signedIn uuid.UUID, q url.Values) (Outcome, error) {
	// A browser that is not the one that left is refused before the flow is taken, so a stranger
	// cannot spend another's sign-in.
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(bound)) != 1 {
		return Outcome{}, refuse(RefusalExpired, "", errors.New("the state is not the browser's"))
	}
	f, ok, err := s.kv.TakeSignIn(ctx, hashState(state))
	if err != nil {
		return Outcome{}, err
	}
	if !ok || f.Provider != slug {
		return Outcome{}, refuse(RefusalExpired, "", errors.New("no sign-in has the state"))
	}
	out := Outcome{Flow: f}
	out.Profile, out.Identity.Subject, err = s.finish(ctx, f, signedIn, q)
	if err == nil {
		out.Identity.Provider = f.Provider
	}
	return out, err
}

// finish answers the profile and the account's subject.
func (s *Service) finish(ctx context.Context, f kv.SignInFlow, signedIn uuid.UUID, q url.Values) (domain.Profile, string, error) {
	if e := q.Get("error"); e != "" {
		return domain.Profile{}, "", refuse(RefusalDenied, "", fmt.Errorf("%s: %s", e, q.Get("error_description")))
	}
	p, err := s.st.SignInProvider(ctx, f.Provider)
	if errors.Is(err, store.ErrNotFound) || err == nil && p.Issuer != f.Issuer {
		return domain.Profile{}, "", refuse(RefusalExpired, "", errors.New("the provider was removed or moved"))
	}
	if err != nil {
		return domain.Profile{}, "", err
	}
	d, err := s.discover(ctx, p.Issuer)
	if err != nil {
		return domain.Profile{}, "", refuse(RefusalFailed, "", err)
	}
	// Mix-up: an answer from any provider but the one the browser was sent to is refused.
	if iss := q.Get("iss"); iss != "" && iss != p.Issuer || iss == "" && d.issParameter {
		return domain.Profile{}, "", refuse(RefusalFailed, "", fmt.Errorf("the answer is from %q, not %s", iss, p.Issuer))
	}
	callback := s.Callback(p.Slug)
	if callback == nil {
		return domain.Profile{}, "", refuse(RefusalFailed, "", errors.New("the server has no public address"))
	}
	tok, err := config(p, d, callback.String()).Exchange(s.ask(ctx), q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		return domain.Profile{}, "", refuse(RefusalFailed, "", fmt.Errorf("exchanging the code: %w", err))
	}
	raw, _ := tok.Extra("id_token").(string)
	id, err := d.provider.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(ctx, raw)
	if err != nil {
		return domain.Profile{}, "", refuse(RefusalFailed, "", fmt.Errorf("verifying the ID token: %w", err))
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(f.Nonce)) != 1 {
		return domain.Profile{}, "", refuse(RefusalFailed, "", errors.New("the ID token is for another sign-in"))
	}
	c, _, err := s.claims(ctx, d, id.Subject, id, tok)
	if err != nil {
		return domain.Profile{}, "", refuse(RefusalFailed, "", err)
	}
	account := c.account(id.Subject)
	if !admitted(p, c) {
		return domain.Profile{}, "", refuse(RefusalNotInGroup, account, fmt.Errorf("not in %q", p.Group))
	}
	var profile domain.Profile
	if f.Linking != (uuid.UUID{}) {
		profile, err = s.link(ctx, f, signedIn, id.Subject, account)
	} else {
		profile, err = s.signIn(ctx, p, id.Subject, c)
	}
	if err != nil {
		return domain.Profile{}, "", err
	}
	switch p.Recheck {
	case domain.RecheckHourly:
		err = s.st.KeepSignInToken(ctx, domain.SignInIdentity{Provider: p.Slug, Subject: id.Subject}, tok.RefreshToken)
	case domain.RecheckAtSignIn:
	}
	return profile, id.Subject, err
}

// claims reads the claims of subject's account from its ID token, where tok came with one, filled
// out from the provider's userinfo, which Authelia keeps the rest in. complete is whether every
// source the provider has was read: a userinfo that did not answer leaves the claims short.
func (s *Service) claims(ctx context.Context, d discovered, subject string, id *oidc.IDToken, tok *oauth2.Token) (c claims, complete bool, err error) {
	if id != nil {
		if err := id.Claims(&c); err != nil {
			return claims{}, false, fmt.Errorf("reading the ID token: %w", err)
		}
	}
	if d.provider.UserInfoEndpoint() == "" {
		return c, id != nil, nil
	}
	info, err := d.provider.UserInfo(s.ask(ctx), oauth2.StaticTokenSource(tok))
	if err != nil {
		s.log.WarnContext(ctx, "sign-in provider's userinfo not read", slog.Any("err", err))
		return c, false, nil
	}
	if info.Subject != subject {
		return claims{}, false, errors.New("the userinfo is another account's")
	}
	var more claims
	if err := info.Claims(&more); err != nil {
		return claims{}, false, fmt.Errorf("reading the userinfo: %w", err)
	}
	c.PreferredUsername = cmp.Or(c.PreferredUsername, more.PreferredUsername)
	c.Name, c.Email = cmp.Or(c.Name, more.Name), cmp.Or(c.Email, more.Email)
	c.Groups = append(c.Groups, more.Groups...)
	return c, true, nil
}

// link links the account to the profile that started linking it, if the browser is still signed in
// as that profile.
func (s *Service) link(ctx context.Context, f kv.SignInFlow, signedIn uuid.UUID, subject, account string) (domain.Profile, error) {
	if signedIn != f.Linking {
		return domain.Profile{}, refuse(RefusalExpired, account, errors.New("the browser is no longer signed in as the profile linking"))
	}
	err := s.st.LinkSignIn(ctx, f.Linking, f.Provider, subject, account)
	if errors.Is(err, store.ErrAccountLinked) {
		return domain.Profile{}, refuse(RefusalLinkedElsewhere, account, err)
	}
	if err != nil {
		return domain.Profile{}, err
	}
	return s.st.ProfileByID(ctx, f.Linking)
}

// signIn answers the profile that linked the account, or one made for it where the provider makes
// them.
func (s *Service) signIn(ctx context.Context, p domain.SignInProvider, subject string, c claims) (domain.Profile, error) {
	profile, err := s.st.ProfileBySignIn(ctx, p.Slug, subject)
	if !errors.Is(err, store.ErrNotFound) {
		return profile, err
	}
	account := c.account(subject)
	switch p.Provisioning {
	case domain.ProvisionLink:
		return domain.Profile{}, refuse(RefusalNotLinked, account, errors.New("no profile linked the account"))
	case domain.ProvisionCreate:
	}
	profile, err = s.st.AddSignInProfile(ctx, c.profileName(), p.Slug, subject, account)
	if errors.Is(err, store.ErrAccountLinked) {
		// The same account's other sign-in made it a profile meanwhile.
		return s.st.ProfileBySignIn(ctx, p.Slug, subject)
	}
	return profile, err
}
