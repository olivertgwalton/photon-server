package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/plugin/pluginv1"
)

// PageAccess is who a plugin's page is for: the admins, or every profile signed in.
type PageAccess string

const (
	PageForAdmins   PageAccess = "admin"
	PageForEveryone PageAccess = "everyone"
)

func PageAccesses() []PageAccess {
	return []PageAccess{PageForAdmins, PageForEveryone}
}

// Page is a plugin's page, at the address a browser opens it at.
type Page struct {
	Plugin string
	ID     string
	Name   string
	URL    string
	Access PageAccess
}

// visitFor is how long a visit's code may be claimed: the moment a browser takes to open the page.
const visitFor = time.Minute

var (
	ErrNoPage = errors.New("plugin: no such page for this profile")
	// ErrUnknownVisit is a code no visit to the plugin has: unknown, expired or claimed already.
	ErrUnknownVisit = errors.New("plugin: no visit has that code")
)

type visits interface {
	StartVisit(ctx context.Context, codeHash []byte, v kv.Visit, ttl time.Duration) error
	TakeVisit(ctx context.Context, codeHash []byte) (kv.Visit, bool, error)
}

// pagesOf is the pages m names at the version of pages the server speaks, each address under base
// where it is a path; a page with no id, a second of an id, no name, an access the server has no
// name for, or an address that is not the web's, is passed over.
func pagesOf(m pluginv1.Manifest, base string) []Page {
	var out []Page
	for _, c := range m.Capabilities {
		if c.Name != string(domain.CapabilityPages) || c.Version != pluginv1.Speaks[c.Name] {
			continue
		}
		for _, pg := range c.Pages {
			address := pg.URL
			if strings.HasPrefix(address, "/") {
				address = base + address
			}
			u, err := url.Parse(address)
			_, slug := domain.PluginSource(pg.ID).Plugin()
			access := PageAccess(pg.Access)
			if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.Fragment != "" || !slug || pg.Name == "" ||
				!slices.Contains(PageAccesses(), access) || slices.ContainsFunc(out, func(p Page) bool { return p.ID == pg.ID }) {
				continue
			}
			out = append(out, Page{Plugin: m.ID, ID: pg.ID, Name: pg.Name, URL: u.String(), Access: access})
		}
	}
	return out
}

// Pages answers the plugins' pages a profile may visit, by plugin.
func (p *Plugins) Pages(ctx context.Context, profile domain.Profile) ([]Page, error) {
	all, err := p.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Page
	for _, r := range all {
		for _, pg := range r.Pages {
			if pg.Access == PageForEveryone || profile.Role == domain.RoleAdmin {
				out = append(out, pg)
			}
		}
	}
	return out, nil
}

// Visit answers the address a profile's browser opens a plugin's page at: the page's, with a code
// in its fragment, which no server is sent and no log keeps, that the plugin claims to know who
// visits.
func (p *Plugins) Visit(ctx context.Context, profile domain.Profile, plugin, page string) (string, error) {
	pages, err := p.Pages(ctx, profile)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(pages, func(pg Page) bool { return pg.Plugin == plugin && pg.ID == page })
	if i < 0 {
		return "", ErrNoPage
	}
	code := rand.Text()
	sum := sha256.Sum256([]byte(code))
	if err := p.visits.StartVisit(ctx, sum[:], kv.Visit{Plugin: plugin, Page: page, Profile: profile.ID}, visitFor); err != nil {
		return "", err
	}
	return pages[i].URL + "#photon_visit=" + code, nil
}

// Claim answers who visits a plugin's page by the code the visit gave, once, and which page.
func (p *Plugins) Claim(ctx context.Context, plugin, code string) (domain.Profile, string, error) {
	sum := sha256.Sum256([]byte(code))
	v, ok, err := p.visits.TakeVisit(ctx, sum[:])
	if err != nil {
		return domain.Profile{}, "", err
	}
	if !ok || v.Plugin != plugin {
		return domain.Profile{}, "", ErrUnknownVisit
	}
	profile, err := p.st.ProfileByID(ctx, v.Profile)
	return profile, v.Page, err
}
