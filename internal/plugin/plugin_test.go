package plugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// A plugin that repeats its key in an error, or fails on its side, is named in the server's
// error without the key, and one that fails on its side is passed over.
func TestAPluginsErrorNamesItAndNeverItsKey(t *testing.T) {
	status := http.StatusUnauthorized
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		if _, err := io.WriteString(w, `{"title": "Bad key", "detail": "s3cret is not a key"}`); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	c := &client{
		manifest: pluginv1.Manifest{
			ID: "films", Capabilities: []string{"rate"},
			Settings: []pluginv1.Setting{{Key: "api_key", Name: "API key", Secret: true, Required: true}},
		},
		base: srv.URL, http: srv.Client(),
		settings: func(context.Context) (map[string]string, error) { return map[string]string{"api_key": "s3cret"}, nil },
	}
	_, err := c.Ratings(t.Context(), domain.ItemMovie, nil)
	if err == nil || !strings.Contains(err.Error(), "plugin films /ratings: 401 Unauthorized: Bad key") ||
		strings.Contains(err.Error(), "s3cret") || errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("refused: %v", err)
	}
	status = http.StatusServiceUnavailable
	if _, err := c.Ratings(t.Context(), domain.ItemMovie, nil); !errors.Is(err, provider.ErrUnavailable) || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("failing on its side: %v", err)
	}
}
