package httpapi

import (
	"cmp"
	"context"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin"
)

type pluginAdmin interface {
	List(ctx context.Context) ([]plugin.Registered, error)
	Register(ctx context.Context, address string, protocol domain.PluginProtocol, id string) (plugin.Registered, error)
	Refresh(ctx context.Context, slug string) (plugin.Registered, error)
	Remove(ctx context.Context, slug string) error
}

var slugParam = param{"slug", "", "The plugin's id, as its manifest gives it."}

type pluginJSON struct {
	ID string `json:"id"`
	// Provider is its id among the providers, and a library's sources.
	Provider domain.FieldSource    `json:"provider"`
	Protocol domain.PluginProtocol `json:"protocol"`
	// URL is where it answers: of a Stremio addon, its host alone, as the rest of its address is
	// its configuration.
	URL  string `json:"url"`
	Name string `json:"name"`
	// Version is the version of photon's protocol it speaks; none for a Stremio addon.
	Version      int                 `json:"version,omitzero"`
	Kinds        []domain.ItemKind   `json:"kinds"`
	Capabilities []domain.Capability `json:"capabilities"`
}

type addPluginJSON struct {
	// URL is where the plugin answers: photon's protocol's manifest is GET {url}/manifest; a
	// Stremio addon's URL is that of its manifest.json, as Stremio installs it.
	URL string `json:"url"`
	// Protocol is what it speaks, photon unless said.
	Protocol domain.PluginProtocol `json:"protocol,omitzero"`
	// ID names a Stremio addon, where its own would be another's: two installs of one addon,
	// configured apart. A plugin speaking photon's protocol is the id its manifest gives.
	ID string `json:"id,omitzero"`
}

func pluginOf(r plugin.Registered) pluginJSON {
	return pluginJSON{
		ID: r.ID, Provider: domain.PluginSource(r.ID), Protocol: r.Protocol, URL: r.URL, Name: r.Name, Version: r.Version,
		Kinds: r.Kinds, Capabilities: r.Capabilities,
	}
}

func (a *API) adminPlugins(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.Plugins.List(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]pluginJSON, len(all))
	for n, p := range all {
		out[n] = pluginOf(p)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[pluginJSON]{Items: out})
}

// addPlugin registers the plugin at an address, once its manifest is read and is one this server
// speaks. It is then a provider an admin sets and a library takes.
func (a *API) addPlugin(w http.ResponseWriter, r *http.Request) {
	var req addPluginJSON
	if !a.decode(w, r, &req) {
		return
	}
	p, err := a.svc.Plugins.Register(r.Context(), req.URL, cmp.Or(req.Protocol, domain.PluginPhoton), req.ID)
	if a.answeredPlugin(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, pluginOf(p))
}

// refreshPlugin reads a plugin's manifest again.
func (a *API) refreshPlugin(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.Plugins.Refresh(r.Context(), r.PathValue("slug"))
	if a.answeredPlugin(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pluginOf(p))
}

// removePlugin forgets a plugin. What it said about titles stands until another source says
// otherwise.
func (a *API) removePlugin(w http.ResponseWriter, r *http.Request) {
	if a.answeredPlugin(w, r, a.svc.Plugins.Remove(r.Context(), r.PathValue("slug"))) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// answeredPlugin answers a plugin's change, and has the providers read again after one.
func (a *API) answeredPlugin(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		a.svc.Providers.Forget()
	}
	return a.answered(w, r, err)
}

func (a *API) pluginsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/plugins", access: admin, summary: "List the registered plugins",
			status: http.StatusOK, reply: listJSON[pluginJSON]{}, handle: a.adminPlugins,
		},
		{
			pattern: "POST /api/v1/admin/plugins", access: admin,
			summary: "Register the plugin or Stremio addon at an address, once its manifest is read",
			body:    addPluginJSON{}, status: http.StatusCreated, reply: pluginJSON{}, handle: a.addPlugin,
		},
		{
			pattern: "DELETE /api/v1/admin/plugins/{slug}", access: admin,
			summary: "Forget a plugin; what it said about titles stays",
			path:    []param{slugParam}, status: http.StatusNoContent, handle: a.removePlugin,
		},
		{
			pattern: "POST /api/v1/admin/plugins/{slug}/refresh", access: admin, summary: "Read a plugin's manifest again",
			path: []param{slugParam}, status: http.StatusOK, reply: pluginJSON{}, handle: a.refreshPlugin,
		},
	}
}
