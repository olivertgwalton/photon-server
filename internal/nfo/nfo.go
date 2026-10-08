// Package nfo reads the Kodi NFO files that media managers write beside titles.
package nfo

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"io"
	"math"
	"regexp"
	"slices"
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
	IMDbIDSnake   string     `xml:"imdb_id"`
	TMDBID        string     `xml:"tmdbid"`
	TVDBID        string     `xml:"tvdbid"`
	SeasonNumber  string     `xml:"seasonnumber"`
	Season        string     `xml:"season"`
	Episode       string     `xml:"episode"`
	EpisodeEnd    string     `xml:"episodenumberend"`
	NamedSeasons  []struct {
		Number string `xml:"number,attr"`
		Name   string `xml:",chardata"`
	} `xml:"namedseason"`
	LockData     string `xml:"lockdata"`
	LockedFields string `xml:"lockedfields"`
}

// File is what an NFO says: a title's metadata, an episode's or season's numbers where it states
// them, and a series' names for its seasons.
type File struct {
	domain.Metadata
	Season      *int
	Episodes    []int
	SeasonNames map[int]string
}

// lockable maps Jellyfin's lockable field names to the fields they cover.
var lockable = map[string][]domain.Field{
	"name":           {domain.FieldTitle, domain.FieldSortTitle, domain.FieldOriginalTitle},
	"overview":       {domain.FieldOverview, domain.FieldTagline},
	"genres":         {domain.FieldGenres},
	"studios":        {domain.FieldStudios},
	"officialrating": {domain.FieldCertificate},
}

type uniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// Read parses an NFO as Jellyfin does. The first element is the title's, except that a file
// holding several <episodedetails> is one file of several episodes: it is the lowest-numbered,
// with every title and plot joined by " / " and numbered to the highest. Anything after the
// elements is read only for provider links, and a file that is not XML at all is a list of links.
func Read(r io.Reader) (File, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSize))
	if err != nil {
		return File{}, err
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
	var docs []document
	var end int64
	// A syntax error before any element means the file is not XML; its links are all it says.
	for tok, err := d.Token(); err == nil; tok, err = d.Token() {
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if len(docs) > 0 && (start.Name.Local != "episodedetails" || docs[0].XMLName.Local != "episodedetails") {
			break
		}
		var doc document
		if err := d.DecodeElement(&doc, &start); err != nil {
			return File{}, err
		}
		docs = append(docs, doc)
		end = d.InputOffset()
	}
	var f File
	if len(docs) > 0 {
		f = merged(docs)
	}
	// The offset counts decoded bytes, which run ahead of a Latin-1 file's own.
	addLinks(&f.Metadata, data[min(end, int64(len(data))):])
	return f, nil
}

func merged(docs []document) File {
	files := make([]File, len(docs))
	for i, doc := range docs {
		files[i] = doc.file()
	}
	slices.SortStableFunc(files, func(a, b File) int {
		return cmp.Compare(first(a.Episodes), first(b.Episodes))
	})
	f := files[0]
	join := func(get func(File) string) string {
		var parts []string
		for _, g := range files {
			if v := get(g); v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, " / ")
	}
	f.Title = join(func(g File) string { return g.Title })
	f.OriginalTitle = join(func(g File) string { return g.OriginalTitle })
	f.Overview = join(func(g File) string { return g.Overview })
	if last := files[len(files)-1].Episodes; len(f.Episodes) > 0 && len(last) > 0 && last[len(last)-1] > f.Episodes[0] {
		f.Episodes = []int{f.Episodes[0], last[len(last)-1]}
	}
	return f
}

func first(ns []int) int {
	if len(ns) == 0 {
		return math.MaxInt
	}
	return ns[0]
}

// number reads an NFO number; Kodi writes -1 for one it does not know.
func number(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 0
}

func (doc document) file() File {
	f := File{Metadata: doc.metadata()}
	if n, ok := number(cmp.Or(doc.SeasonNumber, doc.Season)); ok {
		f.Season = &n
	}
	if n, ok := number(doc.Episode); ok {
		f.Episodes = []int{n}
		if end, ok := number(doc.EpisodeEnd); ok && end > n {
			f.Episodes = append(f.Episodes, end)
		}
	}
	for _, s := range doc.NamedSeasons {
		if n, ok := number(s.Number); ok && strings.TrimSpace(s.Name) != "" {
			if f.SeasonNames == nil {
				f.SeasonNames = map[int]string{}
			}
			f.SeasonNames[n] = strings.TrimSpace(s.Name)
		}
	}
	if strings.EqualFold(strings.TrimSpace(doc.LockData), "true") {
		f.Locked = domain.Fields()
	} else {
		for name := range strings.SplitSeq(doc.LockedFields, "|") {
			f.Locked = append(f.Locked, lockable[strings.ToLower(strings.TrimSpace(name))]...)
		}
	}
	return f
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
	setID(&m, domain.ProviderIMDb, doc.IMDbIDSnake)
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
