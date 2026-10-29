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

// notificationJSON is a film added, or a show's episodes added while it was unread, or a request
// waiting for the profile to decide it. A client drops a kind it does not know.
type notificationJSON struct {
	ID   int64                   `json:"id"`
	Kind domain.NotificationKind `json:"kind"`
	// Title is a title's film or show, Latest the film or the newest of the episodes it brings.
	Title  cardJSON `json:"title,omitzero"`
	Latest cardJSON `json:"latest,omitzero"`
	New    int      `json:"new,omitzero"`
	// Request is a request's, as the requests one keeps list it.
	Request requestJSON `json:"request,omitzero"`
	// Message says what it brings, beside the title's name, or who asked for a request.
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
	// Cleared are the ids cleared, or Through the added_at up to which every one was read, or
	// cleared.
	Cleared []int64    `json:"cleared,omitzero"`
	Through *time.Time `json:"through,omitzero"`
	Unread  int        `json:"unread"`
}

// inboxMarkerJSON is how far the profile has read its notifications, or cleared them.
type inboxMarkerJSON struct {
	// Through is the added_at of the newest notification shown; any added since stays as it is.
	Through time.Time `json:"through"`
}

// notificationsOf words an inbox's notifications for a reader, a title's as cards it may see; one
// whose title it no longer sees, or whose request has gone, is left out.
func (a *API) notificationsOf(ctx context.Context, profile uuid.UUID, said words.Words, in store.Inbox) ([]notificationJSON, error) {
	ids := make([]uuid.UUID, 0, 2*len(in.Notifications))
	for _, n := range in.Notifications {
		if n.Kind == domain.NotificationTitle {
			ids = append(ids, n.Title, n.Latest)
		}
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
		shown := notificationJSON{ID: n.ID, Kind: n.Kind, AddedAt: n.AddedAt, ReadAt: n.ReadAt}
		switch n.Kind {
		case domain.NotificationTitle:
			title, ok := byID[n.Title]
			latest, seen := byID[n.Latest]
			if !ok || !seen {
				continue
			}
			shown.Title, shown.Latest, shown.New, shown.Message = cardOf(title), cardOf(latest), n.New, said.Notification(n.New, latest)
		case domain.NotificationRequest:
			r, ok := in.Requests[n.Request]
			if !ok {
				continue
			}
			shown.Request, shown.Message = requestOf(r), said.AskedFor(r.ProfileName)
		}
		out = append(out, shown)
	}
	return out, nil
}

// inbox answers the profile's notifications of the films and episodes added that it follows, and of
// the requests waiting for it to decide them.
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

// notification answers one of the profile's notifications, as a device woken for it by its id asks
// to show it.
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
// for the reader, or which were read or cleared.
func (a *API) inboxChange(ctx context.Context, profile uuid.UUID, said words.Words, d domain.NotificationsDetails) (inboxChangeJSON, error) {
	out := inboxChangeJSON{Change: d.Change, Unread: d.Unread}
	if !d.Through.IsZero() {
		out.Through = &d.Through
	}
	switch d.Change {
	case domain.NotificationsAdded:
		in, err := a.svc.Notifications.Inbox(ctx, profile, d.IDs)
		if err != nil {
			return out, err
		}
		out.Unread = in.Unread
		out.Notifications, err = a.notificationsOf(ctx, profile, said, in)
		return out, err
	case domain.NotificationsCleared:
		out.Cleared = d.IDs
	case domain.NotificationsRead:
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

// marked moves a profile's marker on its notifications, up to the newest it was shown: read, or
// cleared from its inbox, on each of its devices.
func (a *API) marked(change domain.NotificationChange, mark func(notifications, context.Context, uuid.UUID, time.Time) (int, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req inboxMarkerJSON
		if !a.decode(w, r, &req) {
			return
		}
		if req.Through.IsZero() {
			writeProblem(w, a.logger, codeInvalidBody, "through is the added_at of the newest notification shown")
			return
		}
		profile := auth.SessionOf(r.Context()).Profile.ID
		unread, err := mark(a.svc.Notifications, r.Context(), profile, req.Through)
		if a.answered(w, r, err) {
			return
		}
		a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventNotificationsChanged, Profile: profile, Details: domain.NotificationsDetails{
			Change: change, Through: req.Through, Unread: unread,
		}})
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) notificationsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/profile/notifications", access: signedIn,
			summary: "List the profile's notifications of the films and episodes added that it follows, and of the requests waiting for it to decide them, newest first",
			status:  http.StatusOK, reply: inboxJSON{}, handle: a.inbox,
		},
		{
			pattern: "GET /api/v1/profile/notifications/{id}", access: signedIn,
			summary: "One of the profile's notifications, as a device woken for it shows it",
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
			body:    inboxMarkerJSON{}, status: http.StatusNoContent, handle: a.marked(domain.NotificationsRead, notifications.ReadNotifications),
		},
		{
			pattern: "PUT /api/v1/profile/notifications/cleared", access: signedIn,
			summary: "Clear the profile's notifications from its inbox, up to the newest it was shown; what they told of is not told again",
			body:    inboxMarkerJSON{}, status: http.StatusNoContent, handle: a.marked(domain.NotificationsCleared, notifications.ClearNotifications),
		},
	}
}
