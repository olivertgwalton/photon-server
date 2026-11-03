// Package ebook reads what an ebook's file says of it: an EPUB's package document (OPF) and a comic
// archive's ComicInfo.xml, each a zip read in place, as Calibre and Komga read them, and the cover
// either holds. A PDF is described by its name alone. Every archive is untrusted: a name that
// climbs out of it, an entry past maxEntries and a document past maxDocument are refused, and
// nothing is extracted.
package ebook

import (
	"archive/zip"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// maxEntries bounds the files an archive may hold, a comic's pages being the most.
	maxEntries = 10_000
	// maxDocument bounds the XML read of one, a package document being a few KiB.
	maxDocument = 1 << 20
	// MaxCover bounds the cover read of one.
	MaxCover = 32 << 20
)

// ErrUnsafe is an archive holding a name that climbs out of it, or more entries than maxEntries.
var ErrUnsafe = errors.New("ebook: an archive no reader should open")

// Info is what an ebook says of itself. Cover is the name of its cover within it, "" for none.
type Info struct {
	Title       string
	Authors     []string
	Series      string
	SeriesIndex float64
	Released    time.Time
	Year        int
	Description string
	Publisher   string
	Genres      []string
	ISBN        string
	Cover       string
}

// Read reads the ebook at path, an EPUB or a comic archive (CBZ), as its extension names.
func Read(name string) (Info, error) {
	z, err := open(name)
	if err != nil {
		return Info{}, err
	}
	defer z.Close()
	if strings.EqualFold(path.Ext(name), ".epub") {
		return readEPUB(&z.Reader)
	}
	return readComic(&z.Reader)
}

// Cover reads the cover named within the ebook at path.
func Cover(name, cover string) ([]byte, error) {
	z, err := open(name)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return readEntry(&z.Reader, cover, MaxCover)
}

func open(name string) (*zip.ReadCloser, error) {
	z, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}
	if len(z.File) > maxEntries || slices.ContainsFunc(z.File, func(f *zip.File) bool { return !filepath.IsLocal(f.Name) }) {
		return nil, errors.Join(ErrUnsafe, z.Close())
	}
	return z, nil
}

// readEntry reads an entry of at most limit bytes, whatever its header claims.
func readEntry(z *zip.Reader, name string, limit int64) ([]byte, error) {
	f, err := z.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("%w: %s is over %d bytes", ErrUnsafe, name, limit)
	}
	return b, err
}

func readXML(z *zip.Reader, name string, into any) error {
	b, err := readEntry(z, name, maxDocument)
	if err != nil {
		return err
	}
	return xml.Unmarshal(b, into)
}

