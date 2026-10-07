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
	return domain.Network{Secure: domain.SecureDisabled}, nil
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
	for body, want := range map[string]int{
		`{"secure_connections":"disabled"}`:                                                    http.StatusOK,
		`{"secure_connections":"required"}`:                                                    http.StatusBadRequest,
		`{"secure_connections":"preferred","certificate":"/nowhere.pem","key":"/nowhere.key"}`: http.StatusBadRequest,
		`{"secure_connections":"always"}`:                                                      http.StatusBadRequest,
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
