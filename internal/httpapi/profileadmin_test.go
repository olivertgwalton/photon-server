package httpapi

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeProfiles has Oliver, the only admin, and keeps the hashes it is given.
type fakeProfiles struct {
	hashes map[string]string
	access store.ProfileAccess
}

func (f *fakeProfiles) AddProfile(_ context.Context, name string, role domain.Role, hash string) (domain.Profile, error) {
	if name == oliver.Name {
		return domain.Profile{}, store.ErrProfileExists
	}
	f.hashes[name] = hash
	return domain.Profile{ID: uuid.NewV7(), Name: name, Role: role}, nil
}

func (f *fakeProfiles) SetProfile(_ context.Context, id uuid.UUID, c store.ProfileChange) (domain.Profile, error) {
	if id != oliver.ID {
		return domain.Profile{}, store.ErrNotFound
	}
	if c.Role != "" && c.Role != domain.RoleAdmin {
		return domain.Profile{}, store.ErrLastAdmin
	}
	if c.Name == "Kid" {
		return domain.Profile{}, store.ErrProfileExists
	}
	renamed := oliver
	renamed.Name = cmp.Or(c.Name, oliver.Name)
	return renamed, nil
}

func (f *fakeProfiles) RemoveProfile(_ context.Context, id uuid.UUID) (string, error) {
	if id == oliver.ID {
		return "", store.ErrLastAdmin
	}
	return "", store.ErrNotFound
}

func (f *fakeProfiles) Access(_ context.Context, id uuid.UUID) (store.ProfileAccess, error) {
	if id != oliver.ID {
		return store.ProfileAccess{}, store.ErrNotFound
	}
	return f.access, nil
}

func (f *fakeProfiles) SetAccess(_ context.Context, id uuid.UUID, a store.ProfileAccess) error {
	if id != oliver.ID {
		return store.ErrNotFound
	}
	f.access = a
	return nil
}

func TestAnAdminKeepsTheHouseholdsProfiles(t *testing.T) {
	profiles := &fakeProfiles{hashes: map[string]string{}}
	told := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, ProfileAdmin: profiles, Events: told})
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
	}{
		{memberToken, http.MethodPost, "/api/v1/admin/profiles", `{"name": "Kid", "role": "user", "password": "correct horse"}`, http.StatusForbidden},
		{goodToken, http.MethodPost, "/api/v1/admin/profiles", `{"name": "Kid", "role": "user"}`, http.StatusBadRequest},
		{goodToken, http.MethodPost, "/api/v1/admin/profiles", `{"name": "Partner", "role": "user", "password": "correct horse"}`, http.StatusCreated},
		{goodToken, http.MethodPost, "/api/v1/admin/profiles", `{"name": "Short", "role": "user", "password": "hunter2"}`, http.StatusBadRequest},
		{goodToken, http.MethodPost, "/api/v1/admin/profiles", `{"name": "Oliver", "role": "user", "password": "correct horse"}`, http.StatusConflict},
		{goodToken, http.MethodPost, "/api/v1/admin/profiles", `{"name": "Cat", "role": "pet"}`, http.StatusBadRequest},
		{goodToken, http.MethodPatch, "/api/v1/admin/profiles/" + oliver.ID.String(), `{"role": "user"}`, http.StatusConflict},
		{goodToken, http.MethodPatch, "/api/v1/admin/profiles/" + oliver.ID.String(), `{"password": ""}`, http.StatusBadRequest},
		{goodToken, http.MethodPatch, "/api/v1/admin/profiles/" + oliver.ID.String(), `{"name": "Ollie"}`, http.StatusOK},
		{goodToken, http.MethodPatch, "/api/v1/admin/profiles/" + uuid.NewV7().String(), `{"name": "Nobody"}`, http.StatusNotFound},
		{goodToken, http.MethodDelete, "/api/v1/admin/profiles/" + oliver.ID.String(), "", http.StatusConflict},
		{goodToken, http.MethodPut, "/api/v1/admin/profiles/" + oliver.ID.String() + "/access", `{"max_age": 12, "unrated": "block", "libraries": []}`, http.StatusNoContent},
		{goodToken, http.MethodPut, "/api/v1/admin/profiles/" + oliver.ID.String() + "/access", `{"max_age": -1}`, http.StatusBadRequest},
		{goodToken, http.MethodPut, "/api/v1/admin/profiles/" + oliver.ID.String() + "/access", `{"unrated": "maybe"}`, http.StatusBadRequest},
		{goodToken, http.MethodPut, "/api/v1/admin/profiles/" + uuid.NewV7().String() + "/access", `{}`, http.StatusNotFound},
		{goodToken, http.MethodGet, "/api/v1/admin/profiles/" + oliver.ID.String() + "/access", "", http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s %s: %d, want %d: %s", tc.method, tc.target, tc.body, rec.Code, tc.want, rec.Body)
		}
	}
	if profiles.access.MaxAge == nil || *profiles.access.MaxAge != 12 || profiles.access.Unrated != domain.UnratedBlock {
		t.Errorf("access kept: %+v", profiles.access)
	}
	if _, ok := profiles.hashes["Kid"]; ok || !strings.HasPrefix(profiles.hashes["Partner"], "$argon2id$") {
		t.Errorf("hashes kept: %v, want only an argon2id hash for Partner", profiles.hashes)
	}
	if got, want := told.kinds(), []domain.EventKind{domain.EventProfileAdded}; !slices.Equal(got, want) {
		t.Errorf("told %v, want the one added", got)
	}
}

// A profile renames itself, as a Jellyfin user may, to a name no other has; an admin renames any.
func TestAProfileIsRenamed(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, ProfileAdmin: &fakeProfiles{hashes: map[string]string{}}})
	for _, tc := range []struct {
		method, target, body string
		want                 int
		said                 string
	}{
		{http.MethodPatch, "/api/v1/me", `{"name": "  Olly  "}`, http.StatusOK, `"name":"Olly"`},
		{http.MethodPatch, "/api/v1/me", `{"name": "Kid"}`, http.StatusConflict, ""},
		{http.MethodPatch, "/api/v1/me", `{"name": "   "}`, http.StatusBadRequest, "64 characters"},
		{http.MethodPatch, "/api/v1/me", `{"name": "` + strings.Repeat("o", 65) + `"}`, http.StatusBadRequest, ""},
		{http.MethodPatch, "/api/v1/me", `{"name": "Ol\u0007ly"}`, http.StatusBadRequest, ""},
		{http.MethodPatch, "/api/v1/admin/profiles/" + oliver.ID.String(), `{"name": " "}`, http.StatusBadRequest, ""},
		{http.MethodPatch, "/api/v1/admin/profiles/" + oliver.ID.String(), `{"name": "Oliver W"}`, http.StatusOK, `"name":"Oliver W"`},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.said) {
			t.Errorf("%s %s %s = %d %s, want %d %s", tc.method, tc.target, tc.body, rec.Code, rec.Body, tc.want, tc.said)
		}
	}
}
