package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type fakeNetwork struct{ set *domain.Network }

func (fakeNetwork) Network(context.Context) (domain.Network, error) {
	return domain.Network{Secure: domain.SecureDisabled, Jellyfin: domain.JellyfinOff, JellyfinPort: 8096}, nil
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
		Auth: fakeAuth{}, Events: &fakeEvents{}, Network: fakeNetwork{&set},
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
		Auth: fakeAuth{}, Events: &fakeEvents{}, Network: fakeNetwork{&set}, Setup: Setup{Listen: ":8640"},
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
