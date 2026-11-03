package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

const maxKeyName = 64

type keyListingJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Profile is the admin who made it, whom it acts as.
	Profile    string    `json:"profile"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type newKeyJSON struct {
	// Name says what the key is for, as Jellyfin's app name does.
	Name string `json:"name"`
}

// createdKeyJSON is the only time the key's token is shown: the server keeps only its hash.
type createdKeyJSON struct {
	ID    uuid.UUID `json:"id"`
	Token string    `json:"token"`
}

func (a *API) keys(r *http.Request, _ none) (listJSON[keyListingJSON], error) {
	list, err := a.svc.Auth.Keys(r.Context())
	out := make([]keyListingJSON, len(list))
	for i, k := range list {
		out[i] = keyListingJSON{ID: k.ID, Name: k.Name, Profile: k.Profile, CreatedAt: k.CreatedAt, LastSeenAt: k.LastSeenAt}
	}
	return listJSON[keyListingJSON]{Items: out}, err
}

func (a *API) createKey(r *http.Request, req newKeyJSON) (createdKeyJSON, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > maxKeyName {
		return createdKeyJSON{}, refuse(codeInvalidBody, "name is 1 to 64 characters")
	}
	id, token, err := a.svc.Auth.CreateKey(r.Context(), auth.SessionOf(r.Context()), name)
	return createdKeyJSON{ID: id, Token: token}, err
}

func (a *API) revokeKey(r *http.Request, _ none) (none, error) {
	profile, err := a.svc.Auth.RevokeKey(r.Context(), pathUUID(r, "id"))
	if err != nil {
		return none{}, err
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSessionsChanged, Profile: profile})
	return none{}, nil
}

func (a *API) keysRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/keys", access: admin, summary: "List the API keys",
			status: http.StatusOK,
		}, a.keys),
		a.handles(route{
			pattern: "POST /api/v1/admin/keys", access: admin,
			summary: "Make an API key acting as this admin; its token is shown only now",
			status:  http.StatusCreated,
		}, a.createKey),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/keys/{id}", access: admin, summary: "Revoke an API key",
			status: http.StatusNoContent,
		}, a.revokeKey),
	}
}
