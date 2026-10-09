package httpapi

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/sso"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type signIns interface {
	Providers(ctx context.Context) ([]domain.SignInProvider, error)
	Provider(ctx context.Context, slug string) (domain.SignInProvider, error)
	SetProvider(ctx context.Context, p domain.SignInProvider) error
	RemoveProvider(ctx context.Context, slug string) error
	Callback(slug string) *url.URL
	Start(ctx context.Context, slug string, f kv.SignInFlow) (state, to string, err error)
	Finish(ctx context.Context, slug, state, bound string, signedIn uuid.UUID, q url.Values) (sso.Outcome, error)
	Accounts(ctx context.Context, profile uuid.UUID) ([]domain.SignInAccount, error)
	Unlink(ctx context.Context, profile uuid.UUID, slug string) error
}

// flowCookie binds a browser to the sign-in it started, so a provider's answer is taken only from
// the browser that left for it. Lax, it is sent with the provider's redirect back.
const flowCookie = "photon_sign_in"

// loginPage is the web app's, where a refused sign-in is said why.
const loginPage = "/auth/login"

// signInProviderJSON is a provider the household signs in through, as the login page offers it.
type signInProviderJSON struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// adminSignInProviderJSON is a provider as an admin registered the server on it, saying whether a
// client secret is kept and never what. CallbackURL is the redirect URI to register there, absent
// until the server has a public address. Access is what a profile it makes may see.
type adminSignInProviderJSON struct {
	Slug            string              `json:"slug"`
	Name            string              `json:"name"`
	Issuer          string              `json:"issuer"`
	ClientID        string              `json:"client_id"`
	ClientSecretSet bool                `json:"client_secret_set"`
	Provisioning    domain.Provisioning `json:"provisioning"`
	Group           string              `json:"group,omitzero"`
	Recheck         domain.Recheck      `json:"recheck"`
	Access          accessJSON          `json:"access"`
	CallbackURL     string              `json:"callback_url,omitzero"`
}

// signInProviderChangeJSON registers the server on a provider. The client secret is kept as it is
// when left out for the same client id; a public client has none. Provisioning link signs in only
// accounts profiles linked, create gives each new account in Group a user profile of its own,
// seeing what Access says: every library, when left out. Group, required to create, is the group
// an account must be in at the provider, read from its groups claim as it signs in. Recheck hourly,
// when left out, asks the provider after each account each hour by the refresh token it granted,
// signing out the devices of one it refuses or that left the group; at_sign_in asks only as it
// signs in.
type signInProviderChangeJSON struct {
	Name         string              `json:"name"`
	Issuer       string              `json:"issuer"`
	ClientID     string              `json:"client_id"`
	ClientSecret string              `json:"client_secret,omitzero"`
	Provisioning domain.Provisioning `json:"provisioning"`
	Group        string              `json:"group,omitzero"`
	Recheck      domain.Recheck      `json:"recheck,omitzero"`
	Access       accessJSON          `json:"access,omitzero"`
}

// signInStartJSON is a browser leaving to sign in, naming its session as a password's sign-in does,
// and the path on this server it comes back to; the home page when left out.
type signInStartJSON struct {
	Device string `json:"device"`
	Client string `json:"client"`
	To     string `json:"to,omitzero"`
}

// linkStartJSON is a profile leaving to link its account, and the path it comes back to.
type linkStartJSON struct {
	To string `json:"to,omitzero"`
}

// signInRedirectJSON is where to send the browser: the provider's sign-in page. It comes back to
// the path it asked for, or, refused, to the login page with refused saying why (expired, denied,
// failed, not_linked, not_in_group or linked_elsewhere; a link refused comes back to its path).
type signInRedirectJSON struct {
	URL string `json:"url"`
}

// profileSignInProviderJSON is a provider and the account the profile linked there, if any.
type profileSignInProviderJSON struct {
	Slug    string             `json:"slug"`
	Name    string             `json:"name"`
	Account *signInAccountJSON `json:"account,omitzero"`
}

type signInAccountJSON struct {
	Username string    `json:"username"`
	LinkedAt time.Time `json:"linked_at"`
}

var (
	providerSlug    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	signInSlugParam = param{"slug", "", "The provider's slug, which names it in the address it sends browsers back to."}
)

const (
	maxIssuer       = 2000
	maxClientSecret = 2000
	maxPath         = 2000
)

func (a *API) signInProviders(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.SignIns.Providers(r.Context())
	if a.answered(w, r, err) {
		return
	}
	out := make([]signInProviderJSON, len(all))
	for i, p := range all {
		out[i] = signInProviderJSON{Slug: p.Slug, Name: p.Name}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[signInProviderJSON]{out})
}

