package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type profileAdmin interface {
	AddProfile(ctx context.Context, name string, role domain.Role, passwordHash string, by *uuid.UUID) (domain.Profile, error)
	SetProfile(ctx context.Context, id uuid.UUID, c store.ProfileChange, by *uuid.UUID) (domain.Profile, error)
	RemoveProfile(ctx context.Context, id uuid.UUID, by *uuid.UUID) (string, error)
	Access(ctx context.Context, id uuid.UUID, by *uuid.UUID) (store.ProfileAccess, error)
	SetAccess(ctx context.Context, id uuid.UUID, a store.ProfileAccess, by *uuid.UUID) error
}

type accessJSON struct {
	// MaxAge is the oldest certificate it sees, by the age it is for; null for any.
	MaxAge    *int           `json:"max_age"`
	Unrated   domain.Unrated `json:"unrated"`
	Libraries []uuid.UUID    `json:"libraries"`
}

// profileAccess answers what a profile may see, as Jellyfin's parental control and library access
// say it.
func (a *API) profileAccess(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	access, err := a.svc.ProfileAdmin.Access(r.Context(), id, keeper(r))
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, accessJSON(access))
}

// setProfileAccess replaces what a profile may see: titles rated for max_age and younger, unrated
// ones allowed or blocked, and only the libraries listed, every one where none are.
func (a *API) setProfileAccess(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req accessJSON
	if !a.decode(w, r, &req) {
		return
	}
	req.Unrated = cmp.Or(req.Unrated, domain.UnratedAllow)
	if req.MaxAge != nil && (*req.MaxAge < 0 || *req.MaxAge > 21) {
		writeProblem(w, a.logger, codeInvalidBody, "max_age is an age to 21 or null, and unrated is allow or block")
		return
	}
	err := a.svc.ProfileAdmin.SetAccess(r.Context(), id, store.ProfileAccess(req), keeper(r))
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no such profile, or one of its libraries")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type addProfileJSON struct {
	Name     string      `json:"name"`
	Role     domain.Role `json:"role"`
	Password string      `json:"password"`
}

// addProfile adds a profile of the household, as Jellyfin's dashboard adds a user; a manager adds
// one it keeps.
func (a *API) addProfile(w http.ResponseWriter, r *http.Request) {
	var req addProfileJSON
	if !a.decode(w, r, &req) {
		return
	}
	name, ok := domain.ProfileName(req.Name)
	if req.Role == "" || !ok {
		writeProblem(w, a.logger, codeInvalidBody, badName+", and role is admin, manager or user")
		return
	}
	req.Name = name
	hash, err := auth.HashPassword(r.Context(), req.Password)
	if a.answered(w, r, err) {
		return
	}
	p, err := a.svc.ProfileAdmin.AddProfile(r.Context(), req.Name, req.Role, hash, keeper(r))
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileAdded, Profile: p.ID, Details: map[string]any{"name": p.Name}})
	writeJSON(w, a.logger, "application/json", http.StatusCreated, profileOf(p))
}

// badName is what a profile's name must be.
var badName = "name is up to " + strconv.Itoa(domain.MaxProfileName) + " characters, besides space and control characters"

type profileChangeJSON struct {
	Name     string      `json:"name,omitzero"`
	Role     domain.Role `json:"role,omitzero"`
	Password *string     `json:"password,omitzero"`
}

// setProfile renames a profile, changes its role, and sets its password.
func (a *API) setProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req profileChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	change := store.ProfileChange{Role: req.Role}
	if req.Name != "" {
		var ok bool
		if change.Name, ok = domain.ProfileName(req.Name); !ok {
			writeProblem(w, a.logger, codeInvalidBody, badName)
			return
		}
	}
	if req.Password != nil {
		var err error
		if change.PasswordHash, err = auth.HashPassword(r.Context(), *req.Password); a.answered(w, r, err) {
			return
		}
	}
	p, err := a.svc.ProfileAdmin.SetProfile(r.Context(), id, change, keeper(r))
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(p))
}

// removeProfile forgets a profile, its devices and what it has watched.
func (a *API) removeProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	name, err := a.svc.ProfileAdmin.RemoveProfile(r.Context(), id, keeper(r))
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileRemoved, Details: map[string]any{"name": name}})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) profileAdminRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/admin/profiles", access: manages, summary: "Add a profile",
			body: addProfileJSON{}, status: http.StatusCreated, reply: profileJSON{}, handle: a.addProfile,
		},
		{
			pattern: "PATCH /api/v1/admin/profiles/{id}", access: manages, summary: "Change a profile",
			body: profileChangeJSON{}, status: http.StatusOK, reply: profileJSON{}, handle: a.setProfile,
		},
		{
			pattern: "DELETE /api/v1/admin/profiles/{id}", access: manages,
			summary: "Remove a profile, its devices and what it has watched", status: http.StatusNoContent,
			handle: a.removeProfile,
		},
		{
			pattern: "GET /api/v1/admin/profiles/{id}/access", access: manages, summary: "What a profile may see",
			status: http.StatusOK, reply: accessJSON{}, handle: a.profileAccess,
		},
		{
			pattern: "PUT /api/v1/admin/profiles/{id}/access", access: manages, summary: "Replace what a profile may see",
			body: accessJSON{}, status: http.StatusNoContent, handle: a.setProfileAccess,
		},
	}
}
