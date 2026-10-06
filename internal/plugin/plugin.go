// Package plugin calls metadata providers that run as web services of their own and speak the
// protocol in pluginv1, so anyone can add one, in any language, without rebuilding the server. An
// admin registers a plugin by its address; it is then a provider like the built-in ones.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
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

// Plugins are the metadata plugins an admin registered.
type Plugins struct {
	st   *store.Store
	http *http.Client
}

func New(st *store.Store) *Plugins {
	return &Plugins{st: st, http: &http.Client{Timeout: callTimeout}}
}

// Registered is a plugin, where it answers and the manifest it answered there.
type Registered struct {
	URL      string
	Manifest pluginv1.Manifest
}

// List answers every registered plugin, by its id.
func (p *Plugins) List(ctx context.Context) ([]Registered, error) {
	rows, err := p.st.Plugins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Registered, len(rows))
	for n, row := range rows {
		out[n].URL = row.URL
		if err := json.Unmarshal(row.Manifest, &out[n].Manifest); err != nil {
			return nil, fmt.Errorf("plugin %s: %w", row.Slug, err)
		}
	}
	return out, nil
}

// Load answers a provider for each registered plugin, for the registry.
func (p *Plugins) Load(ctx context.Context) ([]provider.Provider, error) {
	all, err := p.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Provider, len(all))
	for n, r := range all {
		source := domain.PluginSource(r.Manifest.ID)
		out[n] = &client{
			manifest: r.Manifest, base: r.URL, http: p.http,
			settings: func(ctx context.Context) (map[string]string, error) { return p.st.ProviderSettings(ctx, source) },
		}
	}
	return out, nil
}

// Register reads the manifest a plugin answers at address and keeps it, or answers ErrRefused,
// provider.ErrUnavailable for a plugin that did not answer, or store.ErrPluginExists.
func (p *Plugins) Register(ctx context.Context, address string) (Registered, error) {
	base, err := baseURL(address)
	if err != nil {
		return Registered{}, err
	}
	m, err := p.manifest(ctx, base)
	if err != nil {
		return Registered{}, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Registered{}, err
	}
	if err := p.st.AddPlugin(ctx, store.Plugin{Slug: m.ID, URL: base, Manifest: raw}); err != nil {
		return Registered{}, err
	}
	return Registered{URL: base, Manifest: m}, nil
}

// Refresh reads a plugin's manifest again, as one that has learnt a capability or a setting
// answers a new one; its id may not change.
func (p *Plugins) Refresh(ctx context.Context, slug string) (Registered, error) {
	row, err := p.st.Plugin(ctx, slug)
	if err != nil {
		return Registered{}, err
	}
	m, err := p.manifest(ctx, row.URL)
	if err != nil {
		return Registered{}, err
	}
	if m.ID != slug {
		return Registered{}, fmt.Errorf("%w: it was registered as %q and now calls itself %q", ErrRefused, slug, m.ID)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return Registered{}, err
	}
	if err := p.st.SetPluginManifest(ctx, slug, raw); err != nil {
		return Registered{}, err
	}
	return Registered{URL: row.URL, Manifest: m}, nil
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
	var m pluginv1.Manifest
	if err := call(ctx, p.http, "plugin at "+base, http.MethodGet, base+"/manifest", nil, &m, nil); err != nil {
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
	if len(m.Kinds) == 0 || len(m.Capabilities) == 0 {
		return fmt.Errorf("%w: it names no kinds or no capabilities", ErrRefused)
	}
	for _, k := range m.Kinds {
		if k := domain.ItemKind(k); k != domain.ItemMovie && k != domain.ItemShow {
			return fmt.Errorf("%w: kind %q is not movie or show", ErrRefused, k)
		}
	}
	for _, c := range m.Capabilities {
		if !slices.Contains(domain.Capabilities(), domain.Capability(c)) {
			return fmt.Errorf("%w: capability %q is not one of %v", ErrRefused, c, domain.Capabilities())
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

// call sends body as JSON, where there is one, and decodes the answer into out. A plugin that
// cannot be reached or fails on its side is provider.ErrUnavailable. An error names the plugin
// and never carries a secret setting, even one the plugin repeats.
func call(ctx context.Context, hc *http.Client, name, method, address string, body, out any, secrets []string) error {
	var sent io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		sent = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, sent)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	err = provider.Client{Name: name, HTTP: hc}.Do(req, out)
	refusal, refused := errors.AsType[*provider.Refusal](err)
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, provider.ErrUnreached):
		return fmt.Errorf("%w: %w", provider.ErrUnavailable, err)
	case refused && refusal.Code >= http.StatusInternalServerError:
		return fmt.Errorf("%s %s: %w: %s", name, req.URL.Path, provider.ErrUnavailable, said(refusal, secrets))
	case refused:
		return fmt.Errorf("%s %s: %s", name, req.URL.Path, said(refusal, secrets))
	}
	return err
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
