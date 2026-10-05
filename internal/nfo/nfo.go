// Package nfo reads the Kodi NFO files that media managers write beside titles.
package nfo

import (
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// maxSize bounds what is read; NFOs with embedded thumbnails run to a few hundred KiB.
const maxSize = 4 << 20

type document struct {
	XMLName       xml.Name
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle"`
	SortTitle     string     `xml:"sorttitle"`
	Plot          string     `xml:"plot"`
	Outline       string     `xml:"outline"`
	Tagline       string     `xml:"tagline"`
	MPAA          string     `xml:"mpaa"`
	Premiered     string     `xml:"premiered"`
	ReleaseDate   string     `xml:"releasedate"`
	Aired         string     `xml:"aired"`
	Year          string     `xml:"year"`
	Genres        []string   `xml:"genre"`
	Studios       []string   `xml:"studio"`
	UniqueIDs     []uniqueID `xml:"uniqueid"`
	ID            string     `xml:"id"`
	IMDbID        string     `xml:"imdbid"`
	IMDbIDLegacy  string     `xml:"imdb_id"`
	TMDBID        string     `xml:"tmdbid"`
	TVDBID        string     `xml:"tvdbid"`
}

type uniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// Read parses an NFO as Jellyfin does: the first element is the title's (a multi-episode file's
// first <episodedetails>), anything after it is read only for provider links, and a file that is
// not XML at all is a list of links.
func Read(r io.Reader) (domain.Metadata, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSize))
	if err != nil {
		return domain.Metadata{}, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	d.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		e, err := htmlindex.Get(label)
		if err != nil {
			return nil, err
		}
		return e.NewDecoder().Reader(input), nil
	}
	// A syntax error before any element means the file is not XML; its links are all it says.
	for tok, err := d.Token(); err == nil; tok, err = d.Token() {
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var doc document
		if err := d.DecodeElement(&doc, &start); err != nil {
			return domain.Metadata{}, err
		}
		m := doc.metadata()
		// The offset counts decoded bytes, which run ahead of a Latin-1 file's own.
		addLinks(&m, data[min(d.InputOffset(), int64(len(data))):])
		return m, nil
	}
	var m domain.Metadata
	addLinks(&m, data)
	return m, nil
}

func (doc document) metadata() domain.Metadata {
	m := domain.Metadata{
		Title:         strings.TrimSpace(doc.Title),
		SortTitle:     strings.TrimSpace(doc.SortTitle),
		OriginalTitle: strings.TrimSpace(doc.OriginalTitle),
		Overview:      strings.TrimSpace(doc.Plot),
		Tagline:       strings.TrimSpace(doc.Tagline),
		Certificate:   strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(doc.MPAA), "Rated ")),
	}
	if m.Overview == "" {
		m.Overview = strings.TrimSpace(doc.Outline)
	}
	for _, s := range []string{doc.Premiered, doc.ReleaseDate, doc.Aired} {
		if t, err := time.Parse(time.DateOnly, strings.TrimSpace(s)); err == nil {
			m.ReleaseDate = t
			break
		}
	}
	if y, err := strconv.Atoi(strings.TrimSpace(doc.Year)); err == nil && y > 0 {
		m.Year = y
	} else if !m.ReleaseDate.IsZero() {
		m.Year = m.ReleaseDate.Year()
	}
	for _, g := range doc.Genres {
		m.Genres = append(m.Genres, names(g, "/|")...)
	}
	for _, s := range doc.Studios {
		m.Studios = append(m.Studios, names(s, "")...)
	}

	for _, u := range doc.UniqueIDs {
		if p, ok := provider(u.Type); ok {
			setID(&m, p, u.Value)
		}
	}
	setID(&m, domain.ProviderIMDb, doc.IMDbID)
	setID(&m, domain.ProviderIMDb, doc.IMDbIDLegacy)
	setID(&m, domain.ProviderTMDB, doc.TMDBID)
	setID(&m, domain.ProviderTVDB, doc.TVDBID)
	// <id> predates <uniqueid>: an IMDb id wherever it starts tt, else the scraper's own, which
	// was TVDB's for series and TMDB's for films.
	if id := strings.TrimSpace(doc.ID); id != "" {
		switch {
		case strings.HasPrefix(id, "tt"):
			setID(&m, domain.ProviderIMDb, id)
		case doc.XMLName.Local == "movie":
			setID(&m, domain.ProviderTMDB, id)
		default:
			setID(&m, domain.ProviderTVDB, id)
		}
	}
	return m
}

func provider(name string) (domain.Provider, bool) {
	for _, p := range domain.Providers() {
		if strings.EqualFold(name, string(p)) {
			return p, true
		}
	}
	return "", false
}

// setID keeps the first id given for a provider.
func setID(m *domain.Metadata, p domain.Provider, value string) {
	value = strings.TrimSpace(value)
	if _, ok := m.IDs[p]; ok || value == "" {
		return
	}
	if m.IDs == nil {
		m.IDs = map[domain.Provider]string{}
	}
	m.IDs[p] = value
}

func names(s, separators string) []string {
	var out []string
	for part := range strings.FieldsFuncSeq(s, func(r rune) bool { return strings.ContainsRune(separators, r) }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

var (
	imdbLink = regexp.MustCompile(`\btt\d{7,}\b`)
	tmdbLink = regexp.MustCompile(`themoviedb\.org/(?:movie|tv)/(\d+)`)
	tvdbLink = regexp.MustCompile(`thetvdb\.com/\S*?[?&]id=(\d+)`)
)

// addLinks takes provider ids from links in free text, as media managers append them.
func addLinks(m *domain.Metadata, text []byte) {
	if id := imdbLink.Find(text); id != nil {
		setID(m, domain.ProviderIMDb, string(id))
	}
	if id := tmdbLink.FindSubmatch(text); id != nil {
		setID(m, domain.ProviderTMDB, string(id[1]))
	}
	if id := tvdbLink.FindSubmatch(text); id != nil {
		setID(m, domain.ProviderTVDB, string(id[1]))
	}
}
