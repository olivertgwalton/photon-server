// Package plugin calls providers that run as web services of their own, so anyone can add one, in
// any language, without rebuilding the server: those speaking the protocol in pluginv1, and Stremio
// addons. An admin registers a plugin by its address; it is then a provider like the built-in ones.
package plugin

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/stremio"
)

const (
	// callTimeout bounds each request to a plugin, the manifest's included.
	callTimeout = 20 * time.Second
	// maxSaid bounds how much of a plugin's own error is kept in the server's.
	maxSaid = 200
)

// ErrRefused is a plugin the server will not register: an address it will not call, or a manifest
// it does not speak.
var ErrRefused = errors.New("plugin refused")

// Plugins are the plugins an admin registered.
type Plugins struct {
	st   *store.Store
	http *http.Client
}

// calls follows no redirect, as a plugin is registered by the address it answers at: one would
// have the server ask another host, its own network's included, with the plugin's settings.
var calls = &http.Client{
	Timeout:       callTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func New(st *store.Store) *Plugins {
	return &Plugins{st: st, http: calls}
}

// Registered is a plugin as it last answered: what it speaks and is, and where it answers.
type Registered struct {
	ID       string
	Protocol domain.PluginProtocol
	// URL is where it answers: of a Stremio addon, the host alone, as the rest of its address is
	// its configuration.
	URL  string
	Name string
	// Version is the version of photon's protocol it speaks; none for an addon.
	Version      int
	Kinds        []domain.ItemKind
	Capabilities []domain.Capability
}

// List answers every registered plugin, by its id.
func (p *Plugins) List(ctx context.Context) ([]Registered, error) {
	rows, err := p.st.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Registered, len(rows))
	for n, row := range rows {
		if out[n], _, err = p.read(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Load answers a provider for each registered plugin, for the registry.
func (p *Plugins) Load(ctx context.Context) ([]provider.Provider, error) {
	rows, err := p.st.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Provider, len(rows))
	for n, row := range rows {
		if _, out[n], err = p.read(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// read is a registered plugin, and it as a provider, from its manifest as it answered it.
func (p *Plugins) read(row store.Plugin) (Registered, provider.Provider, error) {
	source := domain.PluginSource(row.Slug)
	switch row.Protocol {
	case domain.PluginStremio:
		var m stremio.Manifest
		if err := json.Unmarshal(row.Manifest, &m); err != nil {
			return Registered{}, nil, fmt.Errorf("plugin %s: %w", row.Slug, err)
		}
		addon := stremio.New(source, row.URL, m, p.http)
		r := Registered{ID: row.Slug, Protocol: row.Protocol, URL: origin(row.URL), Name: m.Name, Kinds: m.Kinds()}
		r.Capabilities = provider.Capabilities(addon)
		return r, addon, nil
	case domain.PluginPhoton:
	}
	var m pluginv1.Manifest
	if err := json.Unmarshal(row.Manifest, &m); err != nil {
		return Registered{}, nil, fmt.Errorf("plugin %s: %w", row.Slug, err)
	}
	c := &client{
		manifest: m, speaks: spoken(m), base: row.URL, http: p.http,
		settings: func(ctx context.Context) (map[string]string, error) { return p.st.ProviderSettings(ctx, source) },
	}
	r := Registered{ID: m.ID, Protocol: row.Protocol, URL: row.URL, Name: m.Name, Version: m.Protocol}
	for _, k := range m.Kinds {
		r.Kinds = append(r.Kinds, domain.ItemKind(k))
	}
	r.Capabilities = provider.Capabilities(c)
	return r, c, nil
}

// origin is an address's scheme and host alone.
func origin(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// Register reads the manifest a plugin answers at address and keeps it, or answers ErrRefused,
// provider.ErrUnavailable for a plugin that did not answer, or store.ErrPluginExists. A plugin
// speaking photon's protocol is registered by the address its paths are under, and is the id its
// manifest gives; a Stremio addon by the address of its manifest.json, and is the id given, else
// its own made a plugin id, as two installs of one addon, configured apart, need ids of their own.
func (p *Plugins) Register(ctx context.Context, address string, protocol domain.PluginProtocol, id string) (Registered, error) {
	switch protocol {
	case domain.PluginStremio:
		return p.registerAddon(ctx, address, id)
	case domain.PluginPhoton:
	}
	if id != "" {
		return Registered{}, fmt.Errorf("%w: a plugin is the id its manifest gives", ErrRefused)
	}
	base, err := baseURL(address)
	if err != nil {
		return Registered{}, err
	}
	m, err := p.manifest(ctx, base)
	if err != nil {
		return Registered{}, err
	}
	return p.add(ctx, store.Plugin{Slug: m.ID, Protocol: protocol, URL: base}, m)
}

func (p *Plugins) registerAddon(ctx context.Context, address, id string) (Registered, error) {
	base, err := stremio.Base(address)
	if err != nil {
		return Registered{}, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	m, err := p.addonManifest(ctx, base)
	if err != nil {
		return Registered{}, err
	}
	slug := cmp.Or(id, stremio.Slug(m.ID))
	if _, ok := domain.PluginSource(slug).Plugin(); !ok {
		return Registered{}, fmt.Errorf("%w: its id %q is not lower case letters, digits and hyphens", ErrRefused, slug)
	}
	return p.add(ctx, store.Plugin{Slug: slug, Protocol: domain.PluginStremio, URL: base}, m)
}

// add keeps a plugin and the manifest it answered.
func (p *Plugins) add(ctx context.Context, row store.Plugin, manifest any) (Registered, error) {
	var err error
	if row.Manifest, err = json.Marshal(manifest); err != nil {
		return Registered{}, err
	}
	if err := p.st.AddPlugin(ctx, row, hears(row, manifest)); err != nil {
		return Registered{}, err
	}
	r, _, err := p.read(row)
	return r, err
}

// Refresh reads a plugin's manifest again, as one that has learnt a capability or a setting
// answers a new one; a plugin's id may not change, and an addon's is what it was registered as.
func (p *Plugins) Refresh(ctx context.Context, slug string) (Registered, error) {
	row, err := p.st.Plugin(ctx, slug)
	if err != nil {
		return Registered{}, err
	}
	var manifest any
	switch row.Protocol {
	case domain.PluginStremio:
		if manifest, err = p.addonManifest(ctx, row.URL); err != nil {
			return Registered{}, err
		}
	case domain.PluginPhoton:
		m, err := p.manifest(ctx, row.URL)
		if err != nil {
			return Registered{}, err
		}
		if m.ID != slug {
			return Registered{}, fmt.Errorf("%w: it was registered as %q and now calls itself %q", ErrRefused, slug, m.ID)
		}
		manifest = m
	}
	if row.Manifest, err = json.Marshal(manifest); err != nil {
		return Registered{}, err
	}
	if err := p.st.SetPluginManifest(ctx, slug, row.Manifest, hears(row, manifest)); err != nil {
		return Registered{}, err
	}
	r, _, err := p.read(row)
	return r, err
}

// hears is the events a plugin is told of: none, of an addon.
func hears(row store.Plugin, manifest any) store.Hearing {
	if m, ok := manifest.(pluginv1.Manifest); ok {
		return hearing(m, row.URL)
	}
	return store.Hearing{}
}

// addonManifest reads an addon's manifest, as manifest reads a plugin's.
func (p *Plugins) addonManifest(ctx context.Context, base string) (stremio.Manifest, error) {
	m, err := stremio.Read(ctx, p.http, base)
	switch {
	case err == nil:
		return m, nil
	case errors.Is(err, provider.ErrUnreached):
		return m, fmt.Errorf("%w: %w", provider.ErrUnavailable, err)
	}
	return m, fmt.Errorf("%w: %w", ErrRefused, err)
}

// Remove forgets a plugin; what it said about titles stands.
func (p *Plugins) Remove(ctx context.Context, slug string) error {
	return p.st.RemovePlugin(ctx, slug)
}

// baseURL is the address a plugin's paths are added to: http or https, with no credentials, which
// belong in its settings, and no query.
func baseURL(address string) (string, error) {
	u, err := url.Parse(address)
	switch {
	case err != nil:
		return "", fmt.Errorf("%w: %w", ErrRefused, err)
	case u.Scheme != "http" && u.Scheme != "https", u.Host == "":
		return "", fmt.Errorf("%w: the address is an http or https URL", ErrRefused)
	case u.User != nil:
		return "", fmt.Errorf("%w: credentials go in the plugin's settings, not its address", ErrRefused)
	case u.RawQuery != "" || u.Fragment != "":
		return "", fmt.Errorf("%w: the address has no query or fragment", ErrRefused)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func (p *Plugins) manifest(ctx context.Context, base string) (pluginv1.Manifest, error) {
	m, err := call[struct{}, pluginv1.Manifest](ctx, p.http, "plugin at "+base, http.MethodGet, base, "/manifest", nil, nil)
	if err != nil {
		return m, err
	}
	return m, valid(m)
}

// valid refuses a manifest this server does not speak.
func valid(m pluginv1.Manifest) error {
	if m.Protocol != pluginv1.Version {
		return fmt.Errorf("%w: it speaks protocol %d and this server speaks %d", ErrRefused, m.Protocol, pluginv1.Version)
	}
	if _, ok := domain.PluginSource(m.ID).Plugin(); !ok {
		return fmt.Errorf("%w: its id %q is not lower case letters, digits and hyphens", ErrRefused, m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("%w: it has no name", ErrRefused)
	}
	if len(m.Kinds) == 0 {
		return fmt.Errorf("%w: it names no kinds", ErrRefused)
	}
	if len(spoken(m)) == 0 {
		return fmt.Errorf("%w: it answers no capability at a version this server speaks, of %v", ErrRefused, pluginv1.Speaks)
	}
	for _, k := range m.Kinds {
		if k := domain.ItemKind(k); k != domain.ItemMovie && k != domain.ItemShow {
			return fmt.Errorf("%w: kind %q is not movie or show", ErrRefused, k)
		}
	}
	keys := map[string]bool{}
	for _, s := range m.Settings {
		if s.Key == "" || s.Name == "" || keys[s.Key] {
			return fmt.Errorf("%w: setting %q has no name, or is listed twice", ErrRefused, s.Key)
		}
		keys[s.Key] = true
	}
	return nil
}

// call sends body as JSON, where there is one, and decodes the answer. A plugin that
// cannot be reached or fails on its side is provider.ErrUnavailable. An error names the plugin
// and never carries a secret setting, even one the plugin repeats.
func call[Req, Reply any](ctx context.Context, hc *http.Client, name, method, base, path string, body *Req, secrets []string) (Reply, error) {
	var reply Reply
	sent := provider.Request{Method: method, Path: path}
	if body != nil {
		sent.Body = body
	}
	err := provider.Client{Name: name, Base: base, HTTP: hc}.Do(ctx, sent, &reply)
	refusal, refused := errors.AsType[*provider.Refusal](err)
	switch {
	case err == nil:
		return reply, nil
	case ctx.Err() != nil:
		return reply, ctx.Err()
	case errors.Is(err, provider.ErrUnreached):
		return reply, fmt.Errorf("%w: %w", provider.ErrUnavailable, err)
	case refused && refusal.Code >= http.StatusInternalServerError:
		return reply, fmt.Errorf("%s %s: %w: %s", name, path, provider.ErrUnavailable, said(refusal, secrets))
	case refused:
		return reply, fmt.Errorf("%s %s: %s", name, path, said(refusal, secrets))
	}
	return reply, err
}

// said is a plugin's error: its status, and the title and detail of the problem it answered.
func said(r *provider.Refusal, secrets []string) string {
	var p pluginv1.Problem
	if json.Unmarshal(r.Body, &p) != nil || p.Title == "" {
		return r.Status
	}
	s := r.Status + ": " + p.Title
	if p.Detail != "" {
		s += ": " + p.Detail
	}
	for _, secret := range secrets {
		s = strings.ReplaceAll(s, secret, "[secret]")
	}
	if len(s) > maxSaid {
		s = strings.ToValidUTF8(s[:maxSaid], "") + "…"
	}
	return s
}
