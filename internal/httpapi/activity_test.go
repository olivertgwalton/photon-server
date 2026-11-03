package httpapi

import (
	"cmp"
	"context"
	"encoding/json/v2"
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

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/websocket/websockettest"
)

// fakeEvents keeps what is raised, streams nothing, and tests the webhooks it is told of.
type fakeEvents struct {
	mu       sync.Mutex
	raised   []domain.Event
	webhooks []uuid.UUID
	stopped  []domain.JobKind
	// changed are the titles told as changed outside a scan, by their library.
	changed map[uuid.UUID]store.Changed
}

func (f *fakeEvents) Raise(_ context.Context, e domain.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raised = append(f.raised, e)
}

func (f *fakeEvents) Changed(_ context.Context, lib uuid.UUID, titles store.Changed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.changed == nil {
		f.changed = map[uuid.UUID]store.Changed{}
	}
	if f.changed[lib] == nil {
		f.changed[lib] = store.Changed{}
	}
	for change, ids := range titles {
		f.changed[lib][change] = append(f.changed[lib][change], ids...)
	}
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

func (f *fakeActivity) Activity(_ context.Context, kind domain.EventKind, _ store.Before, _ int) ([]domain.Event, bool, error) {
	f.asked = append(f.asked, kind)
	return []domain.Event{{
		ID: uuid.NewV7(), Kind: domain.EventSignedIn, Profile: oliver.ID,
		Details: domain.SignInDetails{Name: oliver.Name, Device: "Living room", Client: "Photon"},
	}}, false, nil
}

// listedProfiles lists the profiles an event is worded with.
type listedProfiles []domain.Profile

func (l listedProfiles) HasProfiles(context.Context) (bool, error) { return len(l) > 0, nil }

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
		{memberToken, "/api/v1/events?feed=server", http.StatusForbidden, ""},
		{goodToken, "/api/v1/admin/activity", http.StatusOK, `"kind":"auth.signed_in"`},
		// An admin reads what happened as a sentence the server words.
		{goodToken, "/api/v1/admin/activity", http.StatusOK, `"text":"Oliver signed in on Living room (Photon)"`},
		{goodToken, "/api/v1/admin/activity?kind=auth.signed_in&limit=10", http.StatusOK, `"kind":"auth.signed_in"`},
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
	if d, _ := in.Details.(domain.SignInDetails); in.Kind != domain.EventSignedIn || in.Profile != oliver.ID || d.Address != "203.0.113.9" || d.Device != "Living room" {
		t.Errorf("sign-in told as %+v", in)
	}
	if d, _ := refused.Details.(domain.SignInDetails); refused.Kind != domain.EventSignInRefused || d.Name != "Oliver" {
		t.Errorf("refusal told as %+v", refused)
	}
	for _, e := range told.raised {
		said, err := json.Marshal(e.Details)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(said), "guess") || strings.Contains(string(said), "correct horse") {
			t.Errorf("%s carries the password", e.Kind)
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

// A client that stops reading but keeps its connection does not hold its socket for good.
func TestASocketNobodyReadsIsGivenUp(t *testing.T) {
	t.Parallel()
	work := &fakeWork{}
	told := streamingEvents{fakeEvents: &fakeEvents{}, events: make(chan domain.Event), gone: make(chan struct{})}
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{},
		Auth: fakeAuth{}, Tasks: work, Jobs: work, NowPlaying: work, Events: told, Profiles: listedProfiles{oliver}, Libraries: &fakeLibraries{},
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/events?feed=server", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+goodToken)
	c, res, err := websockettest.Open(conn, req)
	if c == nil {
		t.Fatalf("handshake: %v %v", res, err)
	}
	res.Body.Close()
	big := domain.Event{Kind: domain.EventTaskFailed, Details: domain.TaskDetails{Task: domain.TaskScanLibraries, Error: strings.Repeat("x", 64<<10)}}
	deadline := time.After(time.Minute)
	for {
		select {
		case told.events <- big:
		case <-told.gone:
			return
		case <-deadline:
			t.Fatal("the socket is still held by a client that stopped reading")
		}
	}
}

// signedInDevices keeps each token's session until it is signed out, on a profile an admin may
// change, as the store does.
type signedInDevices struct {
	fakeAuth
	mu       sync.Mutex
	sessions map[string]domain.Session
}

func (d *signedInDevices) Authenticate(_ context.Context, token string) (domain.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.sessions[token]
	if !ok {
		return domain.Session{}, auth.ErrUnauthenticated
	}
	return s, nil
}

func (d *signedInDevices) Session(_ context.Context, id uuid.UUID) (domain.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return domain.Session{}, auth.ErrUnauthenticated
}

func (d *signedInDevices) SignOutDevice(_ context.Context, _ domain.Session, device uuid.UUID) (uuid.UUID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for token, s := range d.sessions {
		if s.ID == device {
			delete(d.sessions, token)
			return s.Profile.ID, nil
		}
	}
	return uuid.UUID{}, auth.ErrDeviceNotFound
}

// household changes the role of a profile its devices are signed in to.
type household struct {
	*fakeProfiles
	devices *signedInDevices
}

func (h household) SetProfile(_ context.Context, id uuid.UUID, c store.ProfileChange, _ *uuid.UUID) (domain.Profile, error) {
	h.devices.mu.Lock()
	defer h.devices.mu.Unlock()
	var out domain.Profile
	for token, s := range h.devices.sessions {
		if s.Profile.ID == id {
			s.Profile.Role = cmp.Or(c.Role, s.Profile.Role)
			h.devices.sessions[token], out = s, s.Profile
		}
	}
	if out.ID == (uuid.UUID{}) {
		return domain.Profile{}, store.ErrNotFound
	}
	return out, nil
}

// relayedEvents tells each stream what is raised, as one node's hub does.
type relayedEvents struct {
	*fakeEvents
	mu      sync.Mutex
	streams map[chan domain.Event]struct{}
}

func (f *relayedEvents) Raise(ctx context.Context, e domain.Event) {
	f.fakeEvents.Raise(ctx, e)
	f.mu.Lock()
	defer f.mu.Unlock()
	for s := range f.streams {
		s <- e
	}
}

func (f *relayedEvents) Subscribe() (<-chan domain.Event, func()) {
	s := make(chan domain.Event, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streams[s] = struct{}{}
	return s, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.streams, s)
	}
}

// socketClosed opens the events socket at target as token, answering a channel closed once the
// server closes it.
func socketClosed(t *testing.T, target, token string) <-chan struct{} {
	t.Helper()
	c, res, err := websockettest.Dial(t.Context(), target, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil || c == nil {
		t.Fatalf("%s as %s: %v %v", target, token, res, err)
	}
	res.Body.Close()
	t.Cleanup(func() { c.Close() })
	if err := c.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			head, _, err := c.Next()
			if err != nil || head == websockettest.Fin|websockettest.OpClose {
				return
			}
		}
	}()
	return closed
}

