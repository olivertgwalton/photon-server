//go:build integration

package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

type received struct {
	header http.Header
	body   []byte
}

// receiver answers each POST with the next of its statuses, then 204.
type receiver struct {
	mu       sync.Mutex
	statuses []int
	got      []received
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, received{req.Header, body})
	status := http.StatusNoContent
	if len(r.statuses) > 0 {
		status, r.statuses = r.statuses[0], r.statuses[1:]
	}
	w.WriteHeader(status)
}

func TestAWebhookIsToldWhatItAskedFor(t *testing.T) {
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"))
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	server := events.Server{ID: uuid.NewV7(), Name: "den"}
	hub := events.New(st, k, server, log)
	ctx := t.Context()

	rx := &receiver{statuses: []int{http.StatusServiceUnavailable}}
	srv := httptest.NewServer(rx)
	defer srv.Close()
	const secret = "s3cret"
	hook, err := st.AddWebhook(ctx, srv.URL+"/hook?token=theirs", []domain.EventKind{domain.EventPlaybackStarted}, secret)
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := st.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h")
	if err != nil {
		t.Fatal(err)
	}
	deliver := Deliver(st)
	node := uuid.NewV7()
	claim := func() []domain.Job {
		t.Helper()
		jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobDeliverWebhook}, node, time.Minute, 10)
		if err != nil {
			t.Fatal(err)
		}
		return jobs
	}

	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackPaused, Profile: oliver.ID})
	if jobs := claim(); len(jobs) != 0 {
		t.Fatalf("a kind it did not ask for queued %d deliveries", len(jobs))
	}

	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackStarted, Profile: oliver.ID, Details: map[string]any{"method": "direct"}})
	jobs := claim()
	if len(jobs) != 1 {
		t.Fatalf("queued %d deliveries, want 1", len(jobs))
	}
	failed := deliver(ctx, jobs[0].Subject)
	if failed == nil || strings.Contains(failed.Error(), "theirs") {
		t.Fatalf("a receiver answering 503: %v; want a failure that does not repeat its address's token", failed)
	}
	if dead, err := st.FailJob(ctx, jobs[0], failed); err != nil || dead {
		t.Fatalf("failing it: dead %v, %v; want it queued again", dead, err)
	}
	// Tried again once its backoff has passed, as the worker would.
	if err := deliver(ctx, jobs[0].Subject); err != nil {
		t.Fatalf("the second try: %v", err)
	}
	if err := deliver(ctx, jobs[0].Subject); err != nil || len(rx.got) != 2 {
		t.Fatalf("a delivery made: %v, %d sent; want it sent once more and no more", err, len(rx.got))
	}

	got := rx.got[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(got.body)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); got.header.Get("X-Photon-Signature") != want {
		t.Errorf("signature %q, want %q", got.header.Get("X-Photon-Signature"), want)
	}
	if got.header.Get("X-Photon-Event") != string(domain.EventPlaybackStarted) || got.header.Get("Content-Type") != "application/json" {
		t.Errorf("headers %v", got.header)
	}
	var body struct {
		Event   domain.EventKind `json:"event"`
		At      time.Time        `json:"at"`
		Server  events.Server    `json:"server"`
		Profile struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		} `json:"profile"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(got.body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Event != domain.EventPlaybackStarted || body.Server != server || body.Profile.Name != "Oliver" ||
		body.Profile.ID != oliver.ID || body.Details["method"] != "direct" || time.Since(body.At) > time.Minute {
		t.Errorf("body %s", got.body)
	}

	if err := hub.TestWebhook(ctx, hook.ID); err != nil {
		t.Fatal(err)
	}
	for _, j := range claim() {
		if err := deliver(ctx, j.Subject); err != nil {
			t.Fatal(err)
		}
	}
	if len(rx.got) != 3 || rx.got[2].header.Get("X-Photon-Event") != string(domain.EventWebhookTest) {
		t.Errorf("after a test: %d sent; want the test event", len(rx.got))
	}
	if err := hub.TestWebhook(ctx, uuid.NewV7()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("testing no webhook: %v, want ErrNotFound", err)
	}
}
