package animelist

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// The lists, as they are published.
const (
	// PlacesURL is the Anime-Lists project's: where each AniDB entry falls on TheTVDB and TMDB.
	PlacesURL = "https://raw.githubusercontent.com/Anime-Lists/anime-lists/master/anime-list-full.xml"
	// IDsURL is Fribb's, joining AniDB's ids to AniList's.
	IDsURL = "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-full.json"
)

type placesXML struct {
	Anime []struct {
		AniDB      int    `xml:"anidbid,attr"`
		TVDB       string `xml:"tvdbid,attr"`
		TVDBSeason string `xml:"defaulttvdbseason,attr"`
		TVDBOffset int    `xml:"episodeoffset,attr"`
		TMDBShow   string `xml:"tmdbtv,attr"`
		TMDBSeason string `xml:"tmdbseason,attr"`
		TMDBOffset int    `xml:"tmdboffset,attr"`
		TMDBMovies string `xml:"tmdbid,attr"`
		IMDb       string `xml:"imdbid,attr"`
		Mappings   []struct {
			AniDBSeason int    `xml:"anidbseason,attr"`
			TVDBSeason  *int   `xml:"tvdbseason,attr"`
			TMDBSeason  *int   `xml:"tmdbseason,attr"`
			Start       *int   `xml:"start,attr"`
			End         int    `xml:"end,attr"`
			Offset      *int   `xml:"offset,attr"`
			Episodes    string `xml:",chardata"`
		} `xml:"mapping-list>mapping"`
	} `xml:"anime"`
}

// Read reads the two lists into entries: where each AniDB entry falls, from places, and its AniList
// entry, from ids.
func Read(places, ids io.Reader) ([]Entry, error) {
	var p placesXML
	if err := xml.NewDecoder(places).Decode(&p); err != nil {
		return nil, err
	}
	var joined []struct {
		AniDB   int `json:"anidb_id"`
		AniList int `json:"anilist_id"`
	}
	if err := json.NewDecoder(ids).Decode(&joined); err != nil {
		return nil, err
	}
	aniList := make(map[int]int, len(joined))
	for _, j := range joined {
		if j.AniDB != 0 {
			aniList[j.AniDB] = j.AniList
		}
	}
	out := make([]Entry, 0, len(p.Anime))
	for _, a := range p.Anime {
		e := Entry{
			AniDB: a.AniDB, AniList: aniList[a.AniDB],
			TVDB:       placing(a.TVDB, a.TVDBSeason, a.TVDBOffset),
			TMDB:       placing(a.TMDBShow, a.TMDBSeason, a.TMDBOffset),
			TMDBMovies: numbers(a.TMDBMovies),
			IMDb:       words(a.IMDb),
		}
		for _, m := range a.Mappings {
			for _, on := range []struct {
				p      domain.Provider
				season *int
			}{{domain.ProviderTVDB, m.TVDBSeason}, {domain.ProviderTMDB, m.TMDBSeason}} {
				if on.season == nil {
					continue
				}
				mapping := Mapping{Provider: on.p, Special: m.AniDBSeason == 0, Season: *on.season, Episodes: pairs(m.Episodes)}
				if m.Start != nil || m.Offset != nil {
					mapping.Range = &Range{Start: max(deref(m.Start), 1), End: m.End, Offset: deref(m.Offset)}
				}
				e.Mappings = append(e.Mappings, mapping)
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// placing reads where an entry falls in a show: none where the show is not a number, as an entry
// for a film or an OVA TheTVDB files apart has "movie" or "OVA", and its absolute order for a
// season of "a".
func placing(show, season string, offset int) Placing {
	n, err := strconv.Atoi(show)
	if err != nil || n == 0 {
		return Placing{}
	}
	if season == "a" {
		return Placing{Show: n, Season: Absolute, Offset: offset}
	}
	s, err := strconv.Atoi(season)
	if err != nil {
		return Placing{}
	}
	return Placing{Show: n, Season: s, Offset: offset}
}

// pairs reads a list of episodes placed one by one, ";1-27;2-3;16-31+32;6-0;": the entry's first
// is the show's 27th, its 16th the show's 31st and 32nd, and its 6th in none.
func pairs(s string) map[int][]int {
	out := map[int][]int{}
	for pair := range strings.SplitSeq(s, ";") {
		from, to, ok := strings.Cut(strings.TrimSpace(pair), "-")
		ep, err := strconv.Atoi(from)
		if !ok || err != nil {
			continue
		}
		var at []int
		for t := range strings.SplitSeq(to, "+") {
			if n, err := strconv.Atoi(t); err == nil && n != 0 {
				at = append(at, n)
			}
		}
		out[ep] = at
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func numbers(s string) []int {
	var out []int
	for _, w := range words(s) {
		if n, err := strconv.Atoi(w); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func words(s string) []string {
	var out []string
	for w := range strings.SplitSeq(s, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func deref(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}
