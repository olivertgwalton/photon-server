package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		_, _ = w.Write([]byte(`{"title": "Bad key", "detail": "s3cret is not a key"}`))
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

func TestAPluginSaysAShowsNextEpisodeToAir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"title": "The Wire", "next_episode": {"season": 2, "number": 1, "title": "Ebb Tide", "air_date": "2003-06-01"}}`))
	}))
	defer srv.Close()
	c := &client{
		manifest: pluginv1.Manifest{ID: "shows", Capabilities: []string{"describe"}},
		base:     srv.URL, http: srv.Client(),
		settings: func(context.Context) (map[string]string, error) { return nil, nil },
	}
	m, _, err := c.Describe(t.Context(), domain.ItemShow, "wire", domain.SeasonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Airing{SeasonNumber: 2, EpisodeNumber: 1, Title: "Ebb Tide", Date: time.Date(2003, 6, 1, 0, 0, 0, 0, time.UTC)}
	if m.NextAiring == nil || *m.NextAiring != want {
		t.Errorf("next airing = %+v, want %+v", m.NextAiring, want)
	}
}
