package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var inception = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000a1")

// fakeRequests has Inception found for asking, already asked for once, and keeps who decided what,
// over whose profiles. Asking tells Oliver and Sam, who decide it, and deciding clears theirs.
type fakeRequests struct {
	by      *uuid.UUID
	decider uuid.UUID
	reason  string
}

func (*fakeRequests) AskFor(_ context.Context, profile, discovery uuid.UUID, seasons []int) (store.Request, []store.Told, error) {
	if discovery != inception {
		return store.Request{}, nil, store.ErrNotFound
	}
	if len(seasons) > 0 {
		return store.Request{}, nil, store.ErrFilmSeasons
	}
	told := []store.Told{{ProfileID: oliver.ID, IDs: []int64{7}}, {ProfileID: sam.ID, IDs: []int64{8}}}
	return store.Request{ID: uuid.NewV7(), Profile: profile, Kind: domain.ItemMovie, Title: "Inception", Provider: domain.ProviderTMDB, Value: "27205", State: domain.RequestPending}, told, nil
}

func (*fakeRequests) Requests(context.Context, uuid.UUID, store.Page) ([]store.Request, store.Extent, error) {
	return nil, store.Extent{}, nil
}

func (*fakeRequests) RequestsKept(context.Context, *uuid.UUID, domain.RequestState, store.Page) ([]store.Request, store.Extent, error) {
	return nil, store.Extent{}, nil
}

func (*fakeRequests) WithdrawRequest(context.Context, uuid.UUID, uuid.UUID) ([]store.Cleared, error) {
	return nil, store.ErrDecided
}

func (f *fakeRequests) DecideRequest(_ context.Context, id uuid.UUID, by *uuid.UUID, decider uuid.UUID, state domain.RequestState, reason string) (store.Request, []store.Cleared, error) {
	f.by, f.decider, f.reason = by, decider, reason
	cleared := []store.Cleared{{ProfileID: oliver.ID, IDs: []int64{7}, Unread: 2}, {ProfileID: sam.ID, IDs: []int64{8}}}
	return store.Request{ID: id, State: state, Reason: reason, DecidedBy: decider}, cleared, nil
}

