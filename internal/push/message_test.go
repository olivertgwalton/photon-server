package push

import (
	"encoding/base64"
	"testing"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/pushrelay"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// A browser shows a request's notification by the request's title and who asked, opening the
// requests waiting, and replaces it by its notification; a title's as it always has.
func TestABrowserShowsEachKindOfNotification(t *testing.T) {
	said := words.In(language.English)
	asked := store.Push{ID: 13, Kind: domain.NotificationRequest, Asked: store.Request{ProfileName: "Kit", Title: "Dune", Year: 2021}}
	if n, topic := webNotification(said, "en", asked, nil); n != (notification{
		Title: "Dune (2021)", Body: "Kit asked for it", Lang: "en", Tag: "13", Navigate: "/settings/requests",
	}) || topic != "13" {
		t.Errorf("a request's: %+v, topic %q", n, topic)
	}

	film := uuid.NewV7()
	added := store.Push{ID: 12, Kind: domain.NotificationTitle, Title: film, Latest: film, New: 1}
	cards := map[uuid.UUID]store.Card{film: {ID: film, Kind: domain.ItemMovie, Title: "Heat"}}
	if n, topic := webNotification(said, "en", added, cards); n != (notification{
		Title: "Heat", Body: "New film", Lang: "en", Tag: film.String(), Navigate: "/titles/" + film.String() + "?notification=12",
	}) || topic != base64.RawURLEncoding.EncodeToString(film[:]) {
		t.Errorf("a title's: %+v, topic %q", n, topic)
	}
}

// A device is woken by a notification's id, keeping one of each title, or of each request, while
// it is away.
func TestADeviceIsWokenForEachKindOfNotification(t *testing.T) {
	production := domain.APNsProduction
	phone := store.PushSubscription{Kind: domain.PushApple, Endpoint: "a1b2", ApnsEnvironment: &production}
	show := uuid.NewV7()
	for _, c := range []struct {
		push store.Push
		want pushrelay.Message
	}{
		{store.Push{ID: 13, Kind: domain.NotificationRequest}, pushrelay.Message{Token: "a1b2", Environment: production, Collapse: "13", Notification: "13"}},
		{store.Push{ID: 12, Kind: domain.NotificationTitle, Title: show}, pushrelay.Message{Token: "a1b2", Environment: production, Collapse: show.String(), Notification: "12"}},
	} {
		if got := relayMessage(phone, c.push); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.push.Kind, got, c.want)
		}
	}
}