func (a *API) adminSignInProviderOf(p domain.SignInProvider) adminSignInProviderJSON {
	out := adminSignInProviderJSON{
		Slug: p.Slug, Name: p.Name, Issuer: p.Issuer, ClientID: p.ClientID, ClientSecretSet: p.ClientSecret != "",
		Provisioning: p.Provisioning, Group: p.Group, Recheck: p.Recheck,
		Access: accessJSON{MaxAge: p.MaxAge, Unrated: cmp.Or(p.Unrated, domain.UnratedAllow), Libraries: p.Libraries},
	}
	if out.Access.Libraries == nil {
		out.Access.Libraries = []uuid.UUID{}
	}
	if cb := a.svc.SignIns.Callback(p.Slug); cb != nil {
		out.CallbackURL = cb.String()
	}
	return out
}

func (a *API) adminSignInProviders(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.SignIns.Providers(r.Context())
	if a.answered(w, r, err) {
		return
	}
	out := make([]adminSignInProviderJSON, len(all))
	for i, p := range all {
		out[i] = a.adminSignInProviderOf(p)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[adminSignInProviderJSON]{out})
}

func (a *API) setSignInProvider(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !providerSlug.MatchString(slug) {
		writeProblem(w, a.logger, codeInvalidParameter, "slug is 1 to 32 lower-case letters, digits and hyphens, starting with a letter or digit")
		return
	}
	var req signInProviderChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	name, nameOK := domain.ProfileName(req.Name)
	p := domain.SignInProvider{
		Slug: slug, Name: name, Issuer: strings.TrimSpace(req.Issuer), ClientID: strings.TrimSpace(req.ClientID),
		ClientSecret: req.ClientSecret, Provisioning: req.Provisioning, Group: strings.TrimSpace(req.Group),
		Recheck: cmp.Or(req.Recheck, domain.RecheckHourly),
		MaxAge:  req.Access.MaxAge, Unrated: cmp.Or(req.Access.Unrated, domain.UnratedAllow), Libraries: req.Access.Libraries,
	}
	switch {
	case !nameOK:
		writeProblem(w, a.logger, codeInvalidBody, "name is what the login page calls the provider: up to 64 characters")
		return
	case p.Issuer == "" || len(p.Issuer) > maxIssuer:
		writeProblem(w, a.logger, codeInvalidBody, "issuer is the provider's address, as its discovery document names it")
		return
	case p.ClientID == "" || len(p.ClientID) > maxClientID || strings.ContainsFunc(p.ClientID, unicode.IsSpace):
		writeProblem(w, a.logger, codeInvalidBody, "client_id is the id of the client registered at the provider, with no spaces")
		return
	case len(p.ClientSecret) > maxClientSecret:
		writeProblem(w, a.logger, codeInvalidBody, "client_secret is too long")
		return
	case p.Provisioning == "":
		writeProblem(w, a.logger, codeInvalidBody, "provisioning is required")
		return
	case p.MaxAge != nil && (*p.MaxAge < 0 || *p.MaxAge > 21):
		writeProblem(w, a.logger, codeInvalidBody, "access.max_age is an age to 21 or null")
		return
	}
	if p.ClientSecret == "" {
		now, err := a.svc.SignIns.Provider(r.Context(), slug)
		if !errors.Is(err, store.ErrNotFound) && a.answered(w, r, err) {
			return
		}
		if err == nil && now.ClientID == p.ClientID {
			p.ClientSecret = now.ClientSecret
		}
	}
	if a.answeredAs(w, r, a.svc.SignIns.SetProvider(r.Context(), p), "access.libraries names a library that is not") {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.adminSignInProviderOf(p))
}

