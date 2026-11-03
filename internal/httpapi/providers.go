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
	List(ctx context.Context, source domain.FieldSource, id string) ([]domain.Listed, error)
	Lists(ctx context.Context, source domain.FieldSource, query string) ([]domain.KeptList, error)
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
	// MetadataKinds and ImageKinds are the kinds of item a library may rank it for, as a metadata
	// downloader and as an image fetcher.
	MetadataKinds []domain.ItemKind `json:"metadata_kinds"`
	ImageKinds    []domain.ItemKind `json:"image_kinds"`
}

// adminProviders lists the metadata providers the server has, what each can do, and what is set of
// what each needs, as Jellyfin's plugin settings show them.
func (a *API) adminProviders(r *http.Request, _ none) (listJSON[metadataProviderJSON], error) {
	all, err := a.svc.Providers.All(r.Context())
	if err != nil {
		return listJSON[metadataProviderJSON]{}, err
	}
	out := make([]metadataProviderJSON, 0, len(all))
	for _, p := range all {
		j, err := a.provider(r.Context(), p)
		if err != nil {
			return listJSON[metadataProviderJSON]{}, err
		}
		out = append(out, j)
	}
	return listJSON[metadataProviderJSON]{Items: out}, nil
}

type providerChangeJSON struct {
	Settings map[string]string `json:"settings"`
}

// setProvider changes a provider's settings: each value given replaces what was set, and "" clears
// it. Only the settings it declares are taken.
func (a *API) setProvider(r *http.Request, req providerChangeJSON) (metadataProviderJSON, error) {
	p, ok, err := a.svc.Providers.Get(r.Context(), domain.FieldSource(r.PathValue("id")))
	if err != nil {
		return metadataProviderJSON{}, err
	}
	if !ok {
		return metadataProviderJSON{}, refuse(codeNotFound, "")
	}
	for key := range req.Settings {
		if !slices.ContainsFunc(p.Info().Settings, func(s provider.Setting) bool { return s.Key == key }) {
			return metadataProviderJSON{}, refuse(codeInvalidBody, key+" is not one of "+string(p.Info().ID)+"'s settings")
		}
	}
	if err := a.svc.ProviderSettings.SetProviderSettings(r.Context(), p.Info().ID, req.Settings); err != nil {
		return metadataProviderJSON{}, err
	}
	return a.provider(r.Context(), p)
}

func (a *API) provider(ctx context.Context, p provider.Provider) (metadataProviderJSON, error) {
	info := p.Info()
	can := provider.Capabilities(p)
	j := metadataProviderJSON{
		ID: info.ID, Name: info.Name, Kinds: info.Kinds, Capabilities: can, Ready: true,
		MetadataKinds: rankedFor(info, can, domain.FetcherMetadata), ImageKinds: rankedFor(info, can, domain.FetcherImages),
	}
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

// rankedFor answers the kinds of item a library may rank a provider for to fetch f: a built-in's
// as the server checks them, and a plugin's for the titles it knows and their seasons and episodes
// where it describes them, or for the titles' metadata alone where it only rates them.
func rankedFor(info provider.Info, can []domain.Capability, f domain.Fetcher) []domain.ItemKind {
	var out []domain.ItemKind
	if _, plugin := info.ID.Plugin(); !plugin {
		// Every kind any library ranks sources for: a music library's and a books library's too.
		for _, lib := range domain.LibraryKinds() {
			for _, kind := range lib.ItemKinds() {
				if !slices.Contains(out, kind) && slices.Contains(domain.Fetchable(f, kind), info.ID) {
					out = append(out, kind)
				}
			}
		}
		return out
	}
	for _, kind := range domain.LibraryMixed.ItemKinds() {
		title := domain.ItemShow
		if kind == domain.ItemMovie {
			title = domain.ItemMovie
		}
		rates := f == domain.FetcherMetadata && kind == title && slices.Contains(can, domain.CapabilityRate)
		if slices.Contains(info.Kinds, title) && (slices.Contains(can, domain.CapabilityDescribe) || rates) {
			out = append(out, kind)
		}
	}
	return out
}

type libraryKindOptionsJSON struct {
	Kind domain.LibraryKind `json:"kind"`
	// Defaults are how a new library of the kind is kept, of files and remote, until an admin says
	// otherwise. Its sources rank one for each kind of item it holds, its titles' first.
	Defaults []newLibraryJSON `json:"defaults"`
	// Fetchable are, for each kind of item it holds, the built-in sources it may rank for its
	// metadata and its pictures. A plugin may be ranked for what its provider's kinds say.
	Fetchable []fetchableJSON `json:"fetchable"`
}

type newLibraryJSON struct {
	Media domain.LibraryMedia `json:"media"`
	librarySettingsJSON
}

type fetchableJSON struct {
	Kind     domain.ItemKind      `json:"kind"`
	Metadata []domain.FieldSource `json:"metadata"`
	Images   []domain.FieldSource `json:"images"`
}

// libraryKinds lists the kinds of library and the options of each, as Jellyfin's library options
// list each type's metadata and image fetchers.
func (a *API) libraryKinds(_ *http.Request, _ none) (listJSON[libraryKindOptionsJSON], error) {
	var out []libraryKindOptionsJSON
	for _, lib := range domain.LibraryKinds() {
		j := libraryKindOptionsJSON{Kind: lib}
		for _, media := range domain.LibraryMedias() {
			j.Defaults = append(j.Defaults, newLibraryJSON{media, librarySettings(domain.NewLibrary(lib, media))})
		}
		for _, k := range domain.DefaultSources(lib) {
			j.Fetchable = append(j.Fetchable, fetchableJSON{
				Kind:     k.Kind,
				Metadata: domain.Fetchable(domain.FetcherMetadata, k.Kind),
				Images:   domain.Fetchable(domain.FetcherImages, k.Kind),
			})
		}
		out = append(out, j)
	}
	return listJSON[libraryKindOptionsJSON]{Items: out}, nil
}

func (a *API) providersRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/providers", access: admin,
			summary: "List the metadata providers, what each can do and needs set",
			status:  http.StatusOK,
		}, a.adminProviders),
		a.handles(route{
			pattern: "GET /api/v1/admin/library-kinds", access: admin,
			summary: "List the kinds of library, how a new one of each is kept, and the sources it may rank",
			status:  http.StatusOK,
		}, a.libraryKinds),
		a.handles(route{
			pattern: "PATCH /api/v1/admin/providers/{id}", access: admin, summary: "Change a provider's settings",
			path:   []param{{"id", domain.FieldSource(""), "The provider."}},
			status: http.StatusOK,
		}, a.setProvider),
	}
}
