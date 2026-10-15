//go:build integration

package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/pushrelay"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
	"github.com/olivertgwalton/photon-server/internal/webpush"
)

// browsers is each browser's push service: it answers each endpoint's messages with its error, and
// keeps those it takes.
type browsers struct {
	mu     sync.Mutex
	answer map[string]error
	got    map[string][]message
}

func (b *browsers) Send(_ context.Context, sub webpush.Subscription, m webpush.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.answer[sub.Endpoint]; err != nil {
		return err
	}
	var told message
	if err := json.Unmarshal(m.Payload, &told); err != nil {
		return err
	}
	b.got[sub.Endpoint] = append(b.got[sub.Endpoint], told)
	return nil
}

// phones is photon's push relay: it answers each token's wake-ups with its error, and keeps those it
// takes.
type phones struct {
	mu     sync.Mutex
	answer map[string]error
	got    map[string][]pushrelay.Message
}

func (p *phones) Send(_ context.Context, m pushrelay.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.answer[m.Token]; err != nil {
		return err
	}
	p.got[m.Token] = append(p.got[m.Token], m)
	return nil
}

func (b *browsers) set(endpoint string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.answer[endpoint] = err
}

// fixture is a store with a show library, a profile following its show, and a scan adding more of
// its episodes.
func fixture(t *testing.T) (*store.Store, uuid.UUID, uuid.UUID, func(more ...int)) {
	t.Helper()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ctx := t.Context()
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	fan, err := st.AddProfile(ctx, "Fan", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	var episodes []int
	scan := func(more ...int) {
		t.Helper()
		since := time.Now()
		episodes = append(episodes, more...)
		var eps []store.Episode
		for _, n := range episodes {
			rel := fmt.Sprintf("Severance/S01E%02d.mkv", n)
			eps = append(eps, store.Episode{
				Season: 1, Episodes: []int{n}, Title: rel, Folder: "Severance", ByNumber: true,
				Copies: []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
			})
		}
		if _, err := st.SaveShowFolder(ctx, tv.ID, "Severance", fmt.Append(nil, episodes), store.Show{Title: "Severance", Folder: "Severance"}, eps, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Notify(ctx, tv.ID, since); err != nil {
			t.Fatal(err)
		}
	}
	scan(1)
	shows, _, err := st.Search(ctx, store.SearchQuery{Profile: fan.ID, Text: "Severance", Kinds: []domain.ItemKind{domain.ItemShow}, Limit: 1})
	if err != nil || len(shows) != 1 {
		t.Fatalf("the show: %+v %v", shows, err)
	}
	if err := st.Watchlist(ctx, fan.ID, shows[0].ID); err != nil {
		t.Fatal(err)
	}
	return st, fan.ID, shows[0].ID, scan
}

func TestAProfilesNotificationsArePushedToItsBrowsers(t *testing.T) {
	st, fan, show, scan := fixture(t)
	ctx := t.Context()
	b := &browsers{answer: map[string]error{"https://push.example/ended": webpush.ErrGone}, got: map[string][]message{}}
	for _, endpoint := range []string{"https://push.example/laptop", "https://push.example/ended"} {
		sub := store.PushSubscription{Kind: domain.PushWeb, Endpoint: endpoint, P256DH: []byte{4}, Auth: make([]byte, 16), Language: "en"}
		if _, _, err := st.Subscribe(ctx, fan, sub); err != nil {
			t.Fatal(err)
		}
	}
	production := domain.APNsProduction
	for _, token := range []string{"phone", "old-phone"} {
		sub := store.PushSubscription{Kind: domain.PushApple, Endpoint: token, ApnsEnvironment: &production, Language: "en"}
		if _, _, err := st.Subscribe(ctx, fan, sub); err != nil {
			t.Fatal(err)
		}
	}
	p := &phones{answer: map[string]error{"old-phone": pushrelay.ErrGone}, got: map[string][]pushrelay.Message{}}
	scan(2, 3)

	push := Job(st, b, p)
	if err := push(ctx, fan); err != nil {
		t.Fatal(err)
	}
	got := b.got["https://push.example/laptop"]
	if len(got) != 1 {
		t.Fatalf("the laptop was sent %+v, want one message of the show", got)
	}
	// Declarative, so a browser that reads them shows it with no service worker.
	if m := got[0]; m.WebPush != 8030 || m.Notification.Title != "Severance" || m.Notification.Tag != show.String() ||
		!strings.HasPrefix(m.Notification.Navigate, "/titles/"+show.String()+"?notification=") || m.Notification.Body == "" || m.Notification.Lang != "en" {
		t.Errorf("message = %+v", m)
	}
	// A phone is woken by the notification's id, which it asks the server to word, and told of the
	// show once.
	woken := p.got["phone"]
	if len(woken) != 1 || woken[0].Collapse != show.String() || woken[0].Notification == "" || woken[0].Environment != production {
		t.Errorf("the phone was sent %+v, want one wake-up of the show's notification", woken)
	}
	subs, err := st.PushSubscriptions(ctx, fan)
	if err != nil || len(subs) != 2 || subs[0].Endpoint != "https://push.example/laptop" || subs[1].Endpoint != "phone" {
		t.Errorf("subscriptions after two ended: %+v %v, want the laptop's and the phone's", subs, err)
	}

	// Pushed once, nothing is pushed again; a failing browser leaves its pushes to be tried again.
	if err := push(ctx, fan); err != nil || len(b.got["https://push.example/laptop"]) != 1 {
		t.Errorf("pushed again: %v, %d messages", err, len(b.got["https://push.example/laptop"]))
	}
	b.set("https://push.example/laptop", errors.New("push.example answered 500"))
	scan(4)
	if err := push(ctx, fan); err == nil {
		t.Error("a push service that failed: no error, want the job tried again")
	}
	b.set("https://push.example/laptop", nil)
	if err := push(ctx, fan); err != nil || len(b.got["https://push.example/laptop"]) != 2 {
		t.Errorf("tried again: %v, %d messages, want the new episode sent", err, len(b.got["https://push.example/laptop"]))
	}
}
