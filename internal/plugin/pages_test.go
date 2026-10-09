//go:build integration

package plugin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// A plugin's page is visited on its own origin: a profile is linked only to those it may visit,
// and the plugin is told who visits by claiming the visit's code, once.
func TestAPluginIsToldWhoVisitsItsPage(t *testing.T) {
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
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	p := New(st, k)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(pluginv1.Manifest{
			Protocol: pluginv1.Version, ID: "requests", Name: "Requests", Kinds: []string{"movie"},
			Capabilities: []pluginv1.Capability{{Name: "pages", Version: 1, Pages: []pluginv1.Page{
				{ID: "ask", Name: "Ask for a film", URL: "/ui", Access: "everyone"},
				{ID: "queue", Name: "Requests waiting", URL: "https://requests.example/admin", Access: "admin"},
				{ID: "elsewhere", Name: "Not the web", URL: "ftp://requests.example", Access: "everyone"},
				{ID: "nobody", Name: "For nobody", URL: "/nobody", Access: "owners"},
			}}},
		}); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	if _, err := p.Register(ctx, srv.URL, domain.PluginPhoton, ""); err != nil {
		t.Fatal(err)
	}
	admin, err := st.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.AddProfile(ctx, "Kid", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}

	pages, err := p.Pages(ctx, kid)
	if err != nil || len(pages) != 1 || pages[0].ID != "ask" {
		t.Errorf("the kid's pages = %+v %v, want the one for everyone", pages, err)
	}
	if pages, err := p.Pages(ctx, admin); err != nil || len(pages) != 2 {
		t.Errorf("the admin's pages = %+v %v, want both on the web", pages, err)
	}
	if _, err := p.Visit(ctx, kid, "requests", "queue"); !errors.Is(err, ErrNoPage) {
		t.Errorf("the kid visiting the admins' page: %v, want ErrNoPage", err)
	}

	at, err := p.Visit(ctx, kid, "requests", "ask")
	page, code, cut := strings.Cut(at, "#photon_visit=")
	if err != nil || !cut || page != srv.URL+"/ui" || code == "" {
		t.Fatalf("visit = %q %v, want the plugin's page with a code in its fragment", at, err)
	}
	who, visited, err := p.Claim(ctx, "requests", code)
	if err != nil || who.ID != kid.ID || who.Role != domain.RoleUser || visited != "ask" {
		t.Errorf("claimed %+v %q %v, want the kid on ask", who, visited, err)
	}
	if _, _, err := p.Claim(ctx, "requests", code); !errors.Is(err, ErrUnknownVisit) {
		t.Errorf("claiming again: %v, want ErrUnknownVisit", err)
	}

	at, err = p.Visit(ctx, admin, "requests", "queue")
	if err != nil {
		t.Fatal(err)
	}
	_, code, _ = strings.Cut(at, "#photon_visit=")
	if _, _, err := p.Claim(ctx, "another", code); !errors.Is(err, ErrUnknownVisit) {
		t.Errorf("another plugin claiming: %v, want ErrUnknownVisit", err)
	}
}
