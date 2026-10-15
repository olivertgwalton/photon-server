package pushrelay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type kept struct{ credential string }

func (k *kept) PushRelayCredential(context.Context) (string, error) { return k.credential, nil }

func (k *kept) SetPushRelayCredential(_ context.Context, c string) error {
	k.credential = c
	return nil
}

// relay is photon's push relay: it gives each registration a credential numbered from 1, knows only
// the newest, and answers each token's sends with its status.
type relay struct {
	mu         sync.Mutex
	registered []uuid.UUID
	sent       []Message
	status     map[string]int
}

func (r *relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch req.URL.Path {
	case "/v1/servers":
		var asked struct{ Server uuid.UUID }
		if err := json.NewDecoder(req.Body).Decode(&asked); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.registered = append(r.registered, asked.Server)
		if err := json.NewEncoder(w).Encode(map[string]string{"credential": r.credential()}); err != nil {
			panic(err)
		}
	case "/v1/send":
		if req.Header.Get("Authorization") != "Bearer "+r.credential() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var m Message
		if err := json.NewDecoder(req.Body).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if status := r.status[m.Token]; status != 0 {
			w.WriteHeader(status)
			return
		}
		r.sent = append(r.sent, m)
		w.WriteHeader(http.StatusAccepted)
	}
}

func (r *relay) credential() string { return string(rune('0' + len(r.registered))) }

// A server asks for a credential the first time it sends, keeps it, and asks again when the relay
// no longer knows it; a device APNs has ended is ErrGone.
func TestAServerIsKnownToTheRelayByTheCredentialItKeeps(t *testing.T) {
	rx := &relay{status: map[string]int{"ended": http.StatusGone, "broken": http.StatusBadGateway}}
	svc := httptest.NewServer(rx)
	defer svc.Close()
	server, k := uuid.NewV7(), &kept{}
	r := New(svc.URL, server, k)
	m := Message{Token: "a1", Environment: domain.APNsProduction, Collapse: "show", Notification: "7"}

	if err := r.Send(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if err := r.Send(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if len(rx.registered) != 1 || rx.registered[0] != server || k.credential != "1" || len(rx.sent) != 2 || rx.sent[0] != m {
		t.Fatalf("registered %v, kept %q, sent %+v; want one registration kept for both", rx.registered, k.credential, rx.sent)
	}

	k.credential = "stale"
	if err := r.Send(t.Context(), m); err != nil || len(rx.registered) != 2 || k.credential != "2" {
		t.Errorf("a credential the relay no longer knows: %v, registered %d, kept %q; want it asked for again", err, len(rx.registered), k.credential)
	}
	if err := r.Send(t.Context(), Message{Token: "ended"}); !errors.Is(err, ErrGone) {
		t.Errorf("a device APNs ended: %v, want ErrGone", err)
	}
	if err := r.Send(t.Context(), Message{Token: "broken"}); err == nil || errors.Is(err, ErrGone) {
		t.Errorf("a relay that failed: %v, want an error to try again on", err)
	}
}
