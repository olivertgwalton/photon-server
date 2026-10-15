// Package push is the job that pushes a profile's notifications to the devices subscribed for it.
package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/pushrelay"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/webpush"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// web sends a message to a browser's push service.
type web interface {
	Send(ctx context.Context, sub webpush.Subscription, m webpush.Message) error
}

// apple wakes an Apple device, through photon's push relay.
type apple interface {
	Send(ctx context.Context, m pushrelay.Message) error
}

// pushTTL is how long a push service keeps a notification for a browser that is away: a day on, a
// new episode is no longer news to be woken for, and the inbox has it.
const pushTTL = 24 * time.Hour

// message is a Declarative Web Push message: a browser that reads them shows its notification
// itself, and one that does not hands it to the service worker, which shows the same.
type message struct {
	// WebPush is always declarativePush, which marks the message declarative.
	WebPush      int          `json:"web_push"`
	Notification notification `json:"notification"`
}

const declarativePush = 8030

type notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Lang  string `json:"lang"`
	// Tag is the title the notification is of: a browser replaces the one it shows of the same.
	Tag string `json:"tag"`
	// Navigate is the page a click opens, against the web app's own address, naming the
	// notification so the page clears it.
	Navigate string `json:"navigate"`
}

// Job is the job that pushes a profile's notifications not yet pushed to each device subscribed for
// it: a browser in its language, an Apple device by the notification's id, which it asks the server
// to word. A device that ended its subscription is forgotten; one that fails has every device sent
// again later, where each replaces what it shows of the same title.
func Job(st *store.Store, browsers web, devices apple) jobs.Handler {
	return func(ctx context.Context, profile uuid.UUID) error {
		pushes, err := st.Pushes(ctx, profile)
		if err != nil || len(pushes) == 0 {
			return err
		}
		subs, err := st.PushSubscriptions(ctx, profile)
		if err != nil {
			return err
		}
		var ids, items []uuid.UUID
		for _, p := range pushes {
			ids = append(ids, p.Title, p.Latest)
			items = append(items, p.Items...)
		}
		cards, err := st.Cards(ctx, profile, ids)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]store.Card, len(cards))
		for _, c := range cards {
			byID[c.ID] = c
		}
		var failed []error
		for _, sub := range subs {
			var err error
			switch sub.Kind {
			case domain.PushWeb:
				err = sendWeb(ctx, browsers, sub, pushes, byID)
			case domain.PushApple:
				err = sendApple(ctx, devices, sub, pushes, byID)
			}
			if errors.Is(err, webpush.ErrGone) || errors.Is(err, pushrelay.ErrGone) {
				// Unsubscribed on another device meanwhile is as good.
				if err = st.Unsubscribe(ctx, profile, sub.ID); errors.Is(err, store.ErrNotFound) {
					err = nil
				}
			}
			failed = append(failed, err)
		}
		if err := errors.Join(failed...); err != nil {
			return err
		}
		return st.Pushed(ctx, profile, items)
	}
}

// sendWeb sends a browser the notifications unread that its profile still may see.
func sendWeb(ctx context.Context, browsers web, sub store.PushSubscription, pushes []store.Push, cards map[uuid.UUID]store.Card) error {
	said := words.In(language.Make(sub.Language))
	to := webpush.Subscription{Endpoint: sub.Endpoint, P256DH: sub.P256DH, Auth: sub.Auth}
	for _, p := range pushes {
		title, ok := cards[p.Title]
		latest, seen := cards[p.Latest]
		if p.ReadAt != nil || !ok || !seen {
			continue
		}
		payload, err := json.Marshal(message{WebPush: declarativePush, Notification: notification{
			Title: title.Title, Body: said.Notification(p.New, latest), Lang: sub.Language,
			Tag: p.Title.String(), Navigate: "/titles/" + p.Title.String() + "?notification=" + strconv.FormatInt(p.ID, 10),
		}})
		if err != nil {
			return err
		}
		m := webpush.Message{Payload: payload, Topic: base64.RawURLEncoding.EncodeToString(p.Title[:]), TTL: pushTTL}
		if err := browsers.Send(ctx, to, m); err != nil {
			return err
		}
	}
	return nil
}

// sendApple wakes an Apple device for each notification unread that its profile still may see; the
// device asks the server for each by its id, and shows what it is told.
func sendApple(ctx context.Context, devices apple, sub store.PushSubscription, pushes []store.Push, cards map[uuid.UUID]store.Card) error {
	if sub.ApnsEnvironment == nil {
		return nil
	}
	for _, p := range pushes {
		_, ok := cards[p.Title]
		_, seen := cards[p.Latest]
		if p.ReadAt != nil || !ok || !seen {
			continue
		}
		m := pushrelay.Message{
			Token: sub.Endpoint, Environment: *sub.ApnsEnvironment, Collapse: p.Title.String(),
			Notification: strconv.FormatInt(p.ID, 10),
		}
		if err := devices.Send(ctx, m); err != nil {
			return err
		}
	}
	return nil
}
