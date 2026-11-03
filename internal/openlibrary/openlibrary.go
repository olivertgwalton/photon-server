// Package openlibrary reads Open Library, which describes a book, its work, and an author by OLID,
// and keeps their covers and photographs, as Jellyfin's Open Library provider and Audiobookshelf
// read it. It needs no key, but asks to be told who is asking, and to be asked gently.
package openlibrary

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const (
	baseURL   = "https://openlibrary.org/"
	coversURL = "https://covers.openlibrary.org/"
)

// every is how often Open Library is asked, as it asks a client that is not a person to keep to.
const every = time.Second

type Client struct {
	api    provider.Client
	covers string
	header http.Header
	pace   provider.Pace
}

// New is a client naming the server, at version, as Open Library asks of every client.
func New(version string, pauses provider.Pauses) *Client {
	return &Client{
		api: provider.Client{Name: "openlibrary", Base: baseURL, Pauses: pauses}, covers: coversURL,
		header: http.Header{"User-Agent": {"photon-server/" + version + " ( https://github.com/olivertgwalton/photon-server )"}},
		pace:   provider.Pace{Every: every},
	}
}

func (c *Client) Info() provider.Info {
	return provider.Info{ID: domain.SourceOpenLibrary, Name: "Open Library", Kinds: []domain.ItemKind{domain.ItemAuthor, domain.ItemBook}}
}

// edition is what a store's title of a book adds to its work's: "(Full-Cast Edition)", "[2/2]".
var edition = regexp.MustCompile(`\s*[(\[][^)\]]*[)\]]\s*$`)

// Match answers a book's or an author's OLID: the one it carries, else for a book the work its ISBN
// is an edition of, else the one Open Library's search finds of its name, a book by its author's
// too. Names are compared as letters and digits alone, "J.K. Rowling" being "J. K. Rowling"; of an
// author's namesakes, the one who wrote most is taken.
func (c *Client) Match(ctx context.Context, _ domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	if id := h.IDs[domain.ProviderOpenLibrary]; id != "" {
		return id, nil
	}
	switch kind {
	case domain.ItemBook:
		if isbn := h.IDs[domain.ProviderISBN]; isbn != "" {
			var e struct {
				Works []struct {
					Key string `json:"key"`
				} `json:"works"`
			}
			found, err := c.get(ctx, "isbn/"+url.PathEscape(isbn)+".json", nil, &e)
			if err != nil {
				return "", err
			}
			if found && len(e.Works) > 0 {
				return olid(e.Works[0].Key), nil
			}
		}
		title := edition.ReplaceAllString(h.Title, "")
		var s struct {
			Docs []struct {
				Key     string   `json:"key"`
				Title   string   `json:"title"`
				Authors []string `json:"author_name"`
			} `json:"docs"`
		}
		q := url.Values{"title": {title}, "limit": {"5"}, "fields": {"key,title,author_name"}}
		if h.Artist != "" {
			q.Set("author", h.Artist)
		}
		if _, err := c.get(ctx, "search.json", q, &s); err != nil {
			return "", err
		}
		for _, d := range s.Docs {
			if letters(d.Title) == letters(title) && (h.Artist == "" || slices.ContainsFunc(d.Authors, func(a string) bool { return letters(a) == letters(h.Artist) })) {
				return olid(d.Key), nil
			}
		}
	case domain.ItemAuthor:
		var s struct {
			Docs []struct {
				Key   string `json:"key"`
				Name  string `json:"name"`
				Works int    `json:"work_count"`
			} `json:"docs"`
		}
		if _, err := c.get(ctx, "search/authors.json", url.Values{"q": {h.Title}, "limit": {"10"}}, &s); err != nil {
			return "", err
		}
		best, most := "", -1
		for _, d := range s.Docs {
			if letters(d.Name) == letters(h.Title) && d.Works > most {
				best, most = d.Key, d.Works
			}
		}
		return olid(best), nil
	case domain.ItemMovie, domain.ItemShow, domain.ItemSeason, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection,
		domain.ItemArtist, domain.ItemAlbum, domain.ItemTrack:
	}
	return "", nil
}

