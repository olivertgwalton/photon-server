// Package stremio speaks to a Stremio addon, which an admin registers as a plugin by the address of
// its manifest, as Stremio installs one: its catalogs are lists a library's collections or titles
// are made of, and its streams the copies a remote library plays. Its configuration, a debrid service's key among it, is in its address, which no
// error or answer names.
package stremio

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// maxCatalogTitles is as many of a catalog's titles as are read: a catalog such as "popular" pages
// on for as long as it is asked.
const maxCatalogTitles = 500

var (
	// ErrNotManifest is an address that is not a manifest's, which ends in /manifest.json.
	ErrNotManifest = errors.New("stremio: an addon is registered by the address of its manifest.json")
	// ErrNoSuchCatalog is a catalog named otherwise than as type/catalog, of films or shows, or one
	// the addon does not have.
	ErrNoSuchCatalog = errors.New("stremio: a catalog is named as movie/id or series/id, one the addon lists")
	// ErrNoKinds is an addon of neither films nor shows.
	ErrNoKinds = errors.New("stremio: the addon has neither films nor shows")
)

// kinds are Stremio's types of title that are films and shows.
var kinds = map[string]domain.ItemKind{"movie": domain.ItemMovie, "series": domain.ItemShow}

// Manifest is as much of an addon's manifest as the server reads.
type Manifest struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Types    []string  `json:"types"`
	Catalogs []Catalog `json:"catalogs"`
	// Resources are what the addon answers: a name, or an object naming one.
	Resources []json.RawMessage `json:"resources"`
}

