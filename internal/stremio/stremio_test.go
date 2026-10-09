package stremio

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type meta struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	ReleaseInfo string `json:"releaseInfo,omitempty"`
}

// addon serves an addon under /secret-config, whose catalogs are pages of metas by how many to
// skip.
func addon(t *testing.T, catalogs map[string][][]meta) *Addon {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/secret-config/catalog/")
		if !ok {
			http.NotFound(w, r)
			return
		}
		name, skipped, paged := strings.Cut(strings.TrimSuffix(rest, ".json"), "/skip=")
		pages, ok := catalogs[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		page := 0
		if paged {
			n, err := strconv.Atoi(skipped)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			for seen := 0; page < len(pages) && seen < n; page++ {
				seen += len(pages[page])
			}
		}
		var metas []meta
		if page < len(pages) {
			metas = pages[page]
		}
		if err := json.NewEncoder(w).Encode(map[string][]meta{"metas": metas}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	m := Manifest{Name: "Addon", Types: []string{"movie", "series"}}
	for name := range catalogs {
		typ, id, _ := strings.Cut(name, "/")
		m.Catalogs = append(m.Catalogs, Catalog{Type: typ, ID: id})
	}
	return New(domain.PluginSource("addon"), srv.URL+"/secret-config", m, srv.Client())
}

func TestACatalogIsItsFilmsInOrderAcrossItsPages(t *testing.T) {
	a := addon(t, map[string][][]meta{
		"movie/top": {
			{{"tt0113277", "movie", "Heat", "1995"}, {"tmdb:578", "movie", "", ""}, {"kitsu:1", "movie", "", ""}},
			{{"tt0078748", "movie", "Alien", ""}, {"tt0113277", "movie", "Heat", "1995"}},
		},
	})
	got, err := a.List(t.Context(), "movie/top")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Listed{
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Title: "Heat", Year: 1995},
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderTMDB: "578"}},
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0078748"}, Title: "Alien"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the catalog (-want +got), an addon's own id left out and one listed twice once:\n%s", diff)
	}
}

func TestAShowCatalogIsShows(t *testing.T) {
	got, err := addon(t, map[string][][]meta{"series/top": {{{"tt0903747", "series", "Breaking Bad", "2008-2013"}}}}).List(t.Context(), "series/top")
	if err != nil || len(got) != 1 || got[0].Kind != domain.ItemShow || got[0].Year != 2008 {
		t.Errorf("a series catalog: %+v, %v; want Breaking Bad, a show begun in 2008", got, err)
	}
}

// An addon that does not page answers its first page however much is skipped.
func TestACatalogThatDoesNotPageIsReadOnce(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		fmt.Fprint(w, `{"metas": [{"id": "tt0113277", "type": "movie"}]}`)
	}))
	defer srv.Close()
	a := New(domain.PluginSource("addon"), srv.URL, Manifest{Types: []string{"movie"}, Catalogs: []Catalog{{Type: "movie", ID: "top"}}}, srv.Client())
	got, err := a.List(t.Context(), "movie/top")
	if err != nil || len(got) != 1 || asked != 2 {
		t.Errorf("read %d titles in %d requests, %v; want one title, the second page seen to add nothing", len(got), asked, err)
	}
}

func TestACatalogIsOneTheAddonLists(t *testing.T) {
	a := addon(t, map[string][][]meta{"movie/top": nil})
	for _, id := range []string{"top", "channel/top", "movie/", "movie/gone"} {
		if _, err := a.List(t.Context(), id); !errors.Is(err, ErrNoSuchCatalog) {
			t.Errorf("%q: %v, want ErrNoSuchCatalog", id, err)
		}
	}
	if !a.Answers(domain.CapabilityList) {
		t.Error("an addon with a catalog keeps no lists")
	}
	if New(domain.PluginSource("riven"), "http://riven", Manifest{Types: []string{"movie"}}, nil).Answers(domain.CapabilityList) {
		t.Error("an addon with no catalog, as Riven's, keeps lists")
	}
}

// The addon's address carries its configuration, a debrid key among it, so no error names it.
func TestAnAddonsAddressIsNeverInAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	a := New(domain.PluginSource("addon"), srv.URL+"/secret-config", Manifest{Types: []string{"movie"}, Catalogs: []Catalog{{Type: "movie", ID: "top"}}}, srv.Client())
	_, err := a.List(t.Context(), "movie/top")
	if err == nil || strings.Contains(err.Error(), "secret-config") {
		t.Errorf("a catalog the addon fails to answer: %v, want an error that does not carry its address", err)
	}
}

