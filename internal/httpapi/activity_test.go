package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeEvents keeps what is raised, streams nothing, and tests the webhooks it is told of.
type fakeEvents struct {
	mu       sync.Mutex
	raised   []domain.Event
	webhooks []uuid.UUID
	stopped  []domain.JobKind
}

func (f *fakeEvents) Raise(_ context.Context, e domain.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raised = append(f.raised, e)
}

func (f *fakeEvents) Subscribe() (<-chan domain.Event, func()) {
	return make(chan domain.Event), func() {}
}

func (f *fakeEvents) Scans(context.Context) ([]domain.ScanProgress, error) { return nil, nil }

func (f *fakeEvents) Backlogs(context.Context) ([]domain.Backlog, error) { return nil, nil }

func (f *fakeEvents) BacklogStopped(_ context.Context, kind domain.JobKind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, kind)
	return nil
}

func (f *fakeEvents) TestWebhook(_ context.Context, id uuid.UUID) error {
	if !slices.Contains(f.webhooks, id) {
		return store.ErrNotFound
	}
	return nil
}

func (f *fakeEvents) kinds() []domain.EventKind {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.EventKind, len(f.raised))
	for i, e := range f.raised {
		out[i] = e.Kind
	}
	return out
}

// fakeActivity answers one sign-in, of whichever kind is asked for.
type fakeActivity struct{ asked []domain.EventKind }

func (f *fakeActivity) Activity(_ context.Context, kind domain.EventKind, _, _ int) ([]domain.Event, int64, error) {
	f.asked = append(f.asked, kind)
	return []domain.Event{{
		ID: uuid.NewV7(), Kind: domain.EventSignedIn, Profile: oliver.ID,
		Details: map[string]any{"name": oliver.Name, "device": "Living room", "client": "Photon"},
	}}, 1, nil
}

// listedProfiles lists the profiles an event is worded with.
type listedProfiles []domain.Profile

func (l listedProfiles) Profiles(context.Context) ([]store.ProfileListing, error) {
	out := make([]store.ProfileListing, len(l))
	for i, p := range l {
		out[i] = store.ProfileListing{Profile: p}
	}
	return out, nil
}

func TestAnAdminReadsTheActivityLog(t *testing.T) {
	log := &fakeActivity{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Activity: log, Events: &fakeEvents{}, Profiles: listedProfiles{oliver}, Libraries: &fakeLibraries{},
	})
	for _, tc := range []struct {
		token, target string
		want          int
		body          string
	}{
		{memberToken, "/api/v1/admin/activity", http.StatusForbidden, ""},
		{memberToken, "/api/v1/admin/events", http.StatusForbidden, ""},
		{goodToken, "/api/v1/admin/activity", http.StatusOK, `"kind":"auth.signed_in"`},
		// An admin reads what happened as a sentence the server words.
		{goodToken, "/api/v1/admin/activity", http.StatusOK, `"text":"Oliver signed in on Living room (Photon)"`},
		{goodToken, "/api/v1/admin/activity?kind=auth.signed_in&offset=0&limit=10", http.StatusOK, `"total":1`},
		{goodToken, "/api/v1/admin/activity?kind=job.started", http.StatusBadRequest, "is not one of"},
		{goodToken, "/api/v1/admin/activity?limit=0", http.StatusBadRequest, ""},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.body) {
			t.Errorf("%s: %d %s, want %d with %s", tc.target, rec.Code, rec.Body, tc.want, tc.body)
		}
	}
	if len(log.asked) != 3 || log.asked[0] != "" || log.asked[2] != domain.EventSignedIn {
		t.Errorf("asked for %q, want every kind then sign-ins", log.asked)
	}
}

func TestSignInsAreTold(t *testing.T) {
	told := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Limits: &fakeLimiter{}, Events: told})
	for _, password := range []string{"correct horse", "guess"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"method":"password","name":"Oliver","password":"`+password+`","device":"Living room","client":"Photon"}`))
		req.RemoteAddr = "203.0.113.9:5000"
		api.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(told.raised) != 2 {
		t.Fatalf("told %v, want a sign-in and a refusal", told.kinds())
	}
	in, refused := told.raised[0], told.raised[1]
	if in.Kind != domain.EventSignedIn || in.Profile != oliver.ID || in.Details["address"] != "203.0.113.9" || in.Details["device"] != "Living room" {
		t.Errorf("sign-in told as %+v", in)
	}
	if refused.Kind != domain.EventSignInRefused || refused.Details["name"] != "Oliver" {
		t.Errorf("refusal told as %+v", refused)
	}
	for _, e := range told.raised {
		for _, v := range e.Details {
			if v == "guess" || v == "correct horse" {
				t.Errorf("%s carries the password", e.Kind)
			}
		}
	}
}

// streamingEvents streams events, and says when the stream lets go of its subscription.
type streamingEvents struct {
	*fakeEvents
	events chan domain.Event
	gone   chan struct{}
}

func (f streamingEvents) Subscribe() (<-chan domain.Event, func()) {
	return f.events, func() { close(f.gone) }
}

// A client that stops reading but keeps its connection does not hold its stream for good.
func TestAStreamNobodyReadsIsGivenUp(t *testing.T) {
	t.Parallel()
	work := &fakeWork{}
	told := streamingEvents{fakeEvents: &fakeEvents{}, events: make(chan domain.Event), gone: make(chan struct{})}
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Tasks: work, Jobs: work, NowPlaying: work, Events: told, Profiles: listedProfiles{oliver}, Libraries: &fakeLibraries{},
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET /api/v1/admin/events HTTP/1.1\r\nHost: photon\r\nAuthorization: Bearer %s\r\n\r\n", goodToken); err != nil {
		t.Fatal(err)
	}
	big := domain.Event{Kind: domain.EventScanProgress, Details: map[string]any{"padding": strings.Repeat("x", 64<<10)}}
	deadline := time.After(3 * sendWithin)
	for {
		select {
		case told.events <- big:
		case <-told.gone:
			return
		case <-deadline:
			t.Fatal("the stream is still held by a client that stopped reading")
		}
	}
}
