package httpapi

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
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
	ID uuid.UUID `json:"id"`
	// Endpoint is the address the browser's push service gave it, which tells one browser from
	// another.
	Endpoint uri `json:"endpoint"`
}

type pushKeyJSON struct {
	// Key is the server's VAPID key, unpadded base64url: the applicationServerKey a browser
	// subscribes with.
	Key string `json:"key"`
}

// subscribeJSON is a browser's PushSubscription as its toJSON() gives it.
type subscribeJSON struct {
	Endpoint uri `json:"endpoint"`
	Keys     struct {
		// P256DH is the browser's P-256 key and Auth its 16-byte secret, base64url.
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
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
		out.Items[i] = pushSubscriptionJSON{ID: s.ID, Endpoint: uri(s.Endpoint)}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// subscribe has a browser pushed the profile's notifications, worded in the languages it reads.
func (a *API) subscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeJSON
	if !a.decode(w, r, &req) {
		return
	}
	b64 := base64.RawURLEncoding
	// A browser may pad its keys, though the Push API says not to.
	p256dh, err := b64.DecodeString(strings.TrimRight(req.Keys.P256DH, "="))
	if err == nil {
		_, err = ecdh.P256().NewPublicKey(p256dh)
	}
	secret, authErr := b64.DecodeString(strings.TrimRight(req.Keys.Auth, "="))
	switch {
	case webpush.Check(string(req.Endpoint)) != nil:
		writeProblem(w, a.logger, codeInvalidBody, "endpoint is the https address the browser's push service gave it")
		return
	case err != nil || authErr != nil || len(secret) != 16:
		writeProblem(w, a.logger, codeInvalidBody, "keys are the browser's P-256 key and 16-byte secret, base64url")
		return
	}
	sub, made, err := a.svc.Push.Subscribe(r.Context(), auth.SessionOf(r.Context()).Profile.ID, store.PushSubscription{
		Kind: domain.PushWeb, Endpoint: string(req.Endpoint), P256DH: p256dh, Auth: secret, Language: words.Readable(r).String(),
	})
	if a.answered(w, r, err) {
		return
	}
	status := http.StatusOK
	if made {
		status = http.StatusCreated
	}
	writeJSON(w, a.logger, "application/json", status, pushSubscriptionJSON{ID: sub.ID, Endpoint: uri(sub.Endpoint)})
}

// unsubscribe stops a browser being pushed the profile's notifications.
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
			summary: "List the browsers pushed the profile's notifications",
			status:  http.StatusOK, reply: listJSON[pushSubscriptionJSON]{}, handle: a.pushSubscriptions,
		},
		{
			pattern: "POST /api/v1/profile/push-subscriptions", access: signedIn,
			summary: "Push the profile's notifications to a browser, by the subscription it made with the server's key; one subscribed already is the profile's from now",
			body:    subscribeJSON{}, status: http.StatusCreated, reply: pushSubscriptionJSON{}, again: http.StatusOK, handle: a.subscribe,
		},
		{
			pattern: "DELETE /api/v1/profile/push-subscriptions/{id}", access: signedIn,
			summary: "Stop pushing the profile's notifications to a browser",
			status:  http.StatusNoContent, handle: a.unsubscribe,
		},
	}
}
