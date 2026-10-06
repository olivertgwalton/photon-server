//go:build integration

package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/events"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// node is one server node's hub, on its own connection to Valkey.
func node(t *testing.T, st *store.Store) *events.Hub {
	t.Helper()
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"))
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	return events.New(st, k, events.Server{ID: uuid.NewV7(), Name: "den"}, slog.New(slog.DiscardHandler))
}

type sse struct {
	name string
	data string
}

// stream reads a response's events, leaving out comments.
func stream(body *bufio.Reader) <-chan sse {
	out := make(chan sse)
	go func() {
		defer close(out)
		var e sse
		for {
			line, err := body.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case line == "" && e.name != "":
				out <- e
				e = sse{}
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out
}

func TestAnAdminsStreamTellsWhatHappensOnEveryNode(t *testing.T) {
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
	srv := httptest.NewServer(New(log, domain.Info{}, Services{Auth: fakeAuth{}, Tasks: work, Jobs: work, NowPlaying: work, Events: here}))
	defer srv.Close()
	open := func(token string) *http.Response {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/admin/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	refused := open(memberToken)
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", refused.StatusCode)
	}
	res := open(goodToken)
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type %q", res.Header.Get("Content-Type"))
	}
	told := stream(bufio.NewReader(res.Body))

	first := <-told
	var now snapshotJSON
	if err := json.Unmarshal([]byte(first.data), &now); err != nil || first.name != "snapshot" {
		t.Fatalf("first event %q %s, %v; want the snapshot", first.name, first.data, err)
	}
	if len(now.Tasks) != 1 || now.Tasks[0].Key != domain.TaskScanLibraries || len(now.Jobs) != 1 ||
		now.Jobs[0].LibraryID != films || len(now.Playbacks) != 1 {
		t.Errorf("snapshot %+v; want the scan task, the scan of the films and the playback", now)
	}

	// Told from the other node until this one, subscribing as the stream opened, hears it.
	lib := uuid.NewV7()
	await := func(kind domain.EventKind, raise func()) eventJSON {
		t.Helper()
		again := time.NewTicker(100 * time.Millisecond)
		defer again.Stop()
		deadline := time.After(10 * time.Second)
		for raise(); ; {
			select {
			case e, ok := <-told:
				if !ok {
					t.Fatalf("the stream ended waiting for %s", kind)
				}
				var got eventJSON
				if err := json.Unmarshal([]byte(e.data), &got); err != nil {
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
		there.Raise(t.Context(), domain.Event{Kind: domain.EventTaskFinished, Details: map[string]any{"task": domain.TaskSweepJobs, "result": domain.TaskSucceeded}})
	})
	if finished.Details["task"] != string(domain.TaskSweepJobs) || finished.ID != (uuid.UUID{}) {
		t.Errorf("task finished as %+v; want sweep_jobs's, kept nowhere", finished)
	}

	progress := await(domain.EventScanProgress, func() {
		there.Scanning(t.Context())(domain.ScanProgress{Library: lib, Phase: domain.ScanReading, Done: 3, Known: 10})
	})
	if progress.Details["phase"] != string(domain.ScanReading) || progress.Details["done"] != 3.0 || progress.Details["known"] != 10.0 {
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
		case _, ok := <-told:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the stream outlived its node")
		}
	}
}
