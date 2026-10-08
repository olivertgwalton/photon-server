package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakeNetwork is the defaults, a remote stream limited to remoteKbps, and keeps what is set.
type fakeNetwork struct {
	set        *domain.Network
	remoteKbps int
}

func (f fakeNetwork) Network(context.Context) (domain.Network, error) {
	return domain.Network{
		Secure: domain.SecureDisabled, Jellyfin: domain.JellyfinOff, JellyfinPort: 8096, RemoteMaxBitrateKbps: f.remoteKbps,
	}, nil
}

func (f fakeNetwork) SetNetwork(_ context.Context, n domain.Network) error {
	*f.set = n
	return nil
}

// Secure connections are refused without a certificate this node can read, so an admin cannot
// lock every browser out.
func TestSecureConnectionsNeedACertificateTheServerReads(t *testing.T) {
	var set domain.Network
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Events: &fakeEvents{}, Network: fakeNetwork{set: &set}, Setup: Setup{Listen: ":8640"},
	})
	const jellyfin = `,"jellyfin":"off","jellyfin_port":8096}`
	for body, want := range map[string]int{
		`{"secure_connections":"disabled"` + jellyfin:                                                    http.StatusOK,
		`{"secure_connections":"required"` + jellyfin:                                                    http.StatusBadRequest,
		`{"secure_connections":"preferred","certificate":"/nowhere.pem","key":"/nowhere.key"` + jellyfin: http.StatusBadRequest,
		`{"secure_connections":"always"` + jellyfin:                                                      http.StatusBadRequest,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/network", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d: %s", body, rec.Code, want, rec.Body)
		}
	}
	if set.Secure != domain.SecureDisabled {
		t.Errorf("stored %+v, want only the disabled one", set)
	}
}

// Jellyfin's apps are given a port of their own: a real one, and never the one photon's own API is
// served on.
func TestJellyfinIsGivenAPortOfItsOwn(t *testing.T) {
	var set domain.Network
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Events: &fakeEvents{}, Network: fakeNetwork{set: &set}, Setup: Setup{Listen: ":8640"},
	})
	for body, want := range map[string]int{
		`{"secure_connections":"disabled","jellyfin":"on","jellyfin_port":8096}`:    http.StatusOK,
		`{"secure_connections":"disabled","jellyfin":"on","jellyfin_port":8640}`:    http.StatusBadRequest,
		`{"secure_connections":"disabled","jellyfin":"on","jellyfin_port":0}`:       http.StatusBadRequest,
		`{"secure_connections":"disabled","jellyfin":"on","jellyfin_port":70000}`:   http.StatusBadRequest,
		`{"secure_connections":"disabled","jellyfin":"maybe","jellyfin_port":8096}`: http.StatusBadRequest,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/network", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d: %s", body, rec.Code, want, rec.Body)
		}
	}
	if set.Jellyfin != domain.JellyfinOn || set.JellyfinPort != 8096 {
		t.Errorf("stored %+v, want Jellyfin on 8096", set)
	}
}

type heldPort struct{}

func (heldPort) Err() error { return errors.New("port 8096: address already in use") }

// An admin is told when the node they ask cannot have Jellyfin's port.
func TestAnAdminSeesWhyJellyfinsAppsCannotReachTheServer(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Network: fakeNetwork{}, Jellyfin: heldPort{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/network", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"jellyfin_error":"port 8096: address already in use"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

// A remote stream's limit is no limit or a bitrate, never less.
func TestARemoteStreamsLimitIsABitrate(t *testing.T) {
	var set domain.Network
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Events: &fakeEvents{}, Network: fakeNetwork{set: &set}, Setup: Setup{Listen: ":8640"},
	})
	for body, want := range map[string]int{
		`{"secure_connections":"disabled","jellyfin":"off","jellyfin_port":8096,"remote_max_bitrate_kbps":8000}`: http.StatusOK,
		`{"secure_connections":"disabled","jellyfin":"off","jellyfin_port":8096,"remote_max_bitrate_kbps":-1}`:   http.StatusBadRequest,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/network", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d: %s", body, rec.Code, want, rec.Body)
		}
	}
	if set.RemoteMaxBitrateKbps != 8000 {
		t.Errorf("stored %+v, want the remote limit kept", set)
	}
}

// The networks whose clients are local are prefixes or addresses, kept as prefixes.
func TestLocalNetworksAreNetworks(t *testing.T) {
	var set domain.Network
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Events: &fakeEvents{}, Network: fakeNetwork{set: &set}, Setup: Setup{Listen: ":8640"},
	})
	for body, want := range map[string]int{
		`{"secure_connections":"disabled","jellyfin":"off","jellyfin_port":8096,"remote_max_bitrate_kbps":0,"local_networks":["100.64.0.0/10"," 10.1.2.3 "]}`: http.StatusOK,
		`{"secure_connections":"disabled","jellyfin":"off","jellyfin_port":8096,"remote_max_bitrate_kbps":0,"local_networks":["the office"]}`:                 http.StatusBadRequest,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/network", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d: %s", body, rec.Code, want, rec.Body)
		}
	}
	if fmt.Sprint(set.LocalNetworks) != "[100.64.0.0/10 10.1.2.3/32]" {
		t.Errorf("stored %v, want the tailnet and the one address", set.LocalNetworks)
	}
}
