package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeLibraries keeps libraries in memory, and the scans asked for.
type fakeLibraries struct {
	libs    []domain.Library
	scanned []uuid.UUID
	folders []string
	// missing is how many copies on no disk each library holds, as an ask to forget them takes.
	missing map[uuid.UUID]int64
}

func (f *fakeLibraries) Libraries(context.Context) ([]domain.Library, error) { return f.libs, nil }

// LibraryCounts holds one film in every library.
func (f *fakeLibraries) LibraryCounts(context.Context) (map[uuid.UUID]domain.TitleCounts, error) {
	out := map[uuid.UUID]domain.TitleCounts{}
	for _, l := range f.libs {
		out[l.ID] = domain.TitleCounts{Movies: 1}
	}
	return out, nil
}

func (f *fakeLibraries) Library(_ context.Context, id uuid.UUID) (domain.Library, error) {
	for _, l := range f.libs {
		if l.ID == id {
			return l, nil
		}
	}
	return domain.Library{}, store.ErrNotFound
}

func (f *fakeLibraries) AddLibrary(_ context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error) {
	if slices.ContainsFunc(f.libs, func(l domain.Library) bool { return l.Name == name || l.Root == root }) {
		return domain.Library{}, store.ErrLibraryExists
	}
	l := domain.Library{ID: uuid.NewV7(), Name: name, Kind: kind, Root: root, Sources: domain.DefaultSources(kind), Monitor: domain.MonitorRealtime}
	f.libs = append(f.libs, l)
	return l, nil
}

func (f *fakeLibraries) AddRemoteLibrary(_ context.Context, name string, kind domain.LibraryKind, r store.Remote) (domain.Library, error) {
	l := domain.Library{
		ID: uuid.NewV7(), Name: name, Kind: kind, Media: domain.MediaRemote, ListSource: r.ListSource, ListID: r.ListID,
		DiscoverSource: r.DiscoverSource, StreamSource: r.StreamSource, Sources: domain.DefaultSources(kind), Monitor: domain.MonitorOff,
	}
	f.libs = append(f.libs, l)
	return l, nil
}

func (f *fakeLibraries) SetLibrary(_ context.Context, id uuid.UUID, c store.LibraryChange) error {
	i := slices.IndexFunc(f.libs, func(l domain.Library) bool { return l.ID == id })
	if i < 0 {
		return store.ErrNotFound
	}
	if c.Name != "" {
		f.libs[i].Name = c.Name
	}
	if c.Sources != nil {
		f.libs[i].Sources = c.Sources
	}
	if c.MetadataLanguage != nil {
		f.libs[i].Locale.Language = *c.MetadataLanguage
	}
	if c.CertificationCountry != nil {
		f.libs[i].Locale.Country = *c.CertificationCountry
	}
	if c.SubtitleLanguages != nil {
		f.libs[i].SubtitleLanguages = c.SubtitleLanguages
	}
	f.libs[i].SubtitleMatch = cmp.Or(c.SubtitleMatch, f.libs[i].SubtitleMatch)
	f.libs[i].MissingCopies = cmp.Or(c.MissingCopies, f.libs[i].MissingCopies)
	f.libs[i].BoxSets = cmp.Or(c.BoxSets, f.libs[i].BoxSets)
	if c.Requests != nil {
		f.libs[i].RequestSource = *c.Requests
	}
	return nil
}

// ForgetMissingCopies takes the copies on no disk of the library, as one title apiece.
func (f *fakeLibraries) ForgetMissingCopies(_ context.Context, lib uuid.UUID) (int64, store.Changed, error) {
	if !slices.ContainsFunc(f.libs, func(l domain.Library) bool { return l.ID == lib }) {
		return 0, nil, store.ErrNotFound
	}
	forgotten := f.missing[lib]
	delete(f.missing, lib)
	changed := store.Changed{}
	for range forgotten {
		changed[domain.TitleRemoved] = append(changed[domain.TitleRemoved], uuid.NewV7())
	}
	return forgotten, changed, nil
}

