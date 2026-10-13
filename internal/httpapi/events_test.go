//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
	"github.com/olivertgwalton/photon-server/internal/websocket/websockettest"
)

// node is one server node's hub, on its own connection to Valkey: the nodes of one server share
// its keys, as they share its Postgres.
func node(t *testing.T, st *store.Store) *events.Hub {
	t.Helper()
	server, err := st.ServerID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), server)
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	return events.New(st, k, uuid.NewV7(), func() string { return "den" }, slog.New(slog.DiscardHandler))
}

// heard is a message as the events socket sends it.
type heard struct {
	feed feed
	name string
	data json.RawMessage
}

// listen opens the events socket at target as token, and reads its messages until it closes.
func listen(t *testing.T, target, token string) <-chan heard {
	t.Helper()
	c, res, err := websockettest.Dial(t.Context(), target, http.Header{"Authorization": {"Bearer " + token}})
	if err != nil || c == nil {
		t.Fatalf("%s as %s: %v %v", target, token, res, err)
	}
	res.Body.Close()
	t.Cleanup(func() { c.Close() })
	// Dial gives up after a few seconds; a test waits longer for what it raises.
	if err := c.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	out := make(chan heard, 64)
	go func() {
		defer close(out)
		for {
			head, payload, err := c.Next()
			if err != nil || head == websockettest.Fin|websockettest.OpClose {
				return
			}
			if head != websockettest.Fin|websockettest.OpText {
				continue
			}
			var m struct {
				Feed  feed            `json:"feed"`
				Event string          `json:"event"`
				Data  json.RawMessage `json:"data"`
			}
			if json.Unmarshal(payload, &m) != nil {
				return
			}
			out <- heard{m.Feed, m.Event, m.Data}
		}
	}()
	return out
}

func TestAnAdminIsToldWhatHappensOnEveryNode(t *testing.T) {
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
	here, there := node(t, st), node(t, st)
	running, shutDown := context.WithCancel(t.Context())
	defer shutDown()
	go here.Run(running)

	work := &fakeWork{}
	srv := httptest.NewServer(New(log, domain.Info{}, Services{Copies: noCopies{}, Discover: noDiscoveries{}, Auth: fakeAuth{}, Tasks: work, Jobs: work, NowPlaying: work, Events: here, Audience: st, Profiles: listedProfiles{oliver}, Libraries: &fakeLibraries{}}))
	defer srv.Close()
	refused, res, err := websockettest.Dial(t.Context(), srv.URL+"/api/v1/events?feed=server", http.Header{"Authorization": {"Bearer " + memberToken}})
	if refused != nil || err != nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("a member asking for the server's feed: %v %v, want 403", res, err)
	}
	if res != nil {
		res.Body.Close()
	}
	sent := make(chan heard)
	go func() {
		defer close(sent)
		for m := range listen(t, srv.URL+"/api/v1/events?feed=server", goodToken) {
			if m.feed == feedServer {
				sent <- m
			}
		}
	}()

	first := <-sent
	var now snapshotJSON
	if err := json.Unmarshal(first.data, &now); err != nil || first.name != "snapshot" {
		t.Fatalf("first event %q %s, %v; want the snapshot", first.name, first.data, err)
	}
	if len(now.Tasks) != 1 || now.Tasks[0].Key != domain.TaskScanLibraries || len(now.Jobs) != 1 ||
		now.Jobs[0].LibraryID != films || len(now.Playbacks) != 1 {
		t.Errorf("snapshot %+v; want the scan task, the scan of the films and the playback", now)
	}

	// Told from the other node until this one, subscribing as the stream opened, hears it.
	lib := uuid.NewV7()
	await := func(kind domain.EventKind, raise func()) told {
		t.Helper()
		again := time.NewTicker(100 * time.Millisecond)
		defer again.Stop()
		deadline := time.After(10 * time.Second)
		for raise(); ; {
			select {
			case e, ok := <-sent:
				if !ok {
					t.Fatalf("the socket closed waiting for %s", kind)
				}
				var got told
				if err := json.Unmarshal(e.data, &got); err != nil {
					t.Fatal(err)
				}
				if e.name == string(kind) && (kind != domain.EventScanProgress || got.LibraryID == lib) {
					return got
				}
			case <-again.C:
				raise()
			case <-deadline:
				t.Fatalf("no %s", kind)
			}
		}
	}
	finished := await(domain.EventTaskFinished, func() {
		there.Raise(t.Context(), domain.Event{Kind: domain.EventTaskFinished, Details: domain.TaskDetails{Task: domain.TaskSweepJobs, Result: domain.TaskSucceeded}})
	})
	if finished.Details["task"] != string(domain.TaskSweepJobs) || finished.ID != (uuid.UUID{}) {
		t.Errorf("task finished as %+v; want sweep_jobs's, kept nowhere", finished)
	}

	progress := await(domain.EventScanProgress, func() {
		there.Scanning(t.Context())(domain.ScanProgress{Library: lib, Phase: domain.ScanReading, Done: 3, Known: 10, Folder: "Heat (1995)"})
	})
	if progress.Details["phase"] != string(domain.ScanReading) || progress.Details["done"] != 3.0 || progress.Details["known"] != 10.0 || progress.Details["folder"] != "Heat (1995)" {
		t.Errorf("scan progress told as %+v", progress)
	}
	scanning := func() bool {
		scans, err := here.Scans(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range scans {
			if s.Library == lib {
				return true
			}
		}
		return false
	}
	if !scanning() {
		t.Error("the scan is not in what a new snapshot would show")
	}
	there.Scanned(t.Context(), lib)
	if scanning() {
		t.Error("the scan outlived its end")
	}

	shutDown()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-sent:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the socket outlived its node")
		}
	}
}