type Catalog struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Kinds are the kinds of title the addon has.
func (m Manifest) Kinds() []domain.ItemKind {
	var out []domain.ItemKind
	for _, t := range m.Types {
		if k, ok := kinds[t]; ok && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// Streams reports whether the addon answers streams.
func (m Manifest) Streams() bool {
	return slices.ContainsFunc(m.Resources, func(raw json.RawMessage) bool {
		var name string
		if json.Unmarshal(raw, &name) != nil {
			var named struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(raw, &named) != nil {
				return false
			}
			name = named.Name
		}
		return name == "stream"
	})
}

// Lists reports whether the addon has a catalog of films or shows.
func (m Manifest) Lists() bool {
	return slices.ContainsFunc(m.Catalogs, func(c Catalog) bool { _, ok := kinds[c.Type]; return ok })
}

// Base is the address an addon's resources are under, of the address of its manifest.
func Base(manifest string) (string, error) {
	u, err := url.Parse(manifest)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return "", ErrNotManifest
	}
	base, ok := strings.CutSuffix(u.String(), "/manifest.json")
	if !ok {
		return "", ErrNotManifest
	}
	return base, nil
}

// Read reads the manifest of the addon under base.
func Read(ctx context.Context, hc *http.Client, base string) (Manifest, error) {
	var m Manifest
	if err := api(base, hc).Do(ctx, provider.Request{Method: http.MethodGet, Path: "/manifest.json"}, &m); err != nil {
		return m, err
	}
	if len(m.Kinds()) == 0 {
		return m, ErrNoKinds
	}
	return m, nil
}

var unslugged = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is a plugin id made of an addon's own: lower case letters, digits and hyphens.
func Slug(id string) string {
	s := strings.Trim(unslugged.ReplaceAllString(strings.ToLower(id), "-"), "-")
	return strings.TrimRight(s[:min(len(s), 63)], "-")
}

func api(base string, hc *http.Client) provider.Client {
	return provider.Client{Name: "stremio addon", Base: base, HTTP: hc}
}

// Addon is a registered addon as a provider.
type Addon struct {
	id       domain.FieldSource
	manifest Manifest
	api      provider.Client
	// host is the addon's, as host:port, which its streams may name though it is not public:
	// Riven's, on the household's network, is.
	host string
}

func New(id domain.FieldSource, base string, m Manifest, hc *http.Client) *Addon {
	return &Addon{id: id, manifest: m, api: api(base, hc), host: domain.OfferedFrom(base)}
}

func (a *Addon) Info() provider.Info {
	return provider.Info{ID: a.id, Name: a.manifest.Name, Kinds: a.manifest.Kinds()}
}

// Answers lists where the addon has a catalog to list, and streams where it answers streams.
func (a *Addon) Answers(c domain.Capability) bool {
	switch c {
	case domain.CapabilityList:
		return a.manifest.Lists()
	case domain.CapabilityStream:
		return a.manifest.Streams()
	case domain.CapabilityDescribe, domain.CapabilitySearch, domain.CapabilityRate, domain.CapabilityPerson:
	}
	return false
}

// stream is a stream as an addon answers it.
type stream struct {
	URL         string `json:"url"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	InfoHash    string `json:"infoHash"`
	FileIdx     *int   `json:"fileIdx"`
	Hints       struct {
		Filename  string `json:"filename"`
		VideoSize int64  `json:"videoSize"`
		// ProxyHeaders are headers its URL is fetched with, which the server does not send.
		ProxyHeaders json.RawMessage `json:"proxyHeaders"`
	} `json:"behaviorHints"`
}

// Streams answers the streams the addon offers of a film or an episode, in its order, by the
// title's IMDb id, else its TMDB id; none for a title it is known by neither. A stream with no
// address of its own (a torrent alone, a YouTube video) or that needs headers sent with it is left
// out.
func (a *Addon) Streams(ctx context.Context, t domain.Streamed) ([]domain.Offer, error) {
	typ := "movie"
	if t.Kind == domain.ItemShow {
		typ = "series"
	}
	id := t.IDs[domain.ProviderIMDb]
	if id == "" && t.IDs[domain.ProviderTMDB] != "" {
		id = "tmdb:" + t.IDs[domain.ProviderTMDB]
	}
	if id == "" {
		return nil, nil
	}
	if t.Kind == domain.ItemShow {
		id += fmt.Sprintf(":%d:%d", t.Season, t.Episode)
	}
	var answer struct {
		Streams []stream `json:"streams"`
	}
	err := a.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: "/stream/" + typ + "/" + url.PathEscape(id) + ".json"}, &answer)
	if errors.Is(err, provider.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.manifest.Name, err)
	}
	var out []domain.Offer
	for _, s := range answer.Streams {
		if o, ok := offerOf(s, a.host); ok {
			out = append(out, o)
		}
	}
	return out, nil
}

// offerOf is a stream as an offer, keyed by what its bytes are as Remux keys them: a torrent's file
// by its hash and index, else a file by its name and size, else the release it names, else its
// address without the query, which a debrid link signs afresh each time it is offered. An addon
// whose addresses differ only in their query, as AltMount's /play?release= do, is told apart by
// the release. The stream's own name is no key: an addon renames a stream as it is cached.
func offerOf(s stream, from string) (domain.Offer, bool) {
	u, err := url.Parse(s.URL)
	headers := len(s.Hints.ProxyHeaders) > 0 && string(s.Hints.ProxyHeaders) != "null"
	if s.URL == "" || err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || headers {
		return domain.Offer{}, false
	}
	o := domain.Offer{Filename: s.Hints.Filename, Size: s.Hints.VideoSize, URL: u, From: from}
	release := cmp.Or(s.Hints.Filename, firstLine(s.Title), firstLine(s.Description))
	o.Name = cmp.Or(release, s.Name)
	switch {
	case s.InfoHash != "":
		o.Key = "torrent:" + strings.ToLower(s.InfoHash) + ":" + strconv.Itoa(deref(s.FileIdx))
	case s.Hints.Filename != "" && s.Hints.VideoSize > 0:
		o.Key = "file:" + s.Hints.Filename + ":" + strconv.FormatInt(s.Hints.VideoSize, 10)
	case release != "":
		o.Key = "release:" + release
	default:
		o.Key = "url:" + u.Host + u.EscapedPath()
	}
	return o, true
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// List answers a catalog's films or shows in its order, named as type/catalog: movie/top. A
// catalog pages by skip; an addon that does not page answers its first page again, or not found.
func (a *Addon) List(ctx context.Context, id string) ([]domain.Listed, error) {
	typ, catalog, _ := strings.Cut(id, "/")
	kind, ok := kinds[typ]
	if !ok || !slices.ContainsFunc(a.manifest.Catalogs, func(c Catalog) bool { return c.Type == typ && c.ID == catalog }) {
		return nil, ErrNoSuchCatalog
	}
	var out []domain.Listed
	seen := map[string]bool{}
	for len(out) < maxCatalogTitles {
		path := "/catalog/" + typ + "/" + url.PathEscape(catalog)
		if len(seen) > 0 {
			path += "/skip=" + strconv.Itoa(len(seen))
		}
		var page struct {
			Metas []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Name string `json:"name"`
				// ReleaseInfo is a year, or a show's years, as 2008-2013.
				ReleaseInfo string `json:"releaseInfo"`
			} `json:"metas"`
		}
		err := a.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path + ".json"}, &page)
		if len(seen) > 0 && errors.Is(err, provider.ErrNotFound) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.manifest.Name, err)
		}
		fresh := 0
		for _, m := range page.Metas {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			fresh++
			if ids := idsOf(m.ID); ids != nil && m.Type == typ {
				out = append(out, domain.Listed{Kind: kind, IDs: ids, Title: m.Name, Year: yearOf(m.ReleaseInfo)})
			}
		}
		// A page of nothing new is the end, as an addon that ignores skip answers its first again.
		if fresh == 0 {
			break
		}
	}
	return out[:min(len(out), maxCatalogTitles)], nil
}

// yearOf is the year a release is of, as Stremio writes it: 1995, or a show's 2008-2013 begun in
// 2008; zero where it says none.
func yearOf(release string) int {
	year, err := strconv.Atoi(release[:min(len(release), 4)])
	if err != nil {
		return 0
	}
	return year
}

// idsOf is a Stremio id as the providers' ids it is: an IMDb id, or tmdb:{id}; nil for an addon's
// own, which nothing else knows the title by.
func idsOf(id string) map[domain.Provider]string {
	if tmdb, ok := strings.CutPrefix(id, "tmdb:"); ok {
		if _, err := strconv.Atoi(tmdb); err == nil {
			return map[domain.Provider]string{domain.ProviderTMDB: tmdb}
		}
		return nil
	}
	if strings.HasPrefix(id, "tt") {
		if _, err := strconv.Atoi(id[2:]); err == nil {
			return map[domain.Provider]string{domain.ProviderIMDb: id}
		}
	}
	return nil
}