func (a *API) removeSignInProvider(w http.ResponseWriter, r *http.Request) {
	if a.answeredAs(w, r, a.svc.SignIns.RemoveProvider(r.Context(), r.PathValue("slug")), "no provider has that slug") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// origin is an address's scheme and host, without the port its scheme has anyway, as a browser's
// Origin header names it.
func origin(u *url.URL) string {
	host := strings.ToLower(u.Host)
	switch u.Scheme {
	case "https":
		host = strings.TrimSuffix(host, ":443")
	case "http":
		host = strings.TrimSuffix(host, ":80")
	}
	return strings.ToLower(u.Scheme) + "://" + host
}

// localPath is a path on this server to send a browser on to, and never another site: the home
// page for none.
func localPath(to string) (string, bool) {
	if to == "" {
		return "/", true
	}
	u, err := url.Parse(to)
	ok := err == nil && u.Scheme == "" && u.Host == "" && u.User == nil && strings.HasPrefix(to, "/") &&
		!strings.HasPrefix(to, "//") && !strings.ContainsAny(to, `\`) && len(to) <= maxPath
	return to, ok
}

// leave sends a browser to a provider for f, binding it to the sign-in by the cookie, once it is
// asking from the server's public address: the provider sends it back there, and the cookie with
// it only if it left from there.
func (a *API) leave(w http.ResponseWriter, r *http.Request, f kv.SignInFlow) {
	slug := r.PathValue("slug")
	to, ok := localPath(f.To)
	if !ok {
		writeProblem(w, a.logger, codeInvalidBody, "to is a path on this server, starting with /")
		return
	}
	f.To = to
	if public := a.svc.Reach.PublicURL(); public != nil && r.Header.Get("Origin") != "" {
		from, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || origin(from) != origin(public) {
			writeProblem(w, a.logger, codeConflict, "Sign in at "+origin(public)+": the provider sends you back there.")
			return
		}
	}
	state, provider, err := a.svc.SignIns.Start(r.Context(), slug, f)
	if a.answeredAs(w, r, err, "no provider has that slug") {
		return
	}
	// Sent only back to the callback, at the path the browser is sent back to.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure wherever the browser has HTTPS, as the session's is
		Name: flowCookie, Value: state, Path: a.svc.SignIns.Callback(slug).Path,
		MaxAge: int(sso.FlowTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.svc.Reach.HTTPS(r),
	})
	writeJSON(w, a.logger, "application/json", http.StatusOK, signInRedirectJSON{URL: provider})
}

func (a *API) startSignIn(w http.ResponseWriter, r *http.Request) {
	var req signInStartJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Device == "" || req.Client == "" {
		writeProblem(w, a.logger, codeInvalidBody, "device and client are required")
		return
	}
	if !a.allowed(w, r, auth.SignInsPerAddress, a.addrKey(r, "signin")) {
		return
	}
	a.leave(w, r, kv.SignInFlow{Device: req.Device, Client: req.Client, To: req.To})
}

func (a *API) startLink(w http.ResponseWriter, r *http.Request) {
	var req linkStartJSON
	if !a.decode(w, r, &req) {
		return
	}
	a.leave(w, r, kv.SignInFlow{Linking: auth.SessionOf(r.Context()).Profile.ID, To: req.To})
}

// signInCallback is where a provider sends the browser back. It answers the browser, not a client:
// the account is signed in, in the session's cookie, or its link kept, and the browser sent on to
// where it asked; or sent to say why not. The query is the provider's, so none of it is refused.
func (a *API) signInCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")
	var bound string
	if c, err := r.Cookie(flowCookie); err == nil {
		bound = c.Value
	}
	if cb := a.svc.SignIns.Callback(slug); cb != nil {
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // cleared, as it was set
			Name: flowCookie, Path: cb.Path, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.svc.Reach.HTTPS(r),
		})
	}
	var signedIn uuid.UUID
	if c, err := r.Cookie(sessionCookie); err == nil {
		if s, err := a.svc.Auth.Authenticate(ctx, c.Value); err == nil {
			signedIn = s.Profile.ID
		}
	}
	q := r.URL.Query()
	out, err := a.svc.SignIns.Finish(ctx, slug, q.Get("state"), bound, signedIn, q)
	flow := out.Flow
	details := domain.SignInDetails{Device: flow.Device, Client: flow.Client, Address: a.svc.Reach.Client(r).String()}
	if err != nil {
		reason := sso.RefusalFailed
		if refused, ok := errors.AsType[*sso.Refused](err); ok {
			reason, details.Name = refused.Reason, refused.Account
			a.logger.InfoContext(ctx, "sign-in refused", slog.String("provider", slug), slog.Any("err", err))
		} else {
			a.logger.ErrorContext(ctx, "sign-in failed", slog.String("provider", slug), slog.Any("err", err))
		}
		if flow.Linking == (uuid.UUID{}) && reason != sso.RefusalExpired {
			a.svc.Events.Raise(ctx, domain.Event{Kind: domain.EventSignInRefused, Details: details})
		}
		http.Redirect(w, r, refusedPath(flow, reason), http.StatusSeeOther)
		return
	}
	if flow.Linking == (uuid.UUID{}) {
		token, err := a.svc.Auth.SignInAs(ctx, out.Profile, out.Identity, auth.Device{Name: flow.Device, Client: flow.Client})
		if err != nil {
			a.logger.ErrorContext(ctx, "sign-in failed", slog.String("provider", slug), slog.Any("err", err))
			http.Redirect(w, r, refusedPath(flow, sso.RefusalFailed), http.StatusSeeOther)
			return
		}
		details.Name = out.Profile.Name
		a.svc.Events.Raise(ctx, domain.Event{Kind: domain.EventSignedIn, Profile: out.Profile.ID, Details: details})
		a.keepSession(w, r, token)
	}
	http.Redirect(w, r, flow.To, http.StatusSeeOther)
}

// refusedPath is where a browser refused goes: a link back to where it started, a sign-in to the
// login page, each saying why.
func refusedPath(f kv.SignInFlow, reason sso.Refusal) string {
	to, err := url.Parse(f.To)
	if f.Linking != (uuid.UUID{}) && err == nil {
		q := to.Query()
		q.Set("refused", string(reason))
		to.RawQuery = q.Encode()
		return to.String()
	}
	q := url.Values{"refused": {string(reason)}}
	if f.To != "" && f.To != "/" {
		q.Set("to", f.To)
	}
	return loginPage + "?" + q.Encode()
}

func (a *API) ownSignInProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := a.svc.SignIns.Providers(ctx)
	if a.answered(w, r, err) {
		return
	}
	accounts, err := a.svc.SignIns.Accounts(ctx, auth.SessionOf(ctx).Profile.ID)
	if a.answered(w, r, err) {
		return
	}
	out := make([]profileSignInProviderJSON, len(all))
	for i, p := range all {
		out[i] = profileSignInProviderJSON{Slug: p.Slug, Name: p.Name}
		for _, acc := range accounts {
			if acc.Provider == p.Slug {
				out[i].Account = &signInAccountJSON{Username: acc.Username, LinkedAt: acc.LinkedAt.UTC()}
			}
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[profileSignInProviderJSON]{out})
}

func (a *API) unlinkSignIn(w http.ResponseWriter, r *http.Request) {
	err := a.svc.SignIns.Unlink(r.Context(), auth.SessionOf(r.Context()).Profile.ID, r.PathValue("slug"))
	if a.answeredAs(w, r, err, "the profile has linked no account there") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) signInRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/auth/sign-in-providers", access: public,
			summary: "List the providers the household signs in through, for the login page",
			status:  http.StatusOK, reply: listJSON[signInProviderJSON]{}, handle: a.signInProviders,
		},
		{
			pattern: "POST /api/v1/auth/sign-in-providers/{slug}/sign-in", access: public,
			summary: "Send a browser to sign in at a provider, from the server's public address; it comes back signed in, in the session's cookie",
			path:    []param{signInSlugParam}, body: signInStartJSON{}, status: http.StatusOK, reply: signInRedirectJSON{},
			handle: a.startSignIn,
		},
		{
			pattern: "GET /api/v1/admin/sign-in-providers", access: admin,
			summary: "List the OpenID Connect providers the server is registered on, and the redirect URI to register at each",
			status:  http.StatusOK, reply: listJSON[adminSignInProviderJSON]{}, handle: a.adminSignInProviders,
		},
		{
			pattern: "PUT /api/v1/admin/sign-in-providers/{slug}", access: admin,
			summary: "Register the server on an OpenID Connect provider, or change how; the provider must answer at its issuer",
			path:    []param{signInSlugParam}, body: signInProviderChangeJSON{}, status: http.StatusOK,
			reply: adminSignInProviderJSON{}, handle: a.setSignInProvider,
		},
		{
			pattern: "DELETE /api/v1/admin/sign-in-providers/{slug}", access: admin,
			summary: "Remove a provider, and every account profiles linked there, signing out the devices they signed in",
			path:    []param{signInSlugParam}, status: http.StatusNoContent, handle: a.removeSignInProvider,
		},
		{
			pattern: "GET /api/v1/profile/sign-in-providers", access: signedIn,
			summary: "The providers the household signs in through, and the account the profile linked at each",
			status:  http.StatusOK, reply: listJSON[profileSignInProviderJSON]{}, handle: a.ownSignInProviders,
		},
		{
			pattern: "POST /api/v1/profile/sign-in-providers/{slug}/link", access: signedIn,
			summary: "Send the browser to a provider to link the profile's account there, in place of any before",
			path:    []param{signInSlugParam}, body: linkStartJSON{}, status: http.StatusOK, reply: signInRedirectJSON{},
			handle: a.startLink,
		},
		{
			pattern: "DELETE /api/v1/profile/sign-in-providers/{slug}", access: signedIn,
			summary: "Unlink the profile's account at a provider, signing out the devices it signed in, but not one the profile has no other way in than",
			path:    []param{signInSlugParam}, status: http.StatusNoContent, handle: a.unlinkSignIn,
		},
	}
}
