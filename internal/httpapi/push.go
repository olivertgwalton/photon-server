package httpapi

import (
	"cmp"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/webpush"
	"github.com/olivertgwalton/photon-server/internal/words"
)

type pushSubscriptions interface {
	Subscribe(ctx context.Context, profile uuid.UUID, sub store.PushSubscription) (store.PushSubscription, bool, error)
	PushSubscriptions(ctx context.Context, profile uuid.UUID) ([]store.PushSubscription, error)
	Unsubscribe(ctx context.Context, profile, id uuid.UUID) error
}

type pushKey interface {
	PublicKey() []byte
}

type pushSubscriptionJSON struct {
	ID   uuid.UUID       `json:"id"`
	Kind domain.PushKind `json:"kind"`
	// Endpoint is the address a browser's push service gave it, and Token an Apple device's APNs
	// token: either tells one device from another.
	Endpoint uri    `json:"endpoint,omitzero"`
	Token    string `json:"token,omitzero"`
}

type pushKeyJSON struct {
	// Key is the server's VAPID key, unpadded base64url: the applicationServerKey a browser
	// subscribes with.
	Key string `json:"key"`
}

// subscribeJSON is a browser's PushSubscription as its toJSON() gives it, or an Apple device's
// token and the push service that issued it.
type subscribeJSON struct {
	// Kind is web where it is left out, so a browser sends its subscription as it is.
	Kind     domain.PushKind `json:"kind,omitzero"`
	Endpoint uri             `json:"endpoint,omitzero"`
	Keys     struct {
		// P256DH is the browser's P-256 key and Auth its 16-byte secret, base64url.
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys,omitzero"`
	// Token is an Apple device's APNs token, hex, and Environment the push service that issued it.
	Token       string                 `json:"token,omitzero"`
	Environment domain.APNsEnvironment `json:"environment,omitzero"`
}

// subscriptionOf is a subscription as it is listed: a browser by its endpoint, a device by its
// token.
func subscriptionOf(s store.PushSubscription) pushSubscriptionJSON {
	out := pushSubscriptionJSON{ID: s.ID, Kind: s.Kind}
	switch s.Kind {
	case domain.PushWeb:
		out.Endpoint = uri(s.Endpoint)
	case domain.PushApple:
		out.Token = s.Endpoint
	}
	return out
}

// pushKey answers the key a browser subscribes to the server's pushes with.
func (a *API) pushKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, a.logger, "application/json", http.StatusOK, pushKeyJSON{
		Key: base64.RawURLEncoding.EncodeToString(a.svc.PushKey.PublicKey()),
	})
}

// pushSubscriptions answers the browsers pushed the profile's notifications.
func (a *API) pushSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := a.svc.Push.PushSubscriptions(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if a.answered(w, r, err) {
		return
	}
	out := listJSON[pushSubscriptionJSON]{Items: make([]pushSubscriptionJSON, len(subs))}
	for i, s := range subs {
		out.Items[i] = subscriptionOf(s)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// subscribe has a browser or an Apple device pushed the profile's notifications, worded in the
// languages it reads.
func (a *API) subscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeJSON
	if !a.decode(w, r, &req) {
		return
	}
	sub := store.PushSubscription{Kind: cmp.Or(req.Kind, domain.PushWeb), Language: words.Readable(r).String()}
	var refused string
	switch sub.Kind {
	case domain.PushWeb:
		refused = webSubscription(req, &sub)
	case domain.PushApple:
		refused = appleSubscription(req, &sub)
	}
	if refused != "" {
		writeProblem(w, a.logger, codeInvalidBody, refused)
		return
	}
	sub, made, err := a.svc.Push.Subscribe(r.Context(), auth.SessionOf(r.Context()).Profile.ID, sub)
	if a.answered(w, r, err) {
		return
	}
	status := http.StatusOK
	if made {
		status = http.StatusCreated
	}
	writeJSON(w, a.logger, "application/json", status, subscriptionOf(sub))
}

// webSubscription fills a browser's subscription, answering what is wrong with it if anything.
func webSubscription(req subscribeJSON, sub *store.PushSubscription) string {
	b64 := base64.RawURLEncoding
	// A browser may pad its keys, though the Push API says not to.
	p256dh, err := b64.DecodeString(strings.TrimRight(req.Keys.P256DH, "="))
	if err == nil {
		_, err = ecdh.P256().NewPublicKey(p256dh)
	}
	secret, authErr := b64.DecodeString(strings.TrimRight(req.Keys.Auth, "="))
	switch {
	case webpush.Check(string(req.Endpoint)) != nil:
		return "endpoint is the https address the browser's push service gave it"
	case err != nil || authErr != nil || len(secret) != 16:
		return "keys are the browser's P-256 key and 16-byte secret, base64url"
	}
	sub.Endpoint, sub.P256DH, sub.Auth = string(req.Endpoint), p256dh, secret
	return ""
}

// apnsToken is the most an APNs token is: 32 bytes long, and allowed by Apple to grow to 100.
const apnsToken = 100

// appleSubscription fills an Apple device's subscription, answering what is wrong with it if
// anything.
func appleSubscription(req subscribeJSON, sub *store.PushSubscription) string {
	token, err := hex.DecodeString(req.Token)
	switch {
	case err != nil || len(token) == 0 || len(token) > apnsToken:
		return "token is the device's APNs token, hex"
	case req.Environment == "":
		return "environment is the APNs environment that issued the token"
	}
	environment := req.Environment
	sub.Endpoint, sub.ApnsEnvironment = strings.ToLower(req.Token), &environment
	return ""
}

// unsubscribe stops a device being pushed the profile's notifications.
func (a *API) unsubscribe(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.svc.Push.Unsubscribe(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if !a.answeredAs(w, r, err, "no such subscription") {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) pushRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/push-key", access: signedIn,
			summary: "The key a browser subscribes to the server's pushes with",
			status:  http.StatusOK, reply: pushKeyJSON{}, handle: a.pushKey,
		},
		{
			pattern: "GET /api/v1/profile/push-subscriptions", access: signedIn,
			summary: "List the browsers and Apple devices pushed the profile's notifications",
			status:  http.StatusOK, reply: listJSON[pushSubscriptionJSON]{}, handle: a.pushSubscriptions,
		},
		{
			pattern: "POST /api/v1/profile/push-subscriptions", access: signedIn,
			summary: "Push the profile's notifications to a browser, by the subscription it made with the server's key, or to an Apple device by its APNs token; one subscribed already is the profile's from now",
			body:    subscribeJSON{}, status: http.StatusCreated, reply: pushSubscriptionJSON{}, again: http.StatusOK, handle: a.subscribe,
		},
		{
			pattern: "DELETE /api/v1/profile/push-subscriptions/{id}", access: signedIn,
			summary: "Stop pushing the profile's notifications to a browser or an Apple device",
			status:  http.StatusNoContent, handle: a.unsubscribe,
		},
	}
}