func (f *fakeLibraries) CertificateCountries(context.Context) ([]string, error) {
	return []string{"GB", "IN", "US"}, nil
}

func (f *fakeLibraries) RemoveLibrary(_ context.Context, id uuid.UUID) error {
	n := len(f.libs)
	f.libs = slices.DeleteFunc(f.libs, func(l domain.Library) bool { return l.ID == id })
	if len(f.libs) == n {
		return store.ErrNotFound
	}
	return nil
}

func (f *fakeLibraries) ScanFolders(_ context.Context, lib uuid.UUID, folders []string, _ time.Duration) error {
	f.scanned = append(f.scanned, lib)
	f.folders = append(f.folders, folders...)
	return nil
}

func (f *fakeLibraries) RefreshLibrary(_ context.Context, lib uuid.UUID, _ domain.RefreshMode) error {
	if !slices.ContainsFunc(f.libs, func(l domain.Library) bool { return l.ID == lib }) {
		return store.ErrNotFound
	}
	return nil
}

// addon keeps lists and streams, as a Stremio addon with catalogs does.
type addon struct{}

func (addon) Info() provider.Info {
	return provider.Info{ID: domain.PluginSource("aio"), Name: "AIOStreams"}
}

func (addon) List(context.Context, string) ([]domain.Listed, error) { return nil, nil }

func (addon) Streams(context.Context, domain.Streamed) ([]domain.Offer, error) { return nil, nil }

func (addon) Candidates(context.Context, domain.Locale, domain.ItemKind, string, int) ([]domain.Candidate, error) {
	return nil, nil
}

// A remote library holds a list a provider keeps, streamed by a provider that streams, and has no
// root to check or watch.
func TestAnAdminAddsARemoteLibrary(t *testing.T) {
	libs := &fakeLibraries{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Libraries: libs, Events: &fakeEvents{}, Providers: provider.NewRegistry(nil, addon{}),
	})
	do := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	rec := do(http.MethodPost, "/api/v1/admin/libraries",
		`{"name": "Popular", "kind": "movies", "media": "remote", "list": {"source": "plugin:aio", "id": "movie/top"}, "streams": "plugin:aio"}`)
	var added adminLibraryJSON
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil || rec.Code != http.StatusCreated ||
		added.Media != domain.MediaRemote || added.List == nil || added.List.ID != "movie/top" || added.Streams != domain.PluginSource("aio") {
		t.Fatalf("adding: %d %+v %v", rec.Code, added, err)
	}
	if !slices.Equal(libs.scanned, []uuid.UUID{added.ID}) {
		t.Errorf("scanned %v, want the new library's list read at once", libs.scanned)
	}
	for _, body := range []string{
		`{"name": "A", "kind": "movies", "media": "remote", "streams": "plugin:aio"}`,
		`{"name": "B", "kind": "movies", "media": "remote", "list": {"source": "plugin:aio", "id": "movie/top"}}`,
		`{"name": "C", "kind": "movies", "media": "remote", "list": {"source": "tvdb", "id": "top"}, "streams": "plugin:aio"}`,
		`{"name": "D", "kind": "movies", "media": "remote", "list": {"source": "plugin:aio", "id": "movie/top"}, "streams": "tmdb"}`,
		`{"name": "E", "kind": "movies", "media": "remote", "root": "/srv", "list": {"source": "plugin:aio", "id": "movie/top"}, "streams": "plugin:aio"}`,
		`{"name": "F", "kind": "movies", "media": "cloud"}`,
		`{"name": "G", "kind": "movies", "media": "remote", "discover": "omdb", "streams": "plugin:aio"}`,
		`{"name": "H", "kind": "movies", "media": "remote", "discover": "tmdb", "streams": "plugin:aio"}`,
	} {
		if rec := do(http.MethodPost, "/api/v1/admin/libraries", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400: %s", body, rec.Code, rec.Body)
		}
	}
	rec = do(http.MethodPost, "/api/v1/admin/libraries", `{"name": "Found", "kind": "movies", "media": "remote", "discover": "plugin:aio", "streams": "plugin:aio"}`)
	var found adminLibraryJSON
	if err := json.NewDecoder(rec.Body).Decode(&found); err != nil || rec.Code != http.StatusCreated || found.List != nil || found.Discover != domain.PluginSource("aio") {
		t.Errorf("adding one of what a search finds alone: %d %+v %v", rec.Code, found, err)
	}
	if rec := do(http.MethodPost, "/api/v1/admin/libraries/"+added.ID.String()+"/check", ""); rec.Code != http.StatusConflict {
		t.Errorf("checking its root: %d, want 409", rec.Code)
	}
}

