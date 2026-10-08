package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
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

func (a *API) keys(w http.ResponseWriter, r *http.Request) {
	list, err := a.svc.Auth.Keys(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]keyListingJSON, len(list))
	for i, k := range list {
		out[i] = keyListingJSON{ID: k.ID, Name: k.Name, Profile: k.Profile, CreatedAt: k.CreatedAt, LastSeenAt: k.LastSeenAt}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[keyListingJSON]{Items: out})
}

func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var req newKeyJSON
	if !a.decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > maxKeyName {
		writeProblem(w, a.logger, codeInvalidBody, "name is 1 to 64 characters")
		return
	}
	id, token, err := a.svc.Auth.CreateKey(r.Context(), sessionOf(r), name)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, createdKeyJSON{ID: id, Token: token})
}

func (a *API) revokeKey(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if !a.answered(w, r, a.svc.Auth.RevokeKey(r.Context(), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) keysRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/keys", access: admin, summary: "List the API keys",
			status: http.StatusOK, reply: listJSON[keyListingJSON]{}, handle: a.keys,
		},
		{
			pattern: "POST /api/v1/admin/keys", access: admin,
			summary: "Make an API key acting as this admin; its token is shown only now",
			body:    newKeyJSON{}, status: http.StatusCreated, reply: createdKeyJSON{}, handle: a.createKey,
		},
		{
			pattern: "DELETE /api/v1/admin/keys/{id}", access: admin, summary: "Revoke an API key",
			status: http.StatusNoContent, handle: a.revokeKey,
		},
	}
}
