package httpapi

import (
	"context"
	"net/http"
	"slices"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type providerList interface {
	All(ctx context.Context) ([]provider.Provider, error)
	Get(ctx context.Context, id domain.FieldSource) (provider.Provider, bool, error)
	Forget()
}

type providerSettings interface {
	ProviderSettings(ctx context.Context, id domain.FieldSource) (map[string]string, error)
	SetProviderSettings(ctx context.Context, id domain.FieldSource, change map[string]string) error
}

type settingJSON struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Secret   bool   `json:"secret,omitzero"`
	Required bool   `json:"required,omitzero"`
	Set      bool   `json:"set"`
	// Value is what was set, never for a secret.
	Value string `json:"value,omitzero"`
}

type metadataProviderJSON struct {
	ID           domain.FieldSource  `json:"id"`
	Name         string              `json:"name"`
	Kinds        []domain.ItemKind   `json:"kinds"`
	Capabilities []domain.Capability `json:"capabilities"`
	Settings     []settingJSON       `json:"settings"`
	// Ready is whether every setting it needs is set.
	Ready bool `json:"ready"`
}

// adminProviders lists the metadata providers the server has, what each can do, and what is set of
// what each needs, as Jellyfin's plugin settings show them.
func (a *API) adminProviders(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.Providers.All(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]metadataProviderJSON, 0, len(all))
	for _, p := range all {
		j, err := a.provider(r.Context(), p)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		out = append(out, j)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[metadataProviderJSON]{Items: out})
}

type providerChangeJSON struct {
	Settings map[string]string `json:"settings"`
}

// setProvider changes a provider's settings: each value given replaces what was set, and "" clears
// it. Only the settings it declares are taken.
func (a *API) setProvider(w http.ResponseWriter, r *http.Request) {
	p, ok, err := a.svc.Providers.Get(r.Context(), domain.FieldSource(r.PathValue("id")))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if !ok {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	var req providerChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	for key := range req.Settings {
		if !slices.ContainsFunc(p.Info().Settings, func(s provider.Setting) bool { return s.Key == key }) {
			writeProblem(w, a.logger, codeInvalidBody, key+" is not one of "+string(p.Info().ID)+"'s settings")
			return
		}
	}
	if err := a.svc.ProviderSettings.SetProviderSettings(r.Context(), p.Info().ID, req.Settings); err != nil {
		a.internal(w, r, err)
		return
	}
	j, err := a.provider(r.Context(), p)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, j)
}

func (a *API) provider(ctx context.Context, p provider.Provider) (metadataProviderJSON, error) {
	info := p.Info()
	j := metadataProviderJSON{ID: info.ID, Name: info.Name, Kinds: info.Kinds, Capabilities: provider.Capabilities(p), Settings: []settingJSON{}, Ready: true}
	if len(info.Settings) == 0 {
		return j, nil
	}
	set, err := a.svc.ProviderSettings.ProviderSettings(ctx, info.ID)
	if err != nil {
		return metadataProviderJSON{}, err
	}
	for _, s := range info.Settings {
		v := set[s.Key]
		sj := settingJSON{Key: s.Key, Name: s.Name, Secret: s.Secret, Required: s.Required, Set: v != ""}
		if !s.Secret {
			sj.Value = v
		}
		j.Settings = append(j.Settings, sj)
		j.Ready = j.Ready && (v != "" || !s.Required)
	}
	return j, nil
}
