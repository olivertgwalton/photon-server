package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
)

// A node that starts while a restore is under way does not go on to open the database: it says it
// is not ready, to a balancer and to a browser, until the restore ends or lapses, and failing to
// look for it once does not end the wait.
func TestANodeStartingDuringARestoreWaitsSayingItIsNotReady(t *testing.T) {
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	type answer struct {
		still bool
		err   error
	}
	ended := make(chan answer)
	underway := func(context.Context) (bool, error) {
		select {
		case a := <-ended:
			return a.still, a.err
		default:
			return true, nil
		}
	}
	done := make(chan error, 1)
	go func() { done <- answerRestoring(t.Context(), l, underway, slog.New(slog.DiscardHandler)) }()
	base := "http://" + l.Addr().String()
	for path, want := range map[string]string{"/readyz": "not_ready", "/api/v1/server": "restoring its database", "/": "Restoring…"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), want) {
			t.Errorf("%s while restoring: %d %s, want 503 saying %q", path, res.StatusCode, body, want)
		}
	}
	select {
	case err := <-done:
		t.Fatalf("stopped waiting while the restore was under way: %v", err)
	default:
	}
	ended <- answer{err: errors.New("valkey is unreachable")}
	select {
	case ended <- answer{still: false}:
	case err := <-done:
		t.Fatalf("stopped waiting when it could not look for the restore: %v", err)
	}
	if err := <-done; err != nil {
		t.Errorf("once the restore ended: %v, want the node to go on starting", err)
	}
}