type opf struct {
	Metadata struct {
		Titles      []string  `xml:"title"`
		Creators    []creator `xml:"creator"`
		Date        string    `xml:"date"`
		Description string    `xml:"description"`
		Publisher   string    `xml:"publisher"`
		Subjects    []string  `xml:"subject"`
		Identifiers []struct {
			Scheme string `xml:"scheme,attr"`
			Value  string `xml:",chardata"`
		} `xml:"identifier"`
		Metas []struct {
			Name     string `xml:"name,attr"`
			Content  string `xml:"content,attr"`
			Property string `xml:"property,attr"`
			Refines  string `xml:"refines,attr"`
			ID       string `xml:"id,attr"`
			Value    string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		Properties string `xml:"properties,attr"`
		MediaType  string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
}

type creator struct {
	ID   string `xml:"id,attr"`
	Role string `xml:"role,attr"`
	Name string `xml:",chardata"`
}

// readEPUB reads an EPUB's package document, which its container names, as EPUB 2 and 3 write it:
// its series as Calibre's meta names it or as an EPUB 3 collection with its place, its authors
// as creators of no role or the author's, and its cover as the item EPUB 3 marks or EPUB 2's meta
// names.
func readEPUB(z *zip.Reader) (Info, error) {
	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := readXML(z, "META-INF/container.xml", &container); err != nil {
		return Info{}, err
	}
	if len(container.Rootfiles) == 0 {
		return Info{}, errors.New("ebook: an EPUB with no package document")
	}
	at := container.Rootfiles[0].FullPath
	var p opf
	if err := readXML(z, at, &p); err != nil {
		return Info{}, err
	}
	m := p.Metadata
	var info Info
	if len(m.Titles) > 0 {
		info.Title = strings.TrimSpace(m.Titles[0])
	}
	info.Description, info.Publisher = strings.TrimSpace(m.Description), strings.TrimSpace(m.Publisher)
	info.Released, info.Year = dated(strings.TrimSpace(m.Date))
	for _, s := range m.Subjects {
		if s = strings.TrimSpace(s); s != "" {
			info.Genres = append(info.Genres, s)
		}
	}
	roles := map[string]string{}
	var coverID string
	for _, meta := range m.Metas {
		switch {
		case meta.Name == "calibre:series":
			info.Series = meta.Content
		case meta.Name == "calibre:series_index":
			info.SeriesIndex = number(meta.Content)
		case meta.Name == "cover":
			coverID = meta.Content
		case meta.Property == "belongs-to-collection":
			info.Series = cmp.Or(info.Series, strings.TrimSpace(meta.Value))
		case meta.Property == "group-position":
			info.SeriesIndex = cmp.Or(info.SeriesIndex, number(meta.Value))
		case meta.Property == "role":
			roles[strings.TrimPrefix(meta.Refines, "#")] = strings.TrimSpace(meta.Value)
		}
	}
	for _, c := range m.Creators {
		role := cmp.Or(c.Role, roles[c.ID])
		if name := strings.TrimSpace(c.Name); name != "" && (role == "" || role == "aut") {
			info.Authors = append(info.Authors, name)
		}
	}
	// An ISBN is an identifier whose scheme says so, or one written urn:isbn:.
	for _, id := range m.Identifiers {
		if strings.EqualFold(id.Scheme, "isbn") || strings.HasPrefix(strings.ToLower(id.Value), "urn:isbn:") {
			if info.ISBN = isbn(id.Value); info.ISBN != "" {
				break
			}
		}
	}
	for _, item := range p.Manifest {
		if strings.Contains(" "+item.Properties+" ", " cover-image ") || item.ID == coverID && coverID != "" {
			info.Cover = path.Join(path.Dir(at), item.Href)
			break
		}
	}
	return info, nil
}

// readComic reads a comic archive's ComicInfo.xml, as ComicRack writes it and Komga and Kavita
// read it, where it holds one, and takes its first page in name order as its cover.
func readComic(z *zip.Reader) (Info, error) {
	var info Info
	for _, f := range sortedNames(z) {
		if isPicture(f) {
			info.Cover = f
			break
		}
	}
	var ci struct {
		Title, Series, Number, Summary, Writer, Publisher, Genre string
		Year, Month, Day                                         int
	}
	err := readXML(z, "ComicInfo.xml", &ci)
	if errors.Is(err, fs.ErrNotExist) {
		return info, nil
	}
	if err != nil {
		return Info{}, err
	}
	info.Title, info.Series, info.SeriesIndex = ci.Title, ci.Series, number(ci.Number)
	info.Description, info.Publisher, info.Year = ci.Summary, ci.Publisher, ci.Year
	if ci.Year > 0 && ci.Month > 0 && ci.Day > 0 {
		info.Released = time.Date(ci.Year, time.Month(ci.Month), ci.Day, 0, 0, 0, 0, time.UTC)
	}
	for _, list := range []struct {
		into *[]string
		from string
	}{{&info.Authors, ci.Writer}, {&info.Genres, ci.Genre}} {
		for name := range strings.SplitSeq(list.from, ",") {
			if name = strings.TrimSpace(name); name != "" {
				*list.into = append(*list.into, name)
			}
		}
	}
	return info, nil
}

func sortedNames(z *zip.Reader) []string {
	names := make([]string, 0, len(z.File))
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return names
}

func isPicture(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return !strings.HasPrefix(path.Base(name), ".")
	}
	return false
}

// isbn is an identifier's ISBN, its digits alone, where it is one of 10 or 13.
func isbn(s string) string {
	var digits []byte
	for _, r := range strings.ToUpper(s) {
		if r >= '0' && r <= '9' || r == 'X' {
			digits = append(digits, byte(r))
		}
	}
	if len(digits) == 10 || len(digits) == 13 {
		return string(digits)
	}
	return ""
}

func number(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// dated reads a package document's date, "2007-07-21", "2007-07" or "2007" or a timestamp, as the
// day it names, if it names one, and its year.
func dated(s string) (time.Time, int) {
	if len(s) >= len(time.DateOnly) {
		if t, err := time.Parse(time.DateOnly, s[:len(time.DateOnly)]); err == nil {
			return t, t.Year()
		}
	}
	if len(s) >= 4 {
		if year, err := strconv.Atoi(s[:4]); err == nil {
			return time.Time{}, year
		}
	}
	return time.Time{}, 0
}
