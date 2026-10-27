package push

import (
	"context"
	"encoding/json"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/webpush"
)

// waiting is one request, waiting or not, and the browsers of those who decide it.
type waiting struct {
	request store.Request
	subs    []store.KeeperSubscription
	gone    []uuid.UUID
}

func (w *waiting) RequestPushes(context.Context, uuid.UUID) (store.Request, []store.KeeperSubscription, bool, error) {
	return w.request, w.subs, w.request.State == domain.RequestPending, nil
}

func (w *waiting) Unsubscribe(_ context.Context, _, id uuid.UUID) error {
	w.gone = append(w.gone, id)
	return nil
}

// pushService takes every message but those for an ended subscription, keeping each by endpoint.
type pushService struct {
	ended string
	got   map[string]webpush.Message
}

func (p *pushService) Send(_ context.Context, sub webpush.Subscription, m webpush.Message) error {
	if sub.Endpoint == p.ended {
		return webpush.ErrGone
	}
	p.got[sub.Endpoint] = m
	return nil
}

// A request that waits is pushed to each browser of those who decide it, saying who asked and
// opening where it is decided; a browser whose subscription ended is forgotten; a request decided
// before the push is sent is pushed to no one.
func TestARequestWaitingIsPushedToTheBrowsersOfWhoeverDecidesIt(t *testing.T) {
	keeper := uuid.NewV7()
	sub := func(endpoint string) store.KeeperSubscription {
		return store.KeeperSubscription{Keeper: keeper, ID: uuid.NewV7(), Kind: domain.PushWeb, Endpoint: endpoint, Language: "en"}
	}
	ended := sub("https://push.example.com/ended")
	st := &waiting{
		request: store.Request{ID: uuid.NewV7(), ProfileName: "Kids", Title: "Dune", State: domain.RequestPending},
		subs:    []store.KeeperSubscription{sub("https://push.example.com/admin"), ended},
	}
	service := &pushService{ended: ended.Endpoint, got: map[string]webpush.Message{}}
	if err := Requested(st, service)(t.Context(), st.request.ID); err != nil {
		t.Fatal(err)
	}
	sent, ok := service.got["https://push.example.com/admin"]
	var told message
	if !ok || json.Unmarshal(sent.Payload, &told) != nil {
		t.Fatalf("the admin's browser was sent %+v", sent)
	}
	if want := (notification{Title: "Dune", Body: "Kids asked for it", Lang: "en", Tag: st.request.ID.String(), Navigate: "/settings/requests"}); told.Notification != want || told.WebPush != declarativePush {
		t.Errorf("pushed %+v, want %+v", told, want)
	}
	if len(st.gone) != 1 || st.gone[0] != ended.ID {
		t.Errorf("forgot %v, want the ended subscription", st.gone)
	}

	st.request.State, service.got = domain.RequestApproved, map[string]webpush.Message{}
	if err := Requested(st, service)(t.Context(), st.request.ID); err != nil || len(service.got) != 0 {
		t.Errorf("a request decided since: %v, pushed %d", err, len(service.got))
	}
}
