package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/webpush"
	"github.com/olivertgwalton/photon-server/internal/words"
)

type requestPushes interface {
	RequestPushes(ctx context.Context, id uuid.UUID) (store.Request, []store.KeeperSubscription, bool, error)
	Unsubscribe(ctx context.Context, profile, id uuid.UUID) error
}

// decideAt is the page a request pushed opens, where it is decided.
const decideAt = "/settings/requests"

// Requested is the job that pushes a request, while it waits, to each browser of those who decide
// it, in its language. Apple devices are woken only for notifications in the inbox, which the
// Photon apps ask the server for by id; a request is not one. A browser that ended its subscription
// is forgotten; one that fails has every browser sent again later, where each replaces what it
// shows of the same request.
func Requested(st requestPushes, browsers web) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		r, subs, waits, err := st.RequestPushes(ctx, id)
		if err != nil || !waits {
			return err
		}
		var failed []error
		for _, sub := range subs {
			err := sendRequest(ctx, browsers, sub.PushSubscription, r)
			if errors.Is(err, webpush.ErrGone) {
				// Unsubscribed on another device meanwhile is as good.
				if err = st.Unsubscribe(ctx, sub.Keeper, sub.ID); errors.Is(err, store.ErrNotFound) {
					err = nil
				}
			}
			failed = append(failed, err)
		}
		return errors.Join(failed...)
	}
}

func sendRequest(ctx context.Context, browsers web, sub store.PushSubscription, r store.Request) error {
	said := words.In(language.Make(sub.Language))
	payload, err := json.Marshal(message{WebPush: declarativePush, Notification: notification{
		Title: r.Title, Body: said.AskedFor(r.ProfileName), Lang: sub.Language,
		Tag: r.ID.String(), Navigate: decideAt,
	}})
	if err != nil {
		return err
	}
	to := webpush.Subscription{Endpoint: sub.Endpoint, P256DH: sub.P256DH, Auth: sub.Auth}
	return browsers.Send(ctx, to, webpush.Message{Payload: payload, Topic: base64.RawURLEncoding.EncodeToString(r.ID[:]), TTL: pushTTL})
}
