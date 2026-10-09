package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/tracker"
)

// fakeTrackers has Trakt set up and Simkl not, and a profile linked on Trakt once it has asked for
// a code.
type fakeTrackers struct {
	clients map[domain.Tracker]string
	linking bool
}

func (f *fakeTrackers) TrackerClients(context.Context) (map[domain.Tracker]string, error) {
	return f.clients, nil
}

func (f *fakeTrackers) SetTrackerClient(_ context.Context, t domain.Tracker, clientID string) error {
	f.clients[t] = clientID
	return nil
}

func (f *fakeTrackers) Trackers(context.Context, uuid.UUID) ([]tracker.Status, error) {
	trakt := tracker.Status{Tracker: domain.TrackerTrakt, State: tracker.StateUnlinked}
	if f.linking {
		trakt.State, trakt.Link = tracker.StateLinking, kv.TrackerLink{UserCode: "TRAKT123", VerificationURI: "https://trakt.tv/activate", VerificationURIComplete: "https://trakt.tv/activate/TRAKT123", Interval: 5 * time.Second, Expires: time.Now().Add(10 * time.Minute)}
	}
	return []tracker.Status{trakt, {Tracker: domain.TrackerSimkl, State: tracker.StateUnavailable}}, nil
}

func (f *fakeTrackers) Link(_ context.Context, _ uuid.UUID, t domain.Tracker) (kv.TrackerLink, error) {
	if f.clients[t] == "" {
		return kv.TrackerLink{}, fmt.Errorf("%w: %s is not set up: an admin sets its client id", tracker.ErrRefused, t)
	}
	f.linking = true
	return kv.TrackerLink{UserCode: "TRAKT123", VerificationURI: "https://trakt.tv/activate", VerificationURIComplete: "https://trakt.tv/activate/TRAKT123", Interval: 5 * time.Second, Expires: time.Now().Add(10 * time.Minute), DeviceCode: "secret"}, nil
}

func (f *fakeTrackers) Unlink(context.Context, uuid.UUID, domain.Tracker) error {
	if !f.linking {
		return store.ErrNotFound
	}
	f.linking = false
	return nil
}

func TestAnAdminSetsUpATrackerAndAProfileLinksIt(t *testing.T) {
	f := &fakeTrackers{clients: map[domain.Tracker]string{}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Trackers: f, TrackerClients: f})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
		says                        string
	}{
		{memberToken, http.MethodPut, "/api/v1/admin/trackers/trakt", `{"client_id": "app"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/trackers/letterboxd", `{"client_id": "app"}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/trackers/trakt", `{"client_id": "an app"}`, http.StatusBadRequest, "no spaces"},
		{memberToken, http.MethodPost, "/api/v1/profile/trackers/trakt/link", "", http.StatusConflict, "an admin sets its client id"},
		{memberToken, http.MethodDelete, "/api/v1/profile/trackers/trakt", "", http.StatusNotFound, "linked no account"},
		{goodToken, http.MethodPut, "/api/v1/admin/trackers/trakt", `{"client_id": " app "}`, http.StatusOK, `"client_id":"app"`},
		{goodToken, http.MethodGet, "/api/v1/admin/trackers", "", http.StatusOK, `{"tracker":"simkl","client_id":""}`},
		{memberToken, http.MethodPost, "/api/v1/profile/trackers/trakt/link", "", http.StatusOK, `"user_code":"TRAKT123"`},
		{memberToken, http.MethodGet, "/api/v1/profile/trackers", "", http.StatusOK, `"state":"linking","code":{"user_code":"TRAKT123"`},
		{memberToken, http.MethodDelete, "/api/v1/profile/trackers/trakt", "", http.StatusNoContent, ""},
		{memberToken, http.MethodGet, "/api/v1/profile/trackers", "", http.StatusOK, `{"tracker":"trakt","state":"unlinked"}`},
	} {
		rec := do(tc.token, tc.method, tc.target, tc.body)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.says) || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%s %s %s: %d %s, want %d saying %q", tc.method, tc.target, tc.body, rec.Code, rec.Body, tc.want, tc.says)
		}
	}
}
