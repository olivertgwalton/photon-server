package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/httpapi"
)

func TestServeAnswersTheQuestion(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	info := httpapi.Info{ID: "0199d1b2-0000-7000-8000-000000000001", Name: "Lounge", Version: "1.2.3"}
	done := make(chan error)
	ctx, stop := context.WithCancel(t.Context())
	go func() { done <- Serve(ctx, server, info, slog.New(slog.DiscardHandler)) }()

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ask := func(payload string) ([]byte, error) {
		if _, err := client.WriteTo([]byte(payload), server.LocalAddr()); err != nil {
			t.Fatal(err)
		}
		if err := client.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 1024)
		n, _, err := client.ReadFrom(buf)
		return buf[:n], err
	}

	if got, err := ask("who is PhotonServer? please"); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("another question was answered: %q, %v", got, err)
	}
	got, err := ask("WHO IS PHOTONSERVER?\n")
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		ID, Name, Version, Address string
	}
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("%v in %s", err, got)
	}
	port := server.LocalAddr().(*net.UDPAddr).Port
	want := "http://127.0.0.1:" + strconv.Itoa(port)
	if a.ID != info.ID || a.Name != info.Name || a.Version != info.Version || a.Address != want {
		t.Errorf("answer = %s, want %+v at %s", got, info, want)
	}

	stop()
	if err := <-done; err != nil {
		t.Errorf("Serve stopped with %v", err)
	}
}

func TestOnlyNearbyAskersAreAnswered(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1":        true,
		"::1":              true,
		"10.1.2.3":         true,
		"172.20.0.5":       true,
		"192.168.1.20":     true,
		"169.254.10.1":     true,
		"fd12:3456::1":     true,
		"fe80::1%en0":      true,
		"::ffff:192.0.2.1": false,
		"::ffff:10.0.0.9":  true,
		"8.8.8.8":          false,
		"100.64.0.1":       false,
		"2001:4860::8888":  false,
	} {
		if got := nearby(netip.MustParseAddr(addr)); got != want {
			t.Errorf("nearby(%s) = %t, want %t", addr, got, want)
		}
	}
}
