package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type webhooks interface {
	Webhooks(ctx context.Context) ([]store.Webhook, error)
	AddWebhook(ctx context.Context, url string, kinds []domain.EventKind, secret string) (store.Webhook, error)
	RemoveWebhook(ctx context.Context, id uuid.UUID) error
}

type webhookJSON struct {
	ID        uuid.UUID          `json:"id"`
	URL       string             `json:"url"`
	Events    []domain.EventKind `json:"events"`
	CreatedAt time.Time          `json:"created_at"`
}

type addWebhookJSON struct {
	URL    string             `json:"url"`
	Events []domain.EventKind `json:"events"`
}

type addedWebhookJSON struct {
	webhookJSON
	Secret string `json:"secret"`
}

func webhookOf(w store.Webhook) webhookJSON {
	return webhookJSON{ID: w.ID, URL: w.URL, Events: nonNil(w.Events), CreatedAt: w.CreatedAt.UTC()}
}

// adminWebhooks lists the webhooks, never their secrets.
func (a *API) adminWebhooks(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.Webhooks.Webhooks(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]webhookJSON, len(all))
	for i, h := range all {
		out[i] = webhookOf(h)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[webhookJSON]{out})
}

// addWebhook adds an address to POST the events it asks for to, and answers the secret each body
// is signed with: once, as it is never answered again.
func (a *API) addWebhook(w http.ResponseWriter, r *http.Request) {
	var req addWebhookJSON
	if !a.decode(w, r, &req) {
		return
	}
	if u, err := url.Parse(req.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeProblem(w, a.logger, codeInvalidBody, "url is an http or https address")
		return
	}
	hookable := domain.HookableEventKinds()
	for i, k := range req.Events {
		if !slices.Contains(hookable, k) || slices.Contains(req.Events[:i], k) {
			req.Events = nil
			break
		}
	}
	if len(req.Events) == 0 {
		writeProblem(w, a.logger, codeInvalidBody, fmt.Sprintf("events are some of %v, each once", hookable))
		return
	}
	secret := rand.Text()
	hook, err := a.svc.Webhooks.AddWebhook(r.Context(), req.URL, req.Events, secret)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, addedWebhookJSON{webhookOf(hook), secret})
}

func (a *API) removeWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Webhooks.RemoveWebhook(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testWebhook queues a webhook.test event to one webhook, whatever it asked for.
func (a *API) testWebhook(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Events.TestWebhook(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
