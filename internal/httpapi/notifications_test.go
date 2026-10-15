package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// fakeNotifications is one inbox, whatever the profile, of titles it sees as cards.
type fakeNotifications struct {
	inbox   store.Inbox
	cards   []store.Card
	read    []int64
	through time.Time
}

func (f *fakeNotifications) Inbox(_ context.Context, _ uuid.UUID, ids []int64) (store.Inbox, error) {
	if ids == nil {
		return f.inbox, nil
	}
	return store.Inbox{Unread: f.inbox.Unread, Notifications: slices.DeleteFunc(slices.Clone(f.inbox.Notifications),
		func(n domain.Notification) bool { return !slices.Contains(ids, n.ID) })}, nil
}

func (f *fakeNotifications) ReadNotification(_ context.Context, _ uuid.UUID, id int64) (int, error) {
	if !slices.ContainsFunc(f.inbox.Notifications, func(n domain.Notification) bool { return n.ID == id }) {
		return 0, store.ErrNotFound
	}
	f.read = append(f.read, id)
	return 1, nil
}

func (f *fakeNotifications) ReadNotifications(_ context.Context, _ uuid.UUID, through time.Time) (int, error) {
	f.through = through
	return 0, nil
}

func (f *fakeNotifications) Cards(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]store.Card, error) {
	return slices.DeleteFunc(slices.Clone(f.cards), func(c store.Card) bool { return !slices.Contains(ids, c.ID) }), nil
}

func TestAProfileReadsItsNotifications(t *testing.T) {
	show, episode, film, gone := uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	one, five := 1, 5
	added := time.Date(2026, time.October, 15, 20, 0, 0, 0, time.UTC)
	notes := &fakeNotifications{
		inbox: store.Inbox{Unread: 2, Notifications: []domain.Notification{
			{ID: 3, Title: show, Latest: episode, New: 2, AddedAt: added},
			{ID: 2, Title: gone, Latest: gone, New: 1, AddedAt: added},
			{ID: 1, Title: film, Latest: film, New: 1, AddedAt: added},
		}},
		cards: []store.Card{
			{ID: show, Kind: domain.ItemShow, Title: "Severance"},
			{ID: episode, Kind: domain.ItemEpisode, Title: "Half Loop", SeasonNumber: &one, EpisodeNumber: &five},
			{ID: film, Kind: domain.ItemMovie, Title: "Heat"},
		},
	}
	events := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Notifications: notes, Events: events})
	call := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodGet, "/api/v1/profile/notifications", "")
	var got inboxJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("inbox: %d %s %v", rec.Code, rec.Body, err)
	}
	if got.Unread != 2 || len(got.Items) != 2 {
		t.Fatalf("inbox = %+v, want two, the title it no longer sees left out", got)
	}
	if n := got.Items[0]; n.Title.Title != "Severance" || n.Latest.ID != episode || n.Message != "2 new episodes, the latest S1 E5 · Half Loop" {
		t.Errorf("the show's notification = %+v", n)
	}
	if n := got.Items[1]; n.Title.ID != film || n.Message != "New film" {
		t.Errorf("the film's notification = %+v", n)
	}

	// An Apple device woken for one asks for it alone, worded.
	rec = call(http.MethodGet, "/api/v1/profile/notifications/3", "")
	var alone notificationJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &alone); rec.Code != http.StatusOK || err != nil || alone.ID != 3 || alone.Message == "" {
		t.Errorf("one notification: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodGet, "/api/v1/profile/notifications/2", ""); rec.Code != http.StatusNotFound {
		t.Errorf("one whose title the profile no longer sees: %d, want 404", rec.Code)
	}

	if rec := call(http.MethodPut, "/api/v1/profile/notifications/3/read", ""); rec.Code != http.StatusNoContent || !slices.Equal(notes.read, []int64{3}) {
		t.Errorf("reading one: %d, read %v", rec.Code, notes.read)
	}
	if rec := call(http.MethodPut, "/api/v1/profile/notifications/9/read", ""); rec.Code != http.StatusNotFound {
		t.Errorf("reading one that is not there: %d, want 404", rec.Code)
	}
	if rec := call(http.MethodPut, "/api/v1/profile/notifications/read", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("reading all with no through: %d, want 400", rec.Code)
	}
	if rec := call(http.MethodPut, "/api/v1/profile/notifications/read", `{"through":"2026-10-15T20:00:00Z"}`); rec.Code != http.StatusNoContent || !notes.through.Equal(added) {
		t.Errorf("reading all: %d, through %v", rec.Code, notes.through)
	}
	// Each read is told to the profile's devices: which, and how many are left unread.
	events.mu.Lock()
	raised := slices.Clone(events.raised)
	events.mu.Unlock()
	if len(raised) != 2 || raised[0].Kind != domain.EventNotificationsChanged || raised[0].Profile != oliver.ID {
		t.Fatalf("raised %+v, want notifications.changed to the profile for each read", raised)
	}
	said := words.In(language.English)
	told, ok, err := api.toldTo(t.Context(), oliver.ID, said, raised[0])
	if want := (inboxChangeJSON{Change: domain.NotificationsRead, Read: []int64{3}, Unread: 1}); err != nil || !ok || !reflect.DeepEqual(told, want) {
		t.Errorf("one read, told: %+v %v %v, want %+v", told, ok, err, want)
	}
	if told, ok, err := api.toldTo(t.Context(), oliver.ID, said, raised[1]); err != nil || !ok || told.(inboxChangeJSON).Through == nil {
		t.Errorf("all read, told: %+v %v, want the time up to which", told, err)
	}

	// Titles added are told as the notifications they made, worded, so a device asks for nothing.
	fresh := domain.Event{Kind: domain.EventNotificationsChanged, Profile: oliver.ID, Details: domain.NotificationsDetails{
		Change: domain.NotificationsAdded, IDs: []int64{3},
	}}
	told, ok, err = api.toldTo(t.Context(), oliver.ID, said, fresh)
	change, _ := told.(inboxChangeJSON)
	if err != nil || !ok || len(change.Notifications) != 1 || change.Notifications[0].Message == "" || change.Unread != 2 {
		t.Errorf("added, told: %+v %v %v, want the show's notification, worded, and 2 unread", told, ok, err)
	}
	if _, ok, err := api.toldTo(t.Context(), uuid.NewV7(), said, fresh); err != nil || ok {
		t.Error("another profile was told of the profile's notifications")
	}
}
