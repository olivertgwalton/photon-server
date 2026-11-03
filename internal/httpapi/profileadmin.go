package httpapi

import (
	"cmp"
	"context"
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
	MaxAge  *int           `json:"max_age"`
	Unrated domain.Unrated `json:"unrated"`
	// MaxCommonSenseAge is the oldest age Common Sense Media advises a title it sees for; null for
	// any. A title Common Sense has no age for is judged by its certificate alone.
	MaxCommonSenseAge *int        `json:"max_common_sense_age"`
	Libraries         []uuid.UUID `json:"libraries"`
}

// commonSenseAgeOK is whether an age is one Common Sense Media advises titles for, or none.
func commonSenseAgeOK(age *int) bool {
	return age == nil || *age >= 1 && *age <= domain.MaxCommonSenseAge
}

// profileAccess answers what a profile may see, as Jellyfin's parental control and library access
// say it.
func (a *API) profileAccess(r *http.Request, _ none) (accessJSON, error) {
	access, err := a.svc.ProfileAdmin.Access(r.Context(), pathUUID(r, "id"), keeper(r))
	return accessJSON(access), err
}

// setProfileAccess replaces what a profile may see: titles rated for max_age and younger, unrated
// ones allowed or blocked, and only the libraries listed, every one where none are.
func (a *API) setProfileAccess(r *http.Request, req accessJSON) (none, error) {
	req.Unrated = cmp.Or(req.Unrated, domain.UnratedAllow)
	if req.MaxAge != nil && (*req.MaxAge < 0 || *req.MaxAge > 21) {
		return none{}, refuse(codeInvalidBody, "max_age is an age to 21 or null, and unrated is allow or block")
	}
	if !commonSenseAgeOK(req.MaxCommonSenseAge) {
		return none{}, refuse(codeInvalidBody, "max_common_sense_age is an age from 1 to 18 or null")
	}
	err := a.svc.ProfileAdmin.SetAccess(r.Context(), pathUUID(r, "id"), store.ProfileAccess(req), keeper(r))
	return none{}, notFoundAs(err, "no such profile, or one of its libraries")
}

type addProfileJSON struct {
	Name     string      `json:"name"`
	Role     domain.Role `json:"role"`
	Password string      `json:"password"`
}

// addProfile adds a profile of the household, as Jellyfin's dashboard adds a user; a manager adds
// one it keeps.
func (a *API) addProfile(r *http.Request, req addProfileJSON) (profileJSON, error) {
	name, ok := domain.ProfileName(req.Name)
	if req.Role == "" || !ok {
		return profileJSON{}, refuse(codeInvalidBody, badName+", and role is admin, manager or user")
	}
	hash, err := a.svc.Auth.HashPassword(r.Context(), req.Password)
	if err != nil {
		return profileJSON{}, err
	}
	p, err := a.svc.ProfileAdmin.AddProfile(r.Context(), name, req.Role, hash, keeper(r))
	if err != nil {
		return profileJSON{}, err
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileAdded, Profile: p.ID, Details: domain.NameDetails{Name: p.Name}})
	return profileOf(p), nil
}

// badName is what a profile's name must be.
var badName = "name is up to " + strconv.Itoa(domain.MaxProfileName) + " characters, besides space and control characters"

type profileChangeJSON struct {
	Name     string      `json:"name,omitzero"`
	Role     domain.Role `json:"role,omitzero"`
	Password *string     `json:"password,omitzero"`
}

// setProfile renames a profile, changes its role, and sets its password.
func (a *API) setProfile(r *http.Request, req profileChangeJSON) (profileJSON, error) {
	change := store.ProfileChange{Role: req.Role, Keep: auth.SessionOf(r.Context()).ID}
	if req.Name != "" {
		var ok bool
		if change.Name, ok = domain.ProfileName(req.Name); !ok {
			return profileJSON{}, refuse(codeInvalidBody, badName)
		}
	}
	if req.Password != nil {
		var err error
		if change.PasswordHash, err = a.svc.Auth.HashPassword(r.Context(), *req.Password); err != nil {
			return profileJSON{}, err
		}
	}
	p, err := a.svc.ProfileAdmin.SetProfile(r.Context(), pathUUID(r, "id"), change, keeper(r))
	if err != nil {
		return profileJSON{}, err
	}
	if change.Role != "" || change.PasswordHash != "" {
		a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSessionsChanged, Profile: p.ID})
	}
	return profileOf(p), nil
}

// removeProfile forgets a profile, its devices and what it has watched.
func (a *API) removeProfile(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	name, err := a.svc.ProfileAdmin.RemoveProfile(r.Context(), id, keeper(r))
	if err != nil {
		return none{}, err
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSessionsChanged, Profile: id})
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileRemoved, Details: domain.NameDetails{Name: name}})
	return none{}, nil
}

func (a *API) profileAdminRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "POST /api/v1/admin/profiles", access: manages, summary: "Add a profile",
			status: http.StatusCreated,
		}, a.addProfile),
		a.handles(route{
			pattern: "PATCH /api/v1/admin/profiles/{id}", access: manages, summary: "Change a profile",
			status: http.StatusOK,
		}, a.setProfile),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/profiles/{id}", access: manages,
			summary: "Remove a profile, its devices and what it has watched", status: http.StatusNoContent,
		}, a.removeProfile),
		a.handles(route{
			pattern: "GET /api/v1/admin/profiles/{id}/access", access: manages, summary: "What a profile may see",
			status: http.StatusOK,
		}, a.profileAccess),
		a.handles(route{
			pattern: "PUT /api/v1/admin/profiles/{id}/access", access: manages, summary: "Replace what a profile may see",
			status: http.StatusNoContent,
		}, a.setProfileAccess),
	}
}
