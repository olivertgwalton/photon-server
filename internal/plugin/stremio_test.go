//go:build integration

package plugin

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func plugins(t *testing.T) *Plugins {
	t.Helper()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return New(st)
}

// AIOStreams' manifest under its configuration, which carries a password, and Riven's, which has
// streams alone.
func addons(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aio/s3cret/manifest.json":
			fmt.Fprint(w, `{"id": "com.aiostreams.viren070", "name": "AIOStreams", "types": ["movie", "series"],
				"catalogs": [{"type": "movie", "id": "top"}]}`)
		case "/aio/s3cret/catalog/movie/top.json":
			fmt.Fprint(w, `{"metas": [{"id": "tt0113277", "type": "movie", "name": "Heat"}]}`)
		case "/riven/manifest.json":
			fmt.Fprint(w, `{"id": "riven", "name": "Riven", "types": ["movie", "series"], "catalogs": []}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// A Stremio addon is registered by its manifest's address, as Stremio installs one, and is a
// provider whose catalogs are lists. Its address is never shown past its host, and two installs
// of one addon are told apart by an id given to the second.
func TestAStremioAddonIsAPlugin(t *testing.T) {
	p := plugins(t)
	at := addons(t)
	aio, err := p.Register(t.Context(), at+"/aio/s3cret/manifest.json", domain.PluginStremio, "")
	if err != nil {
		t.Fatal(err)
	}
	if aio.ID != "com-aiostreams-viren070" || aio.URL != at || strings.Contains(aio.URL, "s3cret") ||
		!slices.Equal(aio.Capabilities, []domain.Capability{domain.CapabilityList}) {
		t.Errorf("registered %+v", aio)
	}
	if _, err := p.Register(t.Context(), at+"/aio/s3cret/manifest.json", domain.PluginStremio, ""); !errors.Is(err, store.ErrPluginExists) {
		t.Errorf("installed again: %v, want ErrPluginExists", err)
	}
	if _, err := p.Register(t.Context(), at+"/aio/s3cret/manifest.json", domain.PluginStremio, "aio-kids"); err != nil {
		t.Errorf("installed again as aio-kids: %v", err)
	}
	riven, err := p.Register(t.Context(), at+"/riven/manifest.json", domain.PluginStremio, "")
	if err != nil || len(riven.Capabilities) != 0 {
		t.Errorf("Riven: %+v, %v; want no lists", riven, err)
	}
	if _, err := p.Register(t.Context(), at+"/aio/s3cret", domain.PluginStremio, ""); !errors.Is(err, ErrRefused) {
		t.Errorf("not a manifest's address: %v, want ErrRefused", err)
	}
	if _, err := p.Register(t.Context(), at+"/plugin", domain.PluginPhoton, "mine"); !errors.Is(err, ErrRefused) {
		t.Errorf("a photon plugin given an id: %v, want ErrRefused", err)
	}

	loaded, err := p.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(loaded, func(l provider.Provider) bool { return l.Info().ID == domain.PluginSource("aio-kids") })
	if i < 0 {
		t.Fatalf("loaded %d providers, none aio-kids", len(loaded))
	}
	listed, err := loaded[i].(provider.Lister).List(t.Context(), "movie/top")
	if err != nil || len(listed) != 1 || listed[0].Title != "Heat" {
		t.Errorf("its top films: %+v, %v", listed, err)
	}
	if r, err := p.Refresh(t.Context(), "aio-kids"); err != nil || r.ID != "aio-kids" {
		t.Errorf("read again: %+v, %v", r, err)
	}
}
