package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

type notifications interface {
	Inbox(ctx context.Context, profile uuid.UUID, ids []int64) (store.Inbox, error)
	ClearNotification(ctx context.Context, profile uuid.UUID, id int64) (int, error)
	ReadNotifications(ctx context.Context, profile uuid.UUID, through time.Time) (int, error)
	ClearNotifications(ctx context.Context, profile uuid.UUID, through time.Time) (int, error)
	Cards(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]store.Card, error)
}

// notificationJSON is a film added, or a show's episodes added while it was unread.
type notificationJSON struct {
	ID int64 `json:"id"`
	// Title is the film or the show, Latest the film or the newest of the episodes it brings.
	Title  cardJSON `json:"title"`
	Latest cardJSON `json:"latest"`
	New    int      `json:"new"`
	// Message says what it brings, beside the title's name.
	Message string `json:"message"`
	// AddedAt is when its newest was added.
	AddedAt time.Time  `json:"added_at"`
	ReadAt  *time.Time `json:"read_at,omitzero"`
}

type inboxJSON struct {
	// Items are the newest notifications, newest first.
	Items  []notificationJSON `json:"items"`
	Unread int                `json:"unread"`
}

// inboxChangeJSON is what the profile feed tells of its notifications changed, on any device, for
// each of its devices to change those alone.
type inboxChangeJSON struct {
	Change domain.NotificationChange `json:"change"`
	// Notifications are those added, or gathering more, as the inbox lists them: each replaces
	// the one of its id, at the top.
	Notifications []notificationJSON `json:"notifications,omitzero"`
	// Read are the ids read and Cleared those cleared, or Through the added_at up to which every
	// one was read, or cleared.
	Read    []int64    `json:"read,omitzero"`
	Cleared []int64    `json:"cleared,omitzero"`
	Through *time.Time `json:"through,omitzero"`
	Unread  int        `json:"unread"`
}

// readMarkerJSON is how far the profile has read its notifications.
type readMarkerJSON struct {
	// Through is the added_at of the newest notification read; any added since stays unread.
	Through time.Time `json:"through"`
}

// notificationsOf words an inbox's notifications for a reader, as cards it may see; one whose
// title it no longer sees is left out.
func (a *API) notificationsOf(ctx context.Context, profile uuid.UUID, said words.Words, in store.Inbox) ([]notificationJSON, error) {
	ids := make([]uuid.UUID, 0, 2*len(in.Notifications))
	for _, n := range in.Notifications {
		ids = append(ids, n.Title, n.Latest)
	}
	cards, err := a.svc.Notifications.Cards(ctx, profile, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]store.Card, len(cards))
	for _, c := range cards {
		byID[c.ID] = c
	}
	out := []notificationJSON{}
	for _, n := range in.Notifications {
		title, ok := byID[n.Title]
		latest, seen := byID[n.Latest]
		if !ok || !seen {
			continue
		}
		out = append(out, notificationJSON{
			ID: n.ID, Title: cardOf(title), Latest: cardOf(latest), New: n.New, Message: said.Notification(n.New, latest),
			AddedAt: n.AddedAt, ReadAt: n.ReadAt,
		})
	}
	return out, nil
}

// inbox answers the profile's notifications of the films and episodes added that it follows.
func (a *API) inbox(w http.ResponseWriter, r *http.Request) {
	ctx, profile := r.Context(), auth.SessionOf(r.Context()).Profile.ID
	in, err := a.svc.Notifications.Inbox(ctx, profile, nil)
	if a.answered(w, r, err) {
		return
	}
	shown, err := a.notificationsOf(ctx, profile, words.Negotiate(w, r), in)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, inboxJSON{Items: shown, Unread: in.Unread})
	}
}

// notification answers one of the profile's notifications, as an Apple device woken for it by its
// id asks to show it.
func (a *API) notification(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathNumber(w, r, "id")
	if !ok {
		return
	}
	ctx, profile := r.Context(), auth.SessionOf(r.Context()).Profile.ID
	in, err := a.svc.Notifications.Inbox(ctx, profile, []int64{int64(id)})
	if a.answered(w, r, err) {
		return
	}
	shown, err := a.notificationsOf(ctx, profile, words.Negotiate(w, r), in)
	switch {
	case a.answered(w, r, err):
	case len(shown) == 0:
		writeProblem(w, a.logger, codeNotFound, "no such notification")
	default:
		writeJSON(w, a.logger, "application/json", http.StatusOK, shown[0])
	}
}