func sessionsAPI(t *testing.T, devices *signedInDevices) string {
	t.Helper()
	work := &fakeWork{}
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{},
		Auth: devices, ProfileAdmin: household{&fakeProfiles{hashes: map[string]string{}}, devices},
		Tasks: work, Jobs: work, NowPlaying: work, Profiles: listedProfiles{oliver}, Libraries: &fakeLibraries{},
		Events: &relayedEvents{fakeEvents: &fakeEvents{}, streams: map[chan domain.Event]struct{}{}},
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func askAs(t *testing.T, method, target, token, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		t.Fatalf("%s %s: %d", method, target, res.StatusCode)
	}
}

func waitClosed(t *testing.T, closed <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s is still open", what)
	}
}

// stillOpen fails if a socket closed by the time another, told the same, has.
func stillOpen(t *testing.T, closed <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-closed:
		t.Errorf("%s closed", what)
	case <-time.After(100 * time.Millisecond):
	}
}

// A device signed out stops being told anything, as Jellyfin closes a session's sockets as it ends.
func TestASocketClosesWhenItsDeviceIsSignedOut(t *testing.T) {
	t.Parallel()
	tv, phone := uuid.NewV7(), uuid.NewV7()
	url := sessionsAPI(t, &signedInDevices{sessions: map[string]domain.Session{
		goodToken:   {ID: uuid.NewV7(), Profile: oliver},
		"pst_tv":    {ID: tv, Profile: sam},
		"pst_phone": {ID: phone, Profile: sam},
	}})
	tvClosed := socketClosed(t, url+"/api/v1/events", "pst_tv")
	phoneClosed := socketClosed(t, url+"/api/v1/events", "pst_phone")
	askAs(t, http.MethodDelete, url+"/api/v1/devices/"+tv.String(), goodToken, "")
	waitClosed(t, tvClosed, "the signed-out TV's socket")
	stillOpen(t, phoneClosed, "the phone's socket, still signed in,")
}

// An admin made a user stops being told what happens on every node, but not what its profile sees.
func TestTheServerFeedClosesWhenItsAdminIsDemoted(t *testing.T) {
	t.Parallel()
	ada := domain.Profile{ID: uuid.NewV7(), Name: "Ada", Role: domain.RoleAdmin}
	url := sessionsAPI(t, &signedInDevices{sessions: map[string]domain.Session{
		goodToken: {ID: uuid.NewV7(), Profile: oliver},
		"pst_ada": {ID: uuid.NewV7(), Profile: ada},
	}})
	serverClosed := socketClosed(t, url+"/api/v1/events?feed=server", "pst_ada")
	profileClosed := socketClosed(t, url+"/api/v1/events", "pst_ada")
	askAs(t, http.MethodPatch, url+"/api/v1/admin/profiles/"+ada.ID.String(), goodToken, `{"role": "user"}`)
	waitClosed(t, serverClosed, "the demoted admin's server feed")
	stillOpen(t, profileClosed, "the demoted admin's profile feed")
}
