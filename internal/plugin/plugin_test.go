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
			ID: "films", Capabilities: []pluginv1.Capability{{Name: "rate", Version: 1}},
			Settings: []pluginv1.Setting{{Key: "api_key", Name: "API key", Secret: true, Required: true}},
		},
		speaks: map[domain.Capability]int{domain.CapabilityRate: 1}, base: srv.URL, http: srv.Client(),
		settings: func(context.Context) (map[string]string, error) { return map[string]string{"api_key": "s3cret"}, nil },
	}
	_, err := c.Ratings(t.Context(), domain.ItemMovie, nil)
	if err == nil || !strings.Contains(err.Error(), "plugin films /rate/v1/ratings: 401 Unauthorized: Bad key") ||
		strings.Contains(err.Error(), "s3cret") || errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("refused: %v", err)
	}
	status = http.StatusServiceUnavailable
	if _, err := c.Ratings(t.Context(), domain.ItemMovie, nil); !errors.Is(err, provider.ErrUnavailable) || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("failing on its side: %v", err)
	}
}

// A plugin that redirects is answered as one that refused: the server never asks the host it
// points at, which would be sent the plugin's settings.
func TestAPluginsRedirectIsNotFollowed(t *testing.T) {
	asked := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true }))
	defer elsewhere.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c := &client{
		manifest: pluginv1.Manifest{
			ID: "films", Capabilities: []pluginv1.Capability{{Name: "rate", Version: 1}},
			Settings: []pluginv1.Setting{{Key: "api_key", Name: "API key", Secret: true}},
		},
		speaks: map[domain.Capability]int{domain.CapabilityRate: 1}, base: srv.URL, http: calls,
		settings: func(context.Context) (map[string]string, error) { return map[string]string{"api_key": "s3cret"}, nil },
	}
	if _, err := c.Ratings(t.Context(), domain.ItemMovie, nil); err == nil || !strings.Contains(err.Error(), "307") {
		t.Errorf("redirected: %v, want the redirect refused", err)
	}
	if asked {
		t.Error("the host redirected to was asked")
	}
}

// A plugin is refused where it answers nothing this server speaks, and registered for what it does
// where it answers more besides.
func TestAPluginIsRefusedWhereItAnswersNoCapabilityTheServerSpeaks(t *testing.T) {
	m := pluginv1.Manifest{
		Protocol: pluginv1.Version, ID: "films", Name: "Films", Kinds: []string{"movie"},
		Capabilities: []pluginv1.Capability{{Name: "subtitles", Version: 1}, {Name: "rate", Version: 2}},
	}
	if err := valid(m); !errors.Is(err, ErrRefused) {
		t.Errorf("answering nothing spoken: %v, want refused", err)
	}
	m.Capabilities = append(m.Capabilities, pluginv1.Capability{Name: "rate", Version: 1})
	if err := valid(m); err != nil {
		t.Errorf("answering rate at version 1 besides: %v", err)
	}
}
