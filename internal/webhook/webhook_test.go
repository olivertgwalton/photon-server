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
	t        *testing.T
	mu       sync.Mutex
	statuses []int
	got      []received
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		r.t.Error(err)
	}
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
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	server := events.Server{ID: uuid.NewV7(), Name: "den"}
	hub := events.New(st, k, server.ID, func() string { return server.Name }, log)
	ctx := t.Context()

	rx := &receiver{t: t, statuses: []int{http.StatusServiceUnavailable}}
	srv := httptest.NewServer(rx)
	defer srv.Close()
	const secret = "s3cret"
	hook, err := st.AddWebhook(ctx, srv.URL+"/hook?token=theirs", []domain.EventKind{domain.EventPlaybackStarted}, secret)
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := st.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	deliver := Deliver(st)
	node := uuid.NewV7()
	claim := func() []domain.Job {
		t.Helper()
		jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobDeliverWebhook}, nil, node, time.Minute, 10)
		if err != nil {
			t.Fatal(err)
		}
		return jobs
	}

	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackPaused, Profile: oliver.ID})
	if jobs := claim(); len(jobs) != 0 {
		t.Fatalf("a kind it did not ask for queued %d deliveries", len(jobs))
	}

	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackStarted, Profile: oliver.ID, Details: domain.PlaybackDetails{Playback: domain.NowPlaying{Method: domain.PlayDirect}}})
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
		Details struct {
			Playback struct {
				Method domain.PlayMethod `json:"method"`
			} `json:"playback"`
		} `json:"details"`
	}
	if err := json.Unmarshal(got.body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Event != domain.EventPlaybackStarted || body.Server != server || body.Profile.Name != "Oliver" ||
		body.Profile.ID != oliver.ID || body.Details.Playback.Method != domain.PlayDirect || time.Since(body.At) > time.Minute {
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

// An event about an episode names its ids, its numbers and its show, as a scrobbler finds it by.
func TestAnEpisodesEventNamesItsShowAndNumbers(t *testing.T) {
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
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	hub := events.New(st, k, uuid.NewV7(), func() string { return "den" }, log)
	ctx := t.Context()

	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	ep := store.Episode{
		Season: 1, Episodes: []int{2}, Title: "S01E02.mkv", Folder: "Severance", ByNumber: true,
		IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt11650328"}, Copies: []store.Copy{{
			ContentKey: []byte("1"), Parts: []store.Part{{RelPath: "S01E02.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
		}},
	}
	show := store.Show{Title: "Severance", Year: 2022, Folder: "Severance", IDs: map[domain.Provider]string{domain.ProviderTVDB: "371980"}}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Severance", []byte("v"), show, []store.Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	shows, _, err := st.Wall(ctx, []uuid.UUID{tv.ID}, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(shows) != 1 {
		t.Fatal(shows, err)
	}
	seasons, err := st.Seasons(ctx, uuid.UUID{}, shows[0].ID)
	if err != nil || len(seasons) != 1 {
		t.Fatal(seasons, err)
	}
	episodes, err := st.Episodes(ctx, uuid.UUID{}, seasons[0].ID)
	if err != nil || len(episodes) != 1 {
		t.Fatal(episodes, err)
	}

	rx := &receiver{t: t}
	srv := httptest.NewServer(rx)
	defer srv.Close()
	if _, err := st.AddWebhook(ctx, srv.URL, []domain.EventKind{domain.EventPlaybackStarted}, "s"); err != nil {
		t.Fatal(err)
	}
	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackStarted, Item: episodes[0].ID, Library: tv.ID})
	jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobDeliverWebhook}, nil, uuid.NewV7(), time.Minute, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	if err := Deliver(st)(ctx, jobs[0].Subject); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Title struct {
			Kind    domain.ItemKind            `json:"kind"`
			IDs     map[domain.Provider]string `json:"ids"`
			Season  int                        `json:"season"`
			Episode int                        `json:"episode"`
			Show    struct {
				ID    uuid.UUID                  `json:"id"`
				Title string                     `json:"title"`
				Year  int                        `json:"year"`
				IDs   map[domain.Provider]string `json:"ids"`
			} `json:"show"`
		} `json:"title"`
	}
	if err := json.Unmarshal(rx.got[0].body, &body); err != nil {
		t.Fatal(err)
	}
	if got := body.Title; got.Kind != domain.ItemEpisode || got.IDs[domain.ProviderIMDb] != "tt11650328" || got.Season != 1 || got.Episode != 2 ||
		got.Show.ID != shows[0].ID || got.Show.Title != "Severance" || got.Show.Year != 2022 || got.Show.IDs[domain.ProviderTVDB] != "371980" {
		t.Errorf("title = %s", rx.got[0].body)
	}
}

// A plugin that hears an event is sent it with its settings as they are when it is sent; its
// webhook is no admin's to list or remove, and goes with the plugin.
func TestAPluginIsToldOfTheEventsItHears(t *testing.T) {
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
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	hub := events.New(st, k, uuid.NewV7(), func() string { return "den" }, log)
	ctx := t.Context()
	node := uuid.NewV7()
	claim := func() []domain.Job {
		t.Helper()
		jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobDeliverWebhook}, nil, node, time.Minute, 10)
		if err != nil {
			t.Fatal(err)
		}
		return jobs
	}

	rx := &receiver{t: t}
	srv := httptest.NewServer(rx)
	defer srv.Close()
	plugin := store.Plugin{Slug: "scrobbler", Protocol: domain.PluginPhoton, URL: srv.URL, Manifest: []byte("{}")}
	hears := store.Hearing{URL: srv.URL + "/events/v1/event", Kinds: []domain.EventKind{domain.EventPlaybackStarted}}
	if err := st.AddPlugin(ctx, plugin, hears); err != nil {
		t.Fatal(err)
	}
	if hooks, err := st.Webhooks(ctx); err != nil || len(hooks) != 0 {
		t.Errorf("an admin's webhooks = %v %v, want none of a plugin's", hooks, err)
	}
	if err := st.SetProviderSettings(ctx, domain.PluginSource("scrobbler"), map[string]string{"token": "first"}); err != nil {
		t.Fatal(err)
	}

	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackStarted})
	jobs := claim()
	if len(jobs) != 1 {
		t.Fatalf("queued %d deliveries, want 1", len(jobs))
	}
	// The setting changes while the event waits: the plugin is sent it as it is now.
	if err := st.SetProviderSettings(ctx, domain.PluginSource("scrobbler"), map[string]string{"token": "second"}); err != nil {
		t.Fatal(err)
	}
	if err := Deliver(st)(ctx, jobs[0].Subject); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Settings map[string]string `json:"settings"`
		Event    struct {
			Event domain.EventKind `json:"event"`
		} `json:"event"`
	}
	if len(rx.got) != 1 || json.Unmarshal(rx.got[0].body, &sent) != nil || sent.Settings["token"] != "second" ||
		sent.Event.Event != domain.EventPlaybackStarted {
		t.Fatalf("sent %d: %s", len(rx.got), rx.got[0].body)
	}

	hub.Raise(ctx, domain.Event{Kind: domain.EventPlaybackStarted})
	if err := st.RemovePlugin(ctx, "scrobbler"); err != nil {
		t.Fatal(err)
	}
	for _, j := range claim() {
		if err := Deliver(st)(ctx, j.Subject); err != nil {
			t.Fatal(err)
		}
	}
	if len(rx.got) != 1 {
		t.Errorf("a removed plugin was sent %d events, want what waited for it forgotten", len(rx.got)-1)
	}
}
