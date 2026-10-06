package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type profileAdmin interface {
	AddProfile(ctx context.Context, name string, role domain.Role, passwordHash string) (domain.Profile, error)
	SetProfile(ctx context.Context, id uuid.UUID, c store.ProfileChange) (domain.Profile, error)
	RemoveProfile(ctx context.Context, id uuid.UUID) (string, error)
	Access(ctx context.Context, id uuid.UUID) (store.ProfileAccess, error)
	SetAccess(ctx context.Context, id uuid.UUID, a store.ProfileAccess) error
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
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	access, err := a.svc.ProfileAdmin.Access(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, accessJSON(access))
}

// setProfileAccess replaces what a profile may see: titles rated for max_age and younger, unrated
// ones allowed or blocked, and only the libraries listed, every one where none are.
func (a *API) setProfileAccess(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
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
	err := a.svc.ProfileAdmin.SetAccess(r.Context(), id, store.ProfileAccess(req))
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
	Password string      `json:"password,omitzero"`
}

// addProfile adds a profile of the household, as Jellyfin's dashboard adds a user. One with no
// password is chosen on a signed-in device and never signs in itself; an admin has one.
func (a *API) addProfile(w http.ResponseWriter, r *http.Request) {
	var req addProfileJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Role == "" || req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is set and role is admin, member or restricted")
		return
	}
	hash, ok := a.hash(w, r, req.Password)
	if !ok {
		return
	}
	if req.Role == domain.RoleAdmin && hash == "" {
		writeProblem(w, a.logger, codeInvalidBody, store.ErrAdminNeedsPassword.Error())
		return
	}
	p, err := a.svc.ProfileAdmin.AddProfile(r.Context(), req.Name, req.Role, hash)
	if a.answeredProfile(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileAdded, Profile: p.ID, Details: map[string]any{"name": p.Name}})
	writeJSON(w, a.logger, "application/json", http.StatusCreated, profileOf(p))
}

type profileChangeJSON struct {
	Name     string      `json:"name,omitzero"`
	Role     domain.Role `json:"role,omitzero"`
	Password *string     `json:"password,omitzero"`
}

// setProfile renames a profile, changes its role, and sets its password, or clears it with "".
func (a *API) setProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req profileChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	change := store.ProfileChange{Name: req.Name, Role: req.Role}
	if req.Password != nil {
		hash, ok := a.hash(w, r, *req.Password)
		if !ok {
			return
		}
		change.PasswordHash = &hash
	}
	p, err := a.svc.ProfileAdmin.SetProfile(r.Context(), id, change)
	if a.answeredProfile(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(p))
}

// removeProfile forgets a profile, its devices and what it has watched.
func (a *API) removeProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	name, err := a.svc.ProfileAdmin.RemoveProfile(r.Context(), id)
	if a.answeredProfile(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileRemoved, Details: map[string]any{"name": name}})
	w.WriteHeader(http.StatusNoContent)
}

// hash hashes a password, "" for none.
func (a *API) hash(w http.ResponseWriter, r *http.Request, password string) (string, bool) {
	if password == "" {
		return "", true
	}
	hash, err := auth.HashPassword(r.Context(), password)
	if errors.Is(err, auth.ErrPasswordTooShort) {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return "", false
	}
	if err != nil {
		a.internal(w, r, err)
		return "", false
	}
	return hash, true
}

func (a *API) answeredProfile(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
	case errors.Is(err, store.ErrProfileExists):
		writeProblem(w, a.logger, codeConflict, "a profile has that name")
	case errors.Is(err, store.ErrLastAdmin), errors.Is(err, store.ErrAdminNeedsPassword):
		writeProblem(w, a.logger, codeConflict, err.Error())
	default:
		a.internal(w, r, err)
	}
	return true
}
