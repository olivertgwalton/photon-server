package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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

// A plugin keeps lists a library's titles come from and streams the copies it plays; a title with
// no ids, or a stream with no key or no web address, is left out.
func TestAPluginListsTitlesAndStreamsThem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /list/v1/list", func(w http.ResponseWriter, r *http.Request) {
		var req pluginv1.ListRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID != "best" {
			http.NotFound(w, r)
			return
		}
		writeJSON(t, w, pluginv1.ListResponse{Titles: []pluginv1.Listed{
			{Kind: "movie", IDs: map[string]string{"imdb": "tt0113277"}, Title: "Heat", Year: 1995},
			{Kind: "movie", Title: "Nobody knows it"},
		}})
	})
	mux.HandleFunc("POST /stream/v1/streams", func(w http.ResponseWriter, r *http.Request) {
		var req pluginv1.StreamsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IDs["imdb"] != "tt0113277" {
			writeJSON(t, w, pluginv1.StreamsResponse{})
			return
		}
		writeJSON(t, w, pluginv1.StreamsResponse{Streams: []pluginv1.Stream{
			{Key: "file:Heat.mkv:42", URL: "http://" + r.Host + "/heat.mkv", Filename: "Heat.mkv", Size: 42},
			{URL: "http://" + r.Host + "/unkeyed.mkv"},
			{Key: "magnet", URL: "magnet:?xt=urn:btih:abc"},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := pluginv1.Manifest{ID: "films", Kinds: []string{"movie"}, Capabilities: []pluginv1.Capability{{Name: "list", Version: 1}, {Name: "stream", Version: 1}}}
	c := &client{manifest: m, speaks: spoken(m), base: srv.URL, http: srv.Client(), settings: func(context.Context) (map[string]string, error) { return nil, nil }}

	listed, err := c.List(t.Context(), "best")
	if err != nil || len(listed) != 1 || listed[0].IDs[domain.ProviderIMDb] != "tt0113277" || listed[0].Title != "Heat" {
		t.Errorf("list = %+v %v, want Heat alone", listed, err)
	}
	if _, err := c.List(t.Context(), "absent"); err == nil {
		t.Error("a list the plugin does not have was answered")
	}
	offers, err := c.Streams(t.Context(), domain.Streamed{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}})
	if err != nil || len(offers) != 1 || offers[0].Key != "file:Heat.mkv:42" || offers[0].Name != "Heat.mkv" ||
		offers[0].From != strings.TrimPrefix(srv.URL, "http://") {
		t.Errorf("streams = %+v %v, want the keyed web stream, from the plugin's host", offers, err)
	}
	if got := provider.Capabilities(c); !slices.Equal(got, []domain.Capability{domain.CapabilityList, domain.CapabilityStream}) {
		t.Errorf("capabilities = %v", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}
