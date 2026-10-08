package jellyfin

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/websocket/websockettest"
)

type fakeSettings struct {
	mu sync.Mutex
	n  domain.Network
}

func (f *fakeSettings) Network(context.Context) (domain.Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n, nil
}

func (f *fakeSettings) set(mode domain.JellyfinMode, port int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n.Jellyfin, f.n.JellyfinPort = mode, port
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func answers(port int) bool {
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/System/Ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// eventually waits for ok, over real sockets, which synctest cannot wait on.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	again := time.NewTicker(10 * time.Millisecond)
	defer again.Stop()
	deadline := time.After(5 * time.Second)
	for !ok() {
		select {
		case <-again.C:
		case <-deadline:
			t.Fatalf("never: %s", what)
		}
	}
}

// An admin turns the API on, moves it, and turns it off, and every node follows as it is told; a
// port another program holds is reported, and taken once it is free.
func TestTheAPIIsServedWhereAndWhileAnAdminSays(t *testing.T) {
	api, _, _, _ := newAPI()
	settings := &fakeSettings{n: domain.Network{Jellyfin: domain.JellyfinOff, JellyfinPort: 8096}}
	events := make(chan domain.Event, 1)
	tell := func() { events <- domain.Event{Kind: domain.EventNetworkChanged} }
	l, err := NewListener(settings, func() (<-chan domain.Event, func()) { return events, func() {} }, api,
		"127.0.0.1:8640", func(l net.Listener) net.Listener { return l }, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); l.Run(ctx) }()

	first, second := freePort(t), freePort(t)
	settings.set(domain.JellyfinOn, first)
	tell()
	eventually(t, "served once turned on", func() bool { return answers(first) })

	settings.set(domain.JellyfinOn, second)
	tell()
	eventually(t, "moved to the new port", func() bool { return answers(second) && !answers(first) })

	held, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(first))
	if err != nil {
		t.Fatal(err)
	}
	settings.set(domain.JellyfinOn, first)
	tell()
	eventually(t, "a held port reported", func() bool { return errors.Is(l.Err(), syscall.EADDRINUSE) })
	held.Close()
	tell()
	eventually(t, "the port taken once free", func() bool { return l.Err() == nil && answers(first) })

	settings.set(domain.JellyfinOff, first)
	tell()
	eventually(t, "nothing served once off", func() bool { return !answers(first) })

	settings.set(domain.JellyfinOn, second)
	tell()
	eventually(t, "served again", func() bool { return answers(second) })
	cancel()
	<-done
	if answers(second) {
		t.Error("still served after the node stopped")
	}
}

// An app's socket is closed as the API stops being served, though the server no longer tracks a
// connection taken from it.
func TestAnAppsSocketClosesAsTheAPIStops(t *testing.T) {
	api, _, _, _ := newAPI()
	port := freePort(t)
	settings := &fakeSettings{n: domain.Network{Jellyfin: domain.JellyfinOn, JellyfinPort: port}}
	events := make(chan domain.Event, 1)
	l, err := NewListener(settings, func() (<-chan domain.Event, func()) { return events, func() {} }, api,
		"127.0.0.1:8640", func(l net.Listener) net.Listener { return l }, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); l.Run(ctx) }()
	eventually(t, "served", func() bool { return answers(port) })
	c := openSocket(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/socket?ApiKey=pst_device", nil)
	heard(t, c)

	settings.set(domain.JellyfinOff, port)
	events <- domain.Event{Kind: domain.EventNetworkChanged}
	if head, _, err := c.Next(); err != nil || head != websockettest.Fin|websockettest.OpClose {
		t.Errorf("once off: %#x %v, want the socket closed", head, err)
	}
	cancel()
	<-done
}