// inboxChange is a change of a profile's notifications as its feed tells it: those added, worded
// for the reader, or which were read.
func (a *API) inboxChange(ctx context.Context, profile uuid.UUID, said words.Words, d domain.NotificationsDetails) (inboxChangeJSON, error) {
	out := inboxChangeJSON{Change: d.Change, Unread: d.Unread}
	switch d.Change {
	case domain.NotificationsAdded:
		in, err := a.svc.Notifications.Inbox(ctx, profile, d.IDs)
		if err != nil {
			return out, err
		}
		out.Unread = in.Unread
		out.Notifications, err = a.notificationsOf(ctx, profile, said, in)
		return out, err
	case domain.NotificationsRead:
		out.Read = d.IDs
		if !d.Through.IsZero() {
			out.Through = &d.Through
		}
	case domain.NotificationsCleared:
		out.Cleared = d.IDs
		if !d.Through.IsZero() {
			out.Through = &d.Through
		}
	}
	return out, nil
}

// clearNotification clears one of the profile's notifications as it is opened, on each of its
// devices.
func (a *API) clearNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathNumber(w, r, "id")
	if !ok {
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	unread, err := a.svc.Notifications.ClearNotification(r.Context(), profile, int64(id))
	if a.answeredAs(w, r, err, "no such notification") {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNotificationsChanged, Profile: profile, Details: domain.NotificationsDetails{
		Change: domain.NotificationsCleared, IDs: []int64{int64(id)}, Unread: unread,
	}})
	w.WriteHeader(http.StatusNoContent)
}

// readNotifications marks the profile's notifications read, up to the newest it was shown.
func (a *API) readNotifications(w http.ResponseWriter, r *http.Request) {
	var req readMarkerJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Through.IsZero() {
		writeProblem(w, a.logger, codeInvalidBody, "through is the added_at of the newest notification read")
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	unread, err := a.svc.Notifications.ReadNotifications(r.Context(), profile, req.Through)
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNotificationsChanged, Profile: profile, Details: domain.NotificationsDetails{
		Change: domain.NotificationsRead, Through: req.Through, Unread: unread,
	}})
	w.WriteHeader(http.StatusNoContent)
}

// clearNotifications clears the profile's notifications from its inbox, up to the newest it was
// shown, on each of its devices.
func (a *API) clearNotifications(w http.ResponseWriter, r *http.Request) {
	through, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("through"))
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, "through is the added_at of the newest notification shown")
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	unread, err := a.svc.Notifications.ClearNotifications(r.Context(), profile, through)
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNotificationsChanged, Profile: profile, Details: domain.NotificationsDetails{
		Change: domain.NotificationsCleared, Through: through, Unread: unread,
	}})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) notificationsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/profile/notifications", access: signedIn,
			summary: "List the profile's notifications of the films and episodes added that it follows, newest first",
			status:  http.StatusOK, reply: inboxJSON{}, handle: a.inbox,
		},
		{
			pattern: "DELETE /api/v1/profile/notifications", access: signedIn,
			summary: "Clear the profile's notifications from its inbox, up to the newest it was shown; what they told of is not told again",
			query:   []param{{"through", time.Time{}, "The added_at of the newest notification shown; any added since stays."}},
			status:  http.StatusNoContent, handle: a.clearNotifications,
		},
		{
			pattern: "GET /api/v1/profile/notifications/{id}", access: signedIn,
			summary: "One of the profile's notifications, as an Apple device woken for it shows it",
			path:    []param{{"id", int64(0), "The notification."}},
			status:  http.StatusOK, reply: notificationJSON{},
			handle: a.notification,
		},
		{
			pattern: "DELETE /api/v1/profile/notifications/{id}", access: signedIn,
			summary: "Clear one of the profile's notifications from its inbox, read, as it is opened",
			path:    []param{{"id", int64(0), "The notification."}},
			status:  http.StatusNoContent, handle: a.clearNotification,
		},
		{
			pattern: "PUT /api/v1/profile/notifications/read", access: signedIn,
			summary: "Mark the profile's notifications read, up to the newest it was shown",
			body:    readMarkerJSON{}, status: http.StatusNoContent, handle: a.readNotifications,
		},
	}
}
