package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"
)

// An admin invites a profile that has no password, and is answered the code it sends; one with a
// password, or none it keeps, has no invite.
func TestAnInviteIsMadeForAProfileWithoutAPassword(t *testing.T) {
	api := resetAPI(&bytes.Buffer{})
	for _, tc := range []struct {
		name, token string
		profile     uuid.UUID
		want        int
	}{
		{"a user", memberToken, kid.ID, http.StatusForbidden},
		{"one with a password", goodToken, oliver.ID, http.StatusConflict},
		{"one not kept", goodToken, uuid.NewV7(), http.StatusNotFound},
		{"one without a password", goodToken, kid.ID, http.StatusCreated},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/profiles/"+tc.profile.String()+"/invites", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("inviting %s: %d, want %d: %s", tc.name, rec.Code, tc.want, rec.Body)
			continue
		}
		if tc.want != http.StatusCreated {
			continue
		}
		var got inviteJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Code != inviteCode || got.ExpiresInMS <= 0 ||
			got.Link != "http://example.com/auth/invite#kid%27s%20invite" {
			t.Errorf("the invite answered: %+v, %v", got, err)
		}
	}
}

// Accepting an invite chooses the password and signs the device in as its profile.
func TestAnInviteIsAcceptedWithAPasswordChosen(t *testing.T) {
	api := resetAPI(&bytes.Buffer{})
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"no device", `{"code":"kid's invite","password":"new horse battery"}`, http.StatusBadRequest},
		{"a short password", `{"code":"kid's invite","password":"short","device":"Laptop","client":"web"}`, http.StatusBadRequest},
		{"another code", `{"code":"someone else's","password":"new horse battery","device":"Laptop","client":"web"}`, http.StatusNotFound},
		{"the code", `{"code":"kid's invite","password":"new horse battery","device":"Laptop","client":"web"}`, http.StatusOK},
	} {
		rec := askReset(t, api, "/api/v1/auth/invites/redemptions", "203.0.113.9", tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: %d, want %d: %s", tc.name, rec.Code, tc.want, rec.Body)
			continue
		}
		if tc.want != http.StatusOK {
			continue
		}
		if body := rec.Body.String(); !strings.Contains(body, goodToken) || !strings.Contains(body, kid.ID.String()) {
			t.Errorf("signed in as %s, want Kid with a token", body)
		}
	}
}
