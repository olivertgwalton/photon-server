package httpapi

import (
	"cmp"
	"context"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin"
)

type pluginAdmin interface {
	List(ctx context.Context) ([]plugin.Registered, error)
	Register(ctx context.Context, address string, protocol domain.PluginProtocol, id string) (plugin.Registered, error)
	Refresh(ctx context.Context, slug string) (plugin.Registered, error)
	Remove(ctx context.Context, slug string) error
	Pages(ctx context.Context, profile domain.Profile) ([]plugin.Page, error)
	Visit(ctx context.Context, profile domain.Profile, slug, page string) (string, error)
	Claim(ctx context.Context, slug, code string) (domain.Profile, string, error)
}

var slugParam = param{"slug", "", "The plugin's id, as its manifest gives it."}

type pluginJSON struct {
	ID string `json:"id"`
	// Provider is its id among the providers, and a library's sources.
	Provider domain.FieldSource    `json:"provider"`
	Protocol domain.PluginProtocol `json:"protocol"`
	// URL is where it answers: of a Stremio addon, its host alone, as the rest of its address is
	// its configuration.
	URL  uri    `json:"url"`
	Name string `json:"name"`
	// Version is the version of photon's protocol it speaks; none for a Stremio addon.
	Version      int                 `json:"version,omitzero"`
	Kinds        []domain.ItemKind   `json:"kinds"`
	Capabilities []domain.Capability `json:"capabilities"`
}

type addPluginJSON struct {
	// URL is where the plugin answers: photon's protocol's manifest is GET {url}/manifest; a
	// Stremio addon's URL is that of its manifest.json, as Stremio installs it.
	URL uri `json:"url"`
	// Protocol is what it speaks, photon unless said.
	Protocol domain.PluginProtocol `json:"protocol,omitzero"`
	// ID names a Stremio addon, where its own would be another's: two installs of one addon,
	// configured apart. A plugin speaking photon's protocol is the id its manifest gives.
	ID string `json:"id,omitzero"`
}

func pluginOf(r plugin.Registered) pluginJSON {
	return pluginJSON{
		ID: r.ID, Provider: domain.PluginSource(r.ID), Protocol: r.Protocol, URL: uri(r.URL), Name: r.Name, Version: r.Version,
		Kinds: r.Kinds, Capabilities: r.Capabilities,
	}
}

func (a *API) adminPlugins(r *http.Request, _ none) (listJSON[pluginJSON], error) {
	all, err := a.svc.Plugins.List(r.Context())
	if err != nil {
		return listJSON[pluginJSON]{}, err
	}
	out := make([]pluginJSON, len(all))
	for n, p := range all {
		out[n] = pluginOf(p)
	}
	return listJSON[pluginJSON]{Items: out}, nil
}

// addPlugin registers the plugin at an address, once its manifest is read and is one this server
// speaks. It is then a provider an admin sets and a library takes.
func (a *API) addPlugin(r *http.Request, req addPluginJSON) (pluginJSON, error) {
	p, err := a.svc.Plugins.Register(r.Context(), string(req.URL), cmp.Or(req.Protocol, domain.PluginPhoton), req.ID)
	return pluginOf(p), a.pluginChangedBy(err)
}

// refreshPlugin reads a plugin's manifest again.
func (a *API) refreshPlugin(r *http.Request, _ none) (pluginJSON, error) {
	p, err := a.svc.Plugins.Refresh(r.Context(), r.PathValue("slug"))
	return pluginOf(p), a.pluginChangedBy(err)
}

// removePlugin forgets a plugin. What it said about titles stands until another source says
// otherwise.
func (a *API) removePlugin(r *http.Request, _ none) (none, error) {
	return none{}, a.pluginChangedBy(a.svc.Plugins.Remove(r.Context(), r.PathValue("slug")))
}

