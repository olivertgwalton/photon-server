package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type pluginAdmin interface {
	List(ctx context.Context) ([]plugin.Registered, error)
	Register(ctx context.Context, address string) (plugin.Registered, error)
	Refresh(ctx context.Context, slug string) (plugin.Registered, error)
	Remove(ctx context.Context, slug string) error
}

var slugParam = param{"slug", "", "The plugin's id, as its manifest gives it."}

type pluginJSON struct {
	ID string `json:"id"`
	// Provider is its id among the providers, and a library's sources.
	Provider     domain.FieldSource  `json:"provider"`
	URL          string              `json:"url"`
	Name         string              `json:"name"`
	Protocol     int                 `json:"protocol"`
	Kinds        []domain.ItemKind   `json:"kinds"`
	Capabilities []domain.Capability `json:"capabilities"`
}

type addPluginJSON struct {
	// URL is where the plugin answers; its manifest is GET {url}/manifest.
	URL string `json:"url"`
}

func pluginOf(r plugin.Registered) pluginJSON {
	m := r.Manifest
	j := pluginJSON{ID: m.ID, Provider: domain.PluginSource(m.ID), URL: r.URL, Name: m.Name, Protocol: m.Protocol}
	for _, k := range m.Kinds {
		j.Kinds = append(j.Kinds, domain.ItemKind(k))
	}
	for _, c := range m.Capabilities {
		j.Capabilities = append(j.Capabilities, domain.Capability(c))
	}
	return j
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

// addPlugin registers the metadata plugin at an address, once its manifest is read and is one
// this server speaks. It is then a provider an admin sets and a library takes.
func (a *API) addPlugin(w http.ResponseWriter, r *http.Request) {
	var req addPluginJSON
	if !a.decode(w, r, &req) {
		return
	}
	p, err := a.svc.Plugins.Register(r.Context(), req.URL)
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
	switch {
	case err == nil:
		a.svc.Providers.Forget()
		return false
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
	case errors.Is(err, store.ErrPluginExists):
		writeProblem(w, a.logger, codeConflict, "a plugin with that id is registered")
	case errors.Is(err, plugin.ErrRefused):
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
	case errors.Is(err, provider.ErrUnavailable):
		writeProblem(w, a.logger, codeProviderUnavailable, err.Error())
	default:
		a.internal(w, r, err)
	}
	return true
}