// Describe answers what Open Library says of a book (its title, what it is about, its subjects and
// cover) or of an author (their biography, birth and photograph).
func (c *Client) Describe(ctx context.Context, _ domain.Locale, kind domain.ItemKind, id string, _ domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	m := domain.Metadata{IDs: map[domain.Provider]string{domain.ProviderOpenLibrary: id}}
	switch kind {
	case domain.ItemBook:
		var w struct {
			Title       string         `json:"title"`
			Description jsontext.Value `json:"description"`
			Subjects    []string       `json:"subjects"`
			Covers      []int          `json:"covers"`
		}
		if _, err := c.get(ctx, "works/"+url.PathEscape(id)+".json", nil, &w); err != nil {
			return domain.Metadata{}, nil, err
		}
		m.Title, m.Overview, m.Genres = w.Title, text(w.Description), w.Subjects[:min(len(w.Subjects), 5)]
		m.Artwork = c.pictures("b", w.Covers)
	case domain.ItemAuthor:
		var a struct {
			Name   string         `json:"name"`
			Bio    jsontext.Value `json:"bio"`
			Photos []int          `json:"photos"`
		}
		if _, err := c.get(ctx, "authors/"+url.PathEscape(id)+".json", nil, &a); err != nil {
			return domain.Metadata{}, nil, err
		}
		m.Title, m.Overview, m.Artwork = a.Name, text(a.Bio), c.pictures("a", a.Photos)
	case domain.ItemMovie, domain.ItemShow, domain.ItemSeason, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection,
		domain.ItemArtist, domain.ItemAlbum, domain.ItemTrack:
	}
	return m, nil, nil
}

// pictures are a book's covers or an author's photographs, of kind "b" or "a", at Open Library's
// large size, the first its own; an id below 1 is none.
func (c *Client) pictures(kind string, ids []int) []domain.Artwork {
	var out []domain.Artwork
	for _, id := range ids {
		if id > 0 {
			out = append(out, domain.Artwork{Kind: domain.ArtworkPoster, URL: c.covers + kind + "/id/" + strconv.Itoa(id) + "-L.jpg"})
		}
	}
	return out
}

// get asks Open Library for JSON, a second after it was last asked, answering false for what it has
// not.
func (c *Client) get(ctx context.Context, path string, q url.Values, into any) (bool, error) {
	if err := c.pace.Wait(ctx); err != nil {
		return false, err
	}
	err := c.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q, Header: c.header}, into)
	if errors.Is(err, provider.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// text is a description as Open Library writes one, a string or {"type": ..., "value": ...}, as
// plain text (see plain).
func text(v jsontext.Value) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return plain(s)
	}
	var typed struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(v, &typed) == nil {
		return plain(typed.Value)
	}
	return ""
}

var (
	// rule is the horizontal rule Open Library's librarians write a description's notes under,
	// "See also" lists of other editions.
	rule = regexp.MustCompile(`(?m)^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)
	// source is the link to where a description was taken from, "([source](https://…))".
	source = regexp.MustCompile(`\(\[[Ss]ource\]\([^)]*\)\)`)
	// link is a Markdown link, inline or by reference, and emphasis its asterisks or underscores.
	link     = regexp.MustCompile(`\[([^\]]*)\](?:\([^)]*\)|\[[^\]]*\])`)
	emphasis = regexp.MustCompile(`(\*{1,2}|_{1,2})([^*_\n]+)(\*{1,2}|_{1,2})`)
	blank    = regexp.MustCompile(`\n{3,}`)
)

// plain is Open Library's Markdown as the plain text a client shows: the description alone,
// without the notes under its rule or the link to its source, each link its words and each
// emphasis its text.
func plain(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	if loc := rule.FindStringIndex(md); loc != nil {
		md = md[:loc[0]]
	}
	md = source.ReplaceAllString(md, "")
	md = link.ReplaceAllString(md, "$1")
	md = emphasis.ReplaceAllString(md, "$2")
	md = strings.NewReplacer(`\[`, "[", `\]`, "]", `\*`, "*", `\_`, "_").Replace(md)
	return strings.TrimSpace(blank.ReplaceAllString(md, "\n\n"))
}

// olid is the OLID a key names: "/works/OL82586W" is OL82586W.
func olid(key string) string {
	return key[strings.LastIndex(key, "/")+1:]
}

// letters is a name as letters and digits alone, in lower case.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
