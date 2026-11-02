package httpapi

import (
	"cmp"
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
	AddWebhook(ctx context.Context, url string, format domain.WebhookFormat, kinds []domain.EventKind, secret string) (store.Webhook, error)
	RemoveWebhook(ctx context.Context, id uuid.UUID) error
}

type webhookJSON struct {
	ID  uuid.UUID `json:"id"`
	URL uri       `json:"url"`
	// Format is how its events are sent: photon's own signed JSON, or as a chat or push service
	// takes a message.
	Format    domain.WebhookFormat `json:"format"`
	Events    []domain.EventKind   `json:"events"`
	CreatedAt time.Time            `json:"created_at"`
}

type addWebhookJSON struct {
	URL uri `json:"url"`
	// Format is photon's own signed JSON where it is left out.
	Format domain.WebhookFormat `json:"format,omitzero"`
	Events []domain.EventKind   `json:"events"`
}

type addedWebhookJSON struct {
	webhookJSON
	Secret string `json:"secret"`
}

func webhookOf(w store.Webhook) webhookJSON {
	return webhookJSON{ID: w.ID, URL: uri(w.URL), Format: w.Format, Events: w.Events, CreatedAt: w.CreatedAt.UTC()}
}

// adminWebhooks lists the webhooks, never their secrets.
func (a *API) adminWebhooks(r *http.Request, _ none) (listJSON[webhookJSON], error) {
	all, err := a.svc.Webhooks.Webhooks(r.Context())
	out := make([]webhookJSON, len(all))
	for i, h := range all {
		out[i] = webhookOf(h)
	}
	return listJSON[webhookJSON]{out}, err
}

// addWebhook adds an address to POST the events it asks for to, and answers the secret each body
// is signed with: once, as it is never answered again.
func (a *API) addWebhook(r *http.Request, req addWebhookJSON) (addedWebhookJSON, error) {
	if u, err := url.Parse(string(req.URL)); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return addedWebhookJSON{}, refuse(codeInvalidBody, "url is an http or https address")
	}
	hookable := domain.HookableEventKinds()
	for i, k := range req.Events {
		if !slices.Contains(hookable, k) || slices.Contains(req.Events[:i], k) {
			req.Events = nil
			break
		}
	}
	if len(req.Events) == 0 {
		return addedWebhookJSON{}, refuse(codeInvalidBody, fmt.Sprintf("events are some of %v, each once", hookable))
	}
	secret := rand.Text()
	hook, err := a.svc.Webhooks.AddWebhook(r.Context(), string(req.URL), cmp.Or(req.Format, domain.WebhookPhoton), req.Events, secret)
	return addedWebhookJSON{webhookOf(hook), secret}, err
}

func (a *API) removeWebhook(r *http.Request, _ none) (none, error) {
	return none{}, a.svc.Webhooks.RemoveWebhook(r.Context(), pathUUID(r, "id"))
}

// testWebhook queues a webhook.test event to one webhook, whatever it asked for.
func (a *API) testWebhook(r *http.Request, _ none) (none, error) {
	return none{}, a.svc.Events.TestWebhook(r.Context(), pathUUID(r, "id"))
}

func (a *API) webhooksRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/webhooks", access: admin, summary: "List the webhooks, without their secrets",
			status: http.StatusOK,
		}, a.adminWebhooks),
		a.handles(route{
			pattern: "POST /api/v1/admin/webhooks", access: admin,
			summary: "Add a webhook, answering the secret its bodies are signed with, this once",
			status:  http.StatusCreated,
		}, a.addWebhook),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/webhooks/{id}", access: admin, summary: "Remove a webhook and what waits to be sent to it",
			status: http.StatusNoContent,
		}, a.removeWebhook),
		a.handles(route{
			pattern: "POST /api/v1/admin/webhooks/{id}/test", access: admin, summary: "Send a webhook.test event to one webhook",
			status: http.StatusAccepted,
		}, a.testWebhook),
	}
}
