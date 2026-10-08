package historyimport

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// plexPage is how many items one request to Plex lists.
const plexPage = 500

// Plex's item types, as a section's items are listed by.
const (
	plexMovie   = 1
	plexShow    = 2
	plexEpisode = 4
)

// plex reads what the token's own account has watched: Plex keeps every account's apart, and a
// server's token is its owner's.
type plex struct {
	base, token string
}

type plexContainer struct {
	MediaContainer struct {
		TotalSize int            `json:"totalSize"`
		Directory []plexSection  `json:"Directory"`
		Metadata  []plexMetadata `json:"Metadata"`
	} `json:"MediaContainer"`
}

type plexSection struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

type plexMetadata struct {
	RatingKey            string `json:"ratingKey"`
	Title                string `json:"title"`
	GrandparentTitle     string `json:"grandparentTitle"`
	GrandparentRatingKey string `json:"grandparentRatingKey"`
	ParentIndex          int    `json:"parentIndex"`
	Index                int    `json:"index"`
	ViewCount            int    `json:"viewCount"`
	ViewOffset           int64  `json:"viewOffset"`
	LastViewedAt         int64  `json:"lastViewedAt"`
	// GUID is Plex's own id, named so that encoding/json, which matches names in any case, never
	// reads it into Guids.
	GUID  string `json:"guid"`
	Guids []struct {
		ID string `json:"id"`
	} `json:"Guid"`
}

// ids are a title's provider ids, which Plex writes as tmdb://603.
func (m plexMetadata) ids() map[domain.Provider]string {
	all := map[string]string{}
	for _, g := range m.Guids {
		if p, v, ok := strings.Cut(g.ID, "://"); ok {
			all[p] = v
		}
	}
	return providerIDs(all)
}

func (p plex) get(ctx context.Context, path string, query url.Values, out any) error {
	header := http.Header{
		"X-Plex-Token": {p.token}, "X-Plex-Client-Identifier": {"photon-server-history-import"},
		"X-Plex-Product": {"photon-server"}, "X-Plex-Version": {"1"},
	}
	return provider.Client{Name: string(domain.ImportPlex), Base: p.base, HTTP: client}.
		Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: query, Header: header}, out)
}

func (p plex) connect(ctx context.Context, c Credentials) (store.ImportLogin, error) {
	if c.Token == "" {
		return store.ImportLogin{}, fmt.Errorf("%w: a Plex server is signed in to by a token", ErrRefused)
	}
	p.token = c.Token
	_, err := p.sections(ctx)
	return store.ImportLogin{Token: c.Token}, err
}

func (plex) signOut(context.Context) error { return nil }

func (p plex) sections(ctx context.Context) ([]plexSection, error) {
	var c plexContainer
	err := p.get(ctx, "/library/sections", nil, &c)
	return c.MediaContainer.Directory, err
}

// all lists every item of a type in a section, a page at a time.
func (p plex) all(ctx context.Context, section string, kind int) ([]plexMetadata, error) {
	var out []plexMetadata
	for {
		var c plexContainer
		err := p.get(ctx, "/library/sections/"+url.PathEscape(section)+"/all", url.Values{
			"type": {strconv.Itoa(kind)}, "includeGuids": {"1"},
			"X-Plex-Container-Start": {strconv.Itoa(len(out))}, "X-Plex-Container-Size": {strconv.Itoa(plexPage)},
		}, &c)
		if err != nil {
			return nil, err
		}
		out = append(out, c.MediaContainer.Metadata...)
		if len(c.MediaContainer.Metadata) == 0 || len(out) >= c.MediaContainer.TotalSize {
			return out, nil
		}
	}
}

func (p plex) entries(ctx context.Context) ([]entry, error) {
	sections, err := p.sections(ctx)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, s := range sections {
		switch s.Type {
		case "movie":
			films, err := p.all(ctx, s.Key, plexMovie)
			if err != nil {
				return nil, err
			}
			for _, f := range films {
				out = appendPlex(out, f, entry{title: f.Title, kind: domain.ItemMovie, ids: f.ids()})
			}
		case "show":
			shows, err := p.all(ctx, s.Key, plexShow)
			if err != nil {
				return nil, err
			}
			showIDs := map[string]map[domain.Provider]string{}
			for _, sh := range shows {
				showIDs[sh.RatingKey] = sh.ids()
			}
			episodes, err := p.all(ctx, s.Key, plexEpisode)
			if err != nil {
				return nil, err
			}
			for _, e := range episodes {
				out = appendPlex(out, e, entry{
					title: fmt.Sprintf("%s S%02dE%02d", e.GrandparentTitle, e.ParentIndex, e.Index),
					kind:  domain.ItemEpisode, ids: showIDs[e.GrandparentRatingKey], season: e.ParentIndex, episode: e.Index,
				})
			}
		}
	}
	return out, nil
}

// appendPlex adds an item the account has watched or started, as Plex has it: watched while its
// count of views is not zero, which marking it unwatched clears.
func appendPlex(out []entry, m plexMetadata, e entry) []entry {
	if m.ViewCount == 0 && m.ViewOffset == 0 {
		return out
	}
	e.plays, e.position = m.ViewCount, time.Duration(m.ViewOffset)*time.Millisecond
	if m.LastViewedAt > 0 {
		e.at = time.Unix(m.LastViewedAt, 0)
	}
	return append(out, e)
}
