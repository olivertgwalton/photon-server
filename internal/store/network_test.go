//go:build integration

package store

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A new server leaves Jellyfin's apps out, on Jellyfin's own port, until an admin lets them in.
func TestJellyfinIsOffUntilAnAdminTurnsItOn(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	n, err := s.Network(ctx)
	if err != nil || n.Jellyfin != domain.JellyfinOff || n.JellyfinPort != 8096 {
		t.Fatalf("a new server: %+v, %v; want Jellyfin off on 8096", n, err)
	}
	n.Jellyfin, n.JellyfinPort = domain.JellyfinOn, 8097
	n.LocalNetworks = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}
	if err := s.SetNetwork(ctx, n); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Network(ctx); err != nil || !reflect.DeepEqual(got, n) {
		t.Errorf("kept %+v, %v; want %+v", got, err, n)
	}
}

// An admin who clears the local networks saves none, which is the private networks.
func TestNoLocalNetworksAreSaved(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	n, err := s.Network(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n.LocalNetworks = nil
	if err := s.SetNetwork(ctx, n); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Network(ctx); err != nil || len(got.LocalNetworks) != 0 {
		t.Errorf("kept %v, %v; want none", got.LocalNetworks, err)
	}
}