// pluginChangedBy has the providers read again after a plugin's change, where err says it was made.
func (a *API) pluginChangedBy(err error) error {
	if err == nil {
		a.svc.Providers.Forget()
	}
	return err
}

type pluginPageJSON struct {
	Plugin string            `json:"plugin"`
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Access plugin.PageAccess `json:"access"`
}

type visitJSON struct {
	// URL is where the browser opens the page, with the visit's code in its fragment.
	URL uri `json:"url"`
}

type claimJSON struct {
	Code string `json:"code"`
}

type visitorJSON struct {
	Page    string      `json:"page"`
	Profile profileJSON `json:"profile"`
}

// pluginPages lists the plugins' pages the profile may visit, for the menus.
func (a *API) pluginPages(r *http.Request, _ none) (listJSON[pluginPageJSON], error) {
	all, err := a.svc.Plugins.Pages(r.Context(), auth.SessionOf(r.Context()).Profile)
	if err != nil {
		return listJSON[pluginPageJSON]{}, err
	}
	out := make([]pluginPageJSON, len(all))
	for n, p := range all {
		out[n] = pluginPageJSON{Plugin: p.Plugin, ID: p.ID, Name: p.Name, Access: p.Access}
	}
	return listJSON[pluginPageJSON]{Items: out}, nil
}

// visitPage starts the profile's visit to a plugin's page, as the plugin is told who visits by
// claiming its code: the page is on the plugin's own origin, which no token of the server's
// reaches.
func (a *API) visitPage(r *http.Request, _ none) (visitJSON, error) {
	at, err := a.svc.Plugins.Visit(r.Context(), auth.SessionOf(r.Context()).Profile, r.PathValue("slug"), r.PathValue("page"))
	return visitJSON{URL: uri(at)}, err
}

// claimVisit answers a plugin who visits its page, by the code the visit gave it, once.
func (a *API) claimVisit(r *http.Request, req claimJSON) (visitorJSON, error) {
	profile, page, err := a.svc.Plugins.Claim(r.Context(), r.PathValue("slug"), req.Code)
	if err != nil {
		return visitorJSON{}, err
	}
	return visitorJSON{Page: page, Profile: profileOf(profile)}, nil
}

func (a *API) pluginsRoutes() []route {
	pageParam := param{"page", "", "The page's id, as the plugin's manifest gives it."}
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/plugin-pages", access: signedIn, summary: "List the plugins' pages the profile may visit",
			status: http.StatusOK,
		}, a.pluginPages),
		a.handles(route{
			pattern: "POST /api/v1/plugins/{slug}/pages/{page}/visits", access: signedIn,
			summary: "Visit a plugin's page: the address to open it at, with a code the plugin claims",
			path:    []param{slugParam, pageParam}, status: http.StatusCreated,
		}, a.visitPage),
		a.handles(route{
			pattern: "POST /api/v1/plugins/{slug}/visits/claim", access: public,
			summary: "Claim a visit to a plugin's page by its code, once, for who visits",
			path:    []param{slugParam}, status: http.StatusOK,
		}, a.claimVisit),
		a.handles(route{
			pattern: "GET /api/v1/admin/plugins", access: admin, summary: "List the registered plugins",
			status: http.StatusOK,
		}, a.adminPlugins),
		a.handles(route{
			pattern: "POST /api/v1/admin/plugins", access: admin,
			summary: "Register the plugin or Stremio addon at an address, once its manifest is read",
			status:  http.StatusCreated,
		}, a.addPlugin),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/plugins/{slug}", access: admin,
			summary: "Forget a plugin; what it said about titles stays",
			path:    []param{slugParam}, status: http.StatusNoContent,
		}, a.removePlugin),
		a.handles(route{
			pattern: "POST /api/v1/admin/plugins/{slug}/refresh", access: admin, summary: "Read a plugin's manifest again",
			path: []param{slugParam}, status: http.StatusOK,
		}, a.refreshPlugin),
	}
}