func TestAnAddonIsRegisteredByItsManifestsAddress(t *testing.T) {
	for address, want := range map[string]string{
		"https://aio.example/stremio/uuid/secret/manifest.json": "https://aio.example/stremio/uuid/secret",
		"http://riven:8080/stremio/token/manifest.json":         "http://riven:8080/stremio/token",
		"https://aio.example/stremio/uuid/secret":               "",
		"https://aio.example/manifest.json?key=1":               "",
		"stremio://aio.example/manifest.json":                   "",
	} {
		got, err := Base(address)
		if want == "" && !errors.Is(err, ErrNotManifest) || want != "" && got != want {
			t.Errorf("%s: %q, %v; want %q", address, got, err, want)
		}
	}
	for id, want := range map[string]string{
		"com.stremio.torrentio.addon": "com-stremio-torrentio-addon",
		"Riven":                       "riven",
		"--aio--":                     "aio",
	} {
		if got := Slug(id); got != want {
			t.Errorf("Slug(%q) = %q, want %q", id, got, want)
		}
	}
}

// An addon's streams of a film or an episode, in its order, each keyed by what its bytes are: a
// torrent's file, else a file by name and size, else an address without the query a debrid link
// signs afresh. A stream with no address of its own, or that needs headers sent, is left out.
func TestAnAddonsStreamsAreOffersKeyedByTheirBytes(t *testing.T) {
	asked := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		fmt.Fprint(w, `{"streams": [
			{"name": "AIO", "description": "Heat.1995.2160p.mkv\n58 GB", "url": "https://debrid.example/dl/abc?sig=1",
				"infoHash": "ABC123", "fileIdx": 2, "behaviorHints": {"filename": "Heat.1995.2160p.mkv", "videoSize": 62000000000}},
			{"name": "Riven", "url": "http://riven:8080/stremio/media/7?token=t", "behaviorHints": {"filename": "Heat (1995).mkv", "videoSize": 9000}},
			{"title": "Heat 1080p\nmore", "url": "https://cdn.example/play/heat.mkv?exp=2"},
			{"infoHash": "DEF456"},
			{"ytId": "abc"},
			{"url": "https://cdn.example/needs-referer.mkv", "behaviorHints": {"proxyHeaders": {"request": {"Referer": "x"}}}}
		]}`)
	}))
	defer srv.Close()
	a := New(domain.PluginSource("addon"), srv.URL, Manifest{Types: []string{"movie", "series"}}, srv.Client())
	got, err := a.Streams(t.Context(), domain.Streamed{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}})
	if err != nil {
		t.Fatal(err)
	}
	var keys, names []string
	for _, o := range got {
		keys, names = append(keys, o.Key), append(names, o.Name)
	}
	if want := []string{"torrent:abc123:2", "file:Heat (1995).mkv:9000", "url:cdn.example/play/heat.mkv"}; !slices.Equal(keys, want) {
		t.Errorf("keys %q, want %q", keys, want)
	}
	if want := []string{"Heat.1995.2160p.mkv", "Heat (1995).mkv", "Heat 1080p"}; !slices.Equal(names, want) {
		t.Errorf("names %q, want %q", names, want)
	}
	if asked != "/stream/movie/tt0113277.json" {
		t.Errorf("asked %s", asked)
	}
	if _, err := a.Streams(t.Context(), domain.Streamed{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTMDB: "1438"}, Season: 1, Episode: 2}); err != nil || asked != "/stream/series/tmdb:1438:1:2.json" {
		t.Errorf("an episode known by its show's TMDB id asked %s, %v", asked, err)
	}
	if got, err := a.Streams(t.Context(), domain.Streamed{Kind: domain.ItemMovie}); got != nil || err != nil {
		t.Errorf("a film with no id: %v, %v; want nothing asked", got, err)
	}
}

func TestAnAddonStreamsWhereItSaysItDoes(t *testing.T) {
	for resources, want := range map[string]bool{`["stream"]`: true, `[{"name": "stream", "types": ["movie"]}]`: true, `["catalog", "meta"]`: false} {
		var m Manifest
		if err := json.Unmarshal([]byte(`{"types": ["movie"], "resources": `+resources+`}`), &m); err != nil {
			t.Fatal(err)
		}
		if got := New(domain.PluginSource("addon"), "http://addon", m, nil).Answers(domain.CapabilityStream); got != want {
			t.Errorf("resources %s stream: %v, want %v", resources, got, want)
		}
	}
}