// A profile asks for a title by the id a search answered it as requestable under; a film asked for
// by seasons, or a request decided, is refused. A request is decided by an admin over every
// profile, or by a manager over the users it keeps, and a user decides none. Those who may decide
// one are told it waits, each on its own devices, and that it no longer does once decided.
func TestARequestIsAskedAndDecidedByWhoMay(t *testing.T) {
	f := &fakeRequests{}
	events := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Auth: fakeAuth{}, Requests: f, Events: events})
	raised := func() []domain.Event {
		events.mu.Lock()
		defer events.mu.Unlock()
		out := events.raised
		events.raised = nil
		return out
	}
	do := func(method, target, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodPost, "/api/v1/requests", memberToken, `{"requestable_id":"`+inception.String()+`"}`)
	var asked requestJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &asked); rec.Code != http.StatusCreated || err != nil ||
		asked.Title != "Inception" || asked.IDs[domain.ProviderTMDB] != "27205" || asked.State != domain.RequestPending {
		t.Errorf("asked: %d %s", rec.Code, rec.Body)
	}
	want := []domain.Event{
		{Kind: domain.EventNotificationsChanged, Profile: oliver.ID, Details: domain.NotificationsDetails{Change: domain.NotificationsAdded, IDs: []int64{7}}},
		{Kind: domain.EventNotificationsChanged, Profile: sam.ID, Details: domain.NotificationsDetails{Change: domain.NotificationsAdded, IDs: []int64{8}}},
	}
	if got := raised(); !reflect.DeepEqual(got, want) {
		t.Errorf("asked, raised %+v; want each who decides it told of its notification", got)
	}
	for body, want := range map[string]int{
		`{"requestable_id":"` + uuid.NewV7().String() + `"}`:             http.StatusNotFound,
		`{"requestable_id":"` + inception.String() + `","seasons":[1]}`:  http.StatusBadRequest,
		`{"requestable_id":"` + inception.String() + `","seasons":[-1]}`: http.StatusBadRequest,
		`{}`: http.StatusBadRequest,
	} {
		if rec := do(http.MethodPost, "/api/v1/requests", memberToken, body); rec.Code != want {
			t.Errorf("asked %s: %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
	if rec := do(http.MethodDelete, "/api/v1/requests/"+uuid.NewV7().String(), memberToken, ""); rec.Code != http.StatusConflict {
		t.Errorf("a decided request taken back: %d, want 409", rec.Code)
	}

	id := uuid.NewV7().String()
	if rec := do(http.MethodPost, "/api/v1/admin/requests/"+id+"/approve", memberToken, ""); rec.Code != http.StatusForbidden {
		t.Errorf("approved by a user: %d, want 403", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/v1/admin/requests/"+id+"/approve", goodToken, ""); rec.Code != http.StatusOK || f.by != nil || f.decider != oliver.ID {
		t.Errorf("approved by an admin: %d, over %v as %v; want every profile's, as Oliver", rec.Code, f.by, f.decider)
	}
	want = []domain.Event{
		{Kind: domain.EventNotificationsChanged, Profile: oliver.ID, Details: domain.NotificationsDetails{Change: domain.NotificationsCleared, IDs: []int64{7}, Unread: 2}},
		{Kind: domain.EventNotificationsChanged, Profile: sam.ID, Details: domain.NotificationsDetails{Change: domain.NotificationsCleared, IDs: []int64{8}}},
	}
	if got := raised(); !reflect.DeepEqual(got, want) {
		t.Errorf("approved, raised %+v; want each who was told it waits told it is cleared", got)
	}
	rec = do(http.MethodPost, "/api/v1/admin/requests/"+id+"/decline", managerToken, `{"reason":"Not this week"}`)
	if rec.Code != http.StatusOK || f.by == nil || *f.by != sam.ID || f.decider != sam.ID || f.reason != "Not this week" {
		t.Errorf("declined by a manager: %d, over %v as %v for %q; want Sam's users, as Sam, saying why", rec.Code, f.by, f.decider, f.reason)
	}
	if rec := do(http.MethodGet, "/api/v1/admin/requests?state=waiting", goodToken, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("requests in no state: %d, want 400", rec.Code)
	}
}

// fakeRequestablePages has Inception found for asking.
type fakeRequestablePages struct{}

func (fakeRequestablePages) Page(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	if id != inception {
		return store.TitlePage{}, store.ErrNotFound
	}
	return store.TitlePage{ID: id, Kind: domain.ItemMovie, Title: "Inception"}, nil
}

// A title a search found to ask for opens at its id as one a library holds does, marked for asking;
// an id neither is is not found.
func TestATitleToAskForOpensAsATitle(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		RemoteVersions: noRemoteVersions{}, Auth: fakeAuth{}, Catalogue: fakeCatalogue{}, RequestablePages: fakeRequestablePages{},
	})
	get := func(id uuid.UUID) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/titles/"+id.String(), nil)
		req.Header.Set("Authorization", "Bearer "+memberToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := get(films); code != http.StatusOK || !strings.Contains(body, `"title":"Heat"`) || strings.Contains(body, `"requestable"`) {
		t.Errorf("a title held: %d %s, want Heat, not to ask for", code, body)
	}
	if code, body := get(inception); code != http.StatusOK || !strings.Contains(body, `"title":"Inception"`) || !strings.Contains(body, `"requestable":true`) {
		t.Errorf("a title to ask for: %d %s, want Inception, to ask for", code, body)
	}
	if code, body := get(uuid.NewV7()); code != http.StatusNotFound || !strings.Contains(body, `"code":"not_found"`) {
		t.Errorf("an id of neither: %d %s, want not found", code, body)
	}
}
