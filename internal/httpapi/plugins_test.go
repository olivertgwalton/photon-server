//go:build integration

package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/identify"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// filmsPlugin is a plugin that knows Jaws, rates it with the key an admin set for it, and keeps
// the keys it was sent.
func filmsPlugin(t *testing.T, protocol int, id string) (*httptest.Server, func() []string) {
	var mu sync.Mutex
	var keys []string
	answer := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Error(err)
		}
	}
	read := func(r *http.Request, v any) {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			t.Error(err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /manifest", func(w http.ResponseWriter, _ *http.Request) {
		answer(w, pluginv1.Manifest{
			Protocol: protocol, ID: id, Name: "Films", Kinds: []string{"movie"}, Capabilities: []string{"describe", "rate"},
			Settings: []pluginv1.Setting{{Key: "api_key", Name: "API key", Secret: true, Required: true}},
		})
	})
	mux.HandleFunc("POST /match", func(w http.ResponseWriter, r *http.Request) {
		var req pluginv1.MatchRequest
		read(r, &req)
		mu.Lock()
		keys = append(keys, req.Settings["api_key"])
		mu.Unlock()
		if req.Title == "jaws" {
			answer(w, pluginv1.MatchResponse{ID: "jaws-1"})
			return
		}
		answer(w, pluginv1.MatchResponse{})
	})
	mux.HandleFunc("POST /describe", func(w http.ResponseWriter, r *http.Request) {
		var req pluginv1.DescribeRequest
		read(r, &req)
		if req.ID != "jaws-1" {
			http.Error(w, "unknown", http.StatusNotFound)
			return
		}
		answer(w, pluginv1.DescribeResponse{
			Title: "Jaws", Overview: "A shark.", ReleaseDate: "1975-06-20",
			IDs:     map[string]string{"imdb": "tt0073195", "plugin:" + id: "jaws-1"},
			Artwork: []pluginv1.Artwork{{Kind: "poster", URL: "https://pictures.example/jaws.jpg"}},
		})
	})
	mux.HandleFunc("POST /ratings", func(w http.ResponseWriter, r *http.Request) {
		var req pluginv1.RatingsRequest
		read(r, &req)
		if req.IDs["imdb"] != "tt0073195" || req.Settings["api_key"] == "" {
			answer(w, pluginv1.RatingsResponse{})
			return
		}
		answer(w, pluginv1.RatingsResponse{Ratings: []pluginv1.Rating{{Site: "imdb", Score: 81, Votes: 600000}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return keys
	}
}

func TestAPluginDescribesTheTitlesOfALibraryThatTakesIt(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	plugins := plugin.New(st)
	providers := provider.NewRegistry(plugins.Load)
	api := New(log, Info{}, Services{Auth: fakeAuth{}, Libraries: st, Providers: providers, ProviderSettings: st, Plugins: plugins})
	do := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}

	films, sent := filmsPlugin(t, pluginv1.Version, "films")
	if rec := do(http.MethodPost, "/api/v1/admin/plugins", `{"url": "`+films.URL+`/"}`); rec.Code != http.StatusCreated ||
		!strings.Contains(rec.Body.String(), `"provider":"plugin:films"`) {
		t.Fatalf("registering: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/api/v1/admin/plugins", `{"url": "`+films.URL+`"}`); rec.Code != http.StatusConflict {
		t.Errorf("registering its id again: %d, want 409", rec.Code)
	}
	future, _ := filmsPlugin(t, pluginv1.Version+1, "future")
	if rec := do(http.MethodPost, "/api/v1/admin/plugins", `{"url": "`+future.URL+`"}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "protocol 2") {
		t.Errorf("a protocol this server does not speak: %d %s, want 400", rec.Code, rec.Body)
	}
	down, _ := filmsPlugin(t, pluginv1.Version, "down")
	if rec := do(http.MethodPost, "/api/v1/admin/plugins", `{"url": "`+down.URL+`"}`); rec.Code != http.StatusCreated {
		t.Fatalf("registering the plugin that goes down: %d %s", rec.Code, rec.Body)
	}
	down.Close()

	rec := do(http.MethodGet, "/api/v1/admin/providers", "")
	if !strings.Contains(rec.Body.String(), `{"id":"plugin:films","name":"Films","kinds":["movie"],"capabilities":["describe","rate"],`+
		`"settings":[{"key":"api_key","name":"API key","secret":true,"required":true,"set":false}],"ready":false}`) {
		t.Errorf("providers: %s", rec.Body)
	}
	rec = do(http.MethodPatch, "/api/v1/admin/providers/plugin:films", `{"settings": {"api_key": "s3cret"}}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "s3cret") || !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Errorf("setting its key: %d %s; want it ready and the key not answered back", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPatch, "/api/v1/admin/providers/plugin:down", `{"settings": {"api_key": "other"}}`); rec.Code != http.StatusOK {
		t.Errorf("setting the key of the plugin that goes down: %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/admin/providers", ""); strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("the key was answered back: %s", rec.Body)
	}

	lib, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	target := "/api/v1/admin/libraries/" + lib.ID.String()
	if rec := do(http.MethodPatch, target, `{"sources": ["nfo", "plugin:absent"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a plugin not registered as a source: %d, want 400", rec.Code)
	}
	if rec := do(http.MethodPatch, target, `{"sources": ["nfo", "plugin:down", "plugin:films"]}`); rec.Code != http.StatusOK {
		t.Fatalf("taking the plugins: %d %s", rec.Code, rec.Body)
	}
	if _, err := st.SaveFolder(ctx, lib.ID, "jaws", []byte("v1"), []store.Film{{Title: "jaws", Folder: "jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, lib.ID, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	id := cards[0].ID

	// The plugin that is down is passed over for the next.
	if err := identify.Handler(st, providers, log)(ctx, id); err != nil {
		t.Fatalf("identifying: %v", err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Jaws" || page.Overview != "A shark." || page.IDs["plugin:films"] != "jaws-1" ||
		len(page.Ratings) != 1 || page.Ratings[0].Score != 81 {
		t.Errorf("page = %q %q %v %v; want the plugin's description, its id and its rating", page.Title, page.Overview, page.IDs, page.Ratings)
	}
	if keys := sent(); len(keys) == 0 || keys[0] != "s3cret" {
		t.Errorf("the plugin was sent %q, want its key", keys)
	}
	posters := page.Artwork[domain.ArtworkPoster]
	if len(posters) != 1 {
		t.Fatalf("posters = %v, want the plugin's", posters)
	}
	if pic, err := st.Picture(ctx, posters[0]); err != nil || pic.URL != "https://pictures.example/jaws.jpg" {
		t.Errorf("poster = %+v %v, want fetched from the plugin's address", pic, err)
	}

	if rec := do(http.MethodDelete, "/api/v1/admin/plugins/films", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("removing: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodGet, "/api/v1/admin/plugins", ""); strings.Contains(rec.Body.String(), "films") {
		t.Errorf("listed after removal: %s", rec.Body)
	}
	if page, err := st.Title(ctx, uuid.UUID{}, id); err != nil || page.Title != "Jaws" {
		t.Errorf("after removal the title is %q %v; want what the plugin said kept", page.Title, err)
	}
}