func TestAnAdminKeepsTheLibraries(t *testing.T) {
	libs := &fakeLibraries{}
	told := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Libraries: libs, Events: told})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	root := t.TempDir()
	quoted, missing := strconv.Quote(root), strconv.Quote(filepath.Join(root, "missing"))
	if rec := do(memberToken, http.MethodGet, "/api/v1/admin/libraries", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member listing: %d, want 403", rec.Code)
	}
	rec := do(goodToken, http.MethodPost, "/api/v1/admin/libraries", `{"name": "Films", "kind": "movies", "root": `+quoted+`}`)
	var added adminLibraryJSON
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil || rec.Code != http.StatusCreated || added.Root != root {
		t.Fatalf("adding: %d %+v %v", rec.Code, added, err)
	}
	if !slices.Equal(libs.scanned, []uuid.UUID{added.ID}) {
		t.Errorf("scanned %v, want the new library at once", libs.scanned)
	}
	// Subtitle languages are kept as their standard writes them, once each.
	rec = do(goodToken, http.MethodPatch, "/api/v1/admin/libraries/"+added.ID.String(), `{"subtitle_languages": ["fr", "pt-br", "fr"], "subtitle_match": "any"}`)
	if !strings.Contains(rec.Body.String(), `"subtitle_languages":["fr","pt-BR"],"subtitle_match":"any"`) {
		t.Errorf("setting subtitle languages: %d %s", rec.Code, rec.Body)
	}
	// Forgetting a library's copies on no disk says how many went, as Plex's Empty Trash does.
	libs.missing = map[uuid.UUID]int64{added.ID: 3}
	rec = do(goodToken, http.MethodDelete, "/api/v1/admin/libraries/"+added.ID.String()+"/missing", "")
	if !strings.Contains(rec.Body.String(), `"copies":3`) {
		t.Errorf("forgetting the copies on no disk: %d %s", rec.Code, rec.Body)
	}
	// Their titles are told as a scan's are, so what is gone leaves every client's catalogue.
	if n := len(told.changed[added.ID][domain.TitleRemoved]); n != 3 {
		t.Errorf("told %d titles removed, want the 3 copies' own", n)
	}
	for _, tc := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Again", "kind": "movies", "root": ` + quoted + `}`, http.StatusConflict},
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Gone", "kind": "movies", "root": ` + missing + `}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Relative", "kind": "movies", "root": "films"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/admin/libraries", `{"name": "Music", "kind": "music", "root": "/"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"sources": [{"kind": "movie", "metadata": [{"source": "tmdb", "enabled": true}, {"source": "tmdb", "enabled": false}]}]}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"sources": [{"kind": "episode", "metadata": []}]}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"sources": [{"kind": "movie", "metadata": [{"source": "tvdb", "enabled": true}]}]}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"sources": [{"kind": "movie", "images": [{"source": "nfo", "enabled": true}]}]}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"name": "Movies", "sources": [{"kind": "movie", "metadata": [{"source": "tmdb", "enabled": true}, {"source": "nfo", "enabled": false}, {"source": "mdblist", "enabled": true}]}]}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"previews": "sometimes"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"previews": "chapters"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"markers": "fingerprints"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"markers": "chapters"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"keyframes": "sometimes"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"keyframes": "full"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"themes": "all"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"themes": "themerr"}`, http.StatusConflict},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"themes": "off"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"subtitle_languages": ["fr", "Klingon!"]}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"subtitle_match": "perfect"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"missing_copies": "bin"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"missing_copies": "forget"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"box_sets": "grouped"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"box_sets": "expanded"}`, http.StatusOK},
		{http.MethodPatch, "/api/v1/admin/libraries/" + added.ID.String(), `{"box_sets": "beside"}`, http.StatusOK},
		{http.MethodDelete, "/api/v1/admin/libraries/" + added.ID.String() + "/missing", "", http.StatusOK},
		{http.MethodDelete, "/api/v1/admin/libraries/" + uuid.NewV7().String() + "/missing", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/admin/libraries/" + added.ID.String() + "/scan", "", http.StatusAccepted},
		{http.MethodPost, "/api/v1/admin/libraries/" + uuid.NewV7().String() + "/scan", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/admin/libraries/" + added.ID.String() + "/refresh", `{"mode": "missing"}`, http.StatusAccepted},
		{http.MethodPost, "/api/v1/admin/libraries/" + added.ID.String() + "/refresh", `{"mode": "some"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/admin/libraries/" + added.ID.String() + "/refresh", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/admin/libraries/" + uuid.NewV7().String() + "/refresh", `{"mode": "all"}`, http.StatusNotFound},
		{http.MethodDelete, "/api/v1/admin/libraries/" + added.ID.String(), "", http.StatusNoContent},
		{http.MethodDelete, "/api/v1/admin/libraries/" + added.ID.String(), "", http.StatusNotFound},
	} {
		if rec := do(goodToken, tc.method, tc.target, tc.body); rec.Code != tc.want {
			t.Errorf("%s %s %s: %d, want %d: %s", tc.method, tc.target, tc.body, rec.Code, tc.want, rec.Body)
		}
	}
	if got, want := told.kinds(), []domain.EventKind{domain.EventLibraryAdded, domain.EventLibraryRemoved}; !slices.Equal(got, want) {
		t.Errorf("told %v, want %v", got, want)
	}
}

// An autoscan tool names the path a download landed at, as it does to Plex's refresh?path=.
func TestAnAdminScansTheFolderAPathIsIn(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Heat (1995)", "Subs"), 0o755); err != nil {
		t.Fatal(err)
	}
	lib := domain.Library{ID: uuid.NewV7(), Name: "Films", Kind: domain.LibraryMovies, Root: root}
	libs := &fakeLibraries{libs: []domain.Library{lib}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Libraries: libs})
	for _, tc := range []struct {
		path string
		want int
	}{
		{"", http.StatusAccepted},
		{filepath.Join(root, "Heat (1995)", "Heat (1995).mkv"), http.StatusAccepted},
		{filepath.Join(root, "Heat (1995)", "Subs", "English.srt"), http.StatusAccepted},
		{filepath.Join(root, "Alien (1979)", "Alien (1979).mkv"), http.StatusAccepted},
		{filepath.Join(root, "..", "elsewhere"), http.StatusBadRequest},
		{"Heat (1995)", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/libraries/"+lib.ID.String()+"/scan?path="+url.QueryEscape(tc.path), nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("path %q: %d, want %d: %s", tc.path, rec.Code, tc.want, rec.Body)
		}
	}
	// A path that is not there yet, or is gone, is read from the nearest folder above it.
	if want := []string{".", "Heat (1995)", "Heat (1995)", "."}; !slices.Equal(libs.folders, want) {
		t.Errorf("scanned %q, want %q", libs.folders, want)
	}
}

func TestALibraryAsksInALanguageAndCountryOfItsOwn(t *testing.T) {
	lib := domain.Library{ID: uuid.NewV7(), Name: "Films", Kind: domain.LibraryMovies, Root: "/srv/films"}
	libs := &fakeLibraries{libs: []domain.Library{lib}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Libraries: libs})
	do := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	target := "/api/v1/admin/libraries/" + lib.ID.String()
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"metadata_language": "Klingon!"}`, http.StatusBadRequest},
		// A region of the world is not a country with certificates of its own.
		{`{"certification_country": "EU"}`, http.StatusBadRequest},
		{`{"artwork_language": "textless"}`, http.StatusBadRequest},
		{`{"title_language": "english"}`, http.StatusBadRequest},
		{`{"metadata_language": "de-de", "certification_country": "in"}`, http.StatusOK},
	} {
		if rec := do(http.MethodPatch, target, tc.body); rec.Code != tc.want {
			t.Errorf("%s: %d, want %d: %s", tc.body, rec.Code, tc.want, rec.Body)
		}
	}
	if got := libs.libs[0].Locale; got != (domain.Locale{Language: "de-DE", Country: "IN"}) {
		t.Errorf("the library asks in %+v, want de-DE and India's certificates, written as their standards write them", got)
	}
	rec := do(http.MethodGet, "/api/v1/admin/locales", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"en-GB"`) || !strings.Contains(rec.Body.String(), `"countries":["GB","IN","US"]`) {
		t.Errorf("locales: %d %s, want TMDB's languages and the countries whose certificates are read", rec.Code, rec.Body)
	}
}

// fetcher fetches the titles a library lacks, as riven does.
type fetcher struct{}

func (fetcher) Info() provider.Info {
	return provider.Info{ID: domain.PluginSource("riven"), Name: "riven", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow}}
}

// filmFetcher fetches films alone, as Radarr does.
type filmFetcher struct{ fetcher }

func (filmFetcher) Info() provider.Info {
	return provider.Info{ID: domain.PluginSource("films"), Name: "Films", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (fetcher) Request(context.Context, domain.Wanted) error { return nil }

// An admin names where a library's approved requests go: a provider that fetches titles of every
// kind the library holds, and no other; none leaves them for an admin to fetch by hand.
func TestAnAdminNamesWhereALibrarysRequestsGo(t *testing.T) {
	shows := uuid.NewV7()
	libs := &fakeLibraries{libs: []domain.Library{{ID: films, Name: "Films", Kind: domain.LibraryMovies}, {ID: shows, Name: "Shows", Kind: domain.LibraryShows}}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Libraries: libs, Events: &fakeEvents{}, Providers: provider.NewRegistry(nil, addon{}, fetcher{}, filmFetcher{}),
	})
	setOn := func(lib uuid.UUID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/libraries/"+lib.String(), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	set := func(body string) *httptest.ResponseRecorder { return setOn(films, body) }
	var got adminLibraryJSON
	rec := set(`{"requests": "plugin:riven"}`)
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || rec.Code != http.StatusOK || got.Requests != domain.PluginSource("riven") {
		t.Errorf("sent to riven: %d %+v %v", rec.Code, got, err)
	}
	if rec := set(`{"requests": "plugin:aio"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("sent to a provider that fetches nothing: %d, want 400", rec.Code)
	}
	if rec := set(`{"requests": ""}`); rec.Code != http.StatusOK || libs.libs[0].RequestSource != "" {
		t.Errorf("sent nowhere: %d, %q", rec.Code, libs.libs[0].RequestSource)
	}
	if rec := set(`{"requests": "plugin:films"}`); rec.Code != http.StatusOK {
		t.Errorf("films sent to a fetcher of films: %d %s", rec.Code, rec.Body)
	}
	if rec := setOn(shows, `{"requests": "plugin:films"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "fetches no shows") {
		t.Errorf("shows sent to a fetcher of films: %d %s, want 400", rec.Code, rec.Body)
	}
}
