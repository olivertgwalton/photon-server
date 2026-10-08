package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
)

// A node that starts while a restore is under way does not go on to open the database: it says it
// is not ready, to a balancer and to a browser, until the restore ends or lapses.
func TestANodeStartingDuringARestoreWaitsSayingItIsNotReady(t *testing.T) {
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan bool)
	underway := func(context.Context) (bool, error) { //nolint:unparam // answers as restoreUnderway does
		select {
		case still := <-ended:
			return still, nil
		default:
			return true, nil
		}
	}
	done := make(chan error, 1)
	go func() { done <- answerRestoring(t.Context(), l, underway, slog.New(slog.DiscardHandler)) }()
	base := "http://" + l.Addr().String()
	for path, want := range map[string]string{"/readyz": "not_ready", "/api/v1/server": "restoring its database", "/": "Restoring…"} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), want) {
			t.Errorf("%s while restoring: %d %s, want 503 saying %q", path, res.StatusCode, body, want)
		}
	}
	select {
	case err := <-done:
		t.Fatalf("stopped waiting while the restore was under way: %v", err)
	default:
	}
	ended <- false
	if err := <-done; err != nil {
		t.Errorf("once the restore ended: %v, want the node to go on starting", err)
	}
}
