//go:build integration

package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// browser is a browser's push service: it answers each browser's messages with its status, and
// keeps those it takes, opened as the browser would.
type browser struct {
	t      *testing.T
	key    *ecdh.PrivateKey
	auth   []byte
	mu     sync.Mutex
	status map[string]int
	got    map[string][]message
}

func (b *browser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		b.t.Error(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if status := b.status[r.URL.Path]; status != 0 {
		w.WriteHeader(status)
		return
	}
	var m message
	if err := json.Unmarshal(b.open(body), &m); err != nil {
		b.t.Errorf("a message that is not JSON: %v", err)
	}
	b.got[r.URL.Path] = append(b.got[r.URL.Path], m)
	w.WriteHeader(http.StatusCreated)
}

// answer sets what an endpoint answers; 0 takes its messages.
func (b *browser) answer(path string, status int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status[path] = status
}

// open reads a one-record aes128gcm body with the browser's key and secret.
func (b *browser) open(body []byte) []byte {
	salt, keyID, sealed := body[:16], body[21:86], body[86:]
	as, err := ecdh.P256().NewPublicKey(keyID)
	if err != nil {
		b.t.Fatal(err)
	}
	shared, err := b.key.ECDH(as)
	if err != nil {
		b.t.Fatal(err)
	}
	ikm := must(hkdf.Key(sha256.New, shared, b.auth, "WebPush: info\x00"+string(b.key.PublicKey().Bytes())+string(keyID), 32))
	prk := must(hkdf.Extract(sha256.New, ikm, salt))
	cek := must(hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16))
	nonce := must(hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12))
	gcm := must(cipher.NewGCM(must(aes.NewCipher(cek))))
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil || plain[len(plain)-1] != 2 {
		b.t.Fatalf("a message the browser cannot open: %v", err)
	}
	return plain[:len(plain)-1]
}

func TestAProfilesNotificationsArePushedToItsBrowsers(t *testing.T) {
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
	key := must(ecdh.P256().GenerateKey(nil))
	b := &browser{t: t, key: key, auth: make([]byte, 16), status: map[string]int{"/ended": http.StatusGone}, got: map[string][]message{}}
	svc := httptest.NewTLSServer(b)
	defer svc.Close()
	for _, path := range []string{"/laptop", "/ended"} {
		sub := store.PushSubscription{Endpoint: svc.URL + path, P256DH: key.PublicKey().Bytes(), Auth: b.auth, Language: "en"}
		if _, _, err := st.Subscribe(ctx, fan.ID, sub); err != nil {
			t.Fatal(err)
		}
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
	show := shows[0].ID
	if err := st.Watchlist(ctx, fan.ID, show); err != nil {
		t.Fatal(err)
	}
	scan(2, 3)

	s := must(New(must(NewKey())))
	s.client = svc.Client()
	push := Push(st, s)
	if err := push(ctx, fan.ID); err != nil {
		t.Fatal(err)
	}
	got := b.got["/laptop"]
	if len(got) != 1 {
		t.Fatalf("the laptop was sent %+v, want one message of the show", got)
	}
	// Declarative, so a browser that reads them shows it with no service worker.
	if m := got[0]; m.WebPush != 8030 || m.Notification.Title != "Severance" || m.Notification.Tag != show.String() ||
		m.Notification.Navigate != "/titles/"+show.String() || m.Notification.Body == "" || m.Notification.Lang != "en" {
		t.Errorf("message = %+v", m)
	}
	if subs, err := st.PushSubscriptions(ctx, fan.ID); err != nil || len(subs) != 1 || subs[0].Endpoint != svc.URL+"/laptop" {
		t.Errorf("subscriptions after one ended: %+v %v, want the laptop's alone", subs, err)
	}

	// Pushed once, nothing is pushed again; a failing browser leaves its pushes to be tried again.
	if err := push(ctx, fan.ID); err != nil || len(b.got["/laptop"]) != 1 {
		t.Errorf("pushed again: %v, %d messages", err, len(b.got["/laptop"]))
	}
	b.answer("/laptop", http.StatusInternalServerError)
	scan(4)
	if err := push(ctx, fan.ID); err == nil {
		t.Error("a push service that failed: no error, want the job tried again")
	}
	b.answer("/laptop", 0)
	if err := push(ctx, fan.ID); err != nil || len(b.got["/laptop"]) != 2 {
		t.Errorf("tried again: %v, %d messages, want the new episode sent", err, len(b.got["/laptop"]))
	}
}
