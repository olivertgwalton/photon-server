// Package animelist reads the published anime lists, which say where each AniDB entry falls in a
// show as TheTVDB or TMDB numbers it, and places a show's episodes back in the entries, as
// Jellyfin's AniDB plugin and Plex's HAMA agent do. An anime database files each cour, film and
// OVA as an entry of its own, where TheTVDB and TMDB file one show of many seasons; the show keeps
// its shape, and each of its episodes is found in the entry it falls in.
package animelist

import (
	"maps"
	"slices"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Absolute is the season of a show numbered from its first episode to its last, which an entry
// placed in the show's absolute order is in.
const Absolute = -1

// Placing is where an entry's regular episodes fall in a show on one provider: its episode e is
// episode e+Offset of Season. A Show of 0 is no show there.
type Placing struct {
	Show   int
	Season int
	Offset int
}

// Range places an entry's episodes Start to End, every one from Start where End is 0, at their
// number plus Offset.
type Range struct {
	Start  int
	End    int
	Offset int
}

// Mapping places some of an entry's episodes in one season of a show on one provider, over its
// Placing: each listed in Episodes at the show's episodes it names, none where it names none, and
// those in Range by it.
type Mapping struct {
	Provider domain.Provider
	// Special maps the entry's specials, AniDB's season 0, rather than its regular episodes.
	Special  bool
	Season   int
	Episodes map[int][]int
	Range    *Range
}

// Entry is an AniDB entry, the AniList entry that is it, and where it falls in a show on TheTVDB
// and TMDB, or, for a film, the film it is on TMDB and IMDb.
type Entry struct {
	AniDB      int
	AniList    int
	TVDB       Placing
	TMDB       Placing
	TMDBMovies []int
	IMDb       []string
	Mappings   []Mapping
}

// Placing answers where the entry falls in a show on p.
func (e Entry) Placing(p domain.Provider) Placing {
	if p == domain.ProviderTVDB {
		return e.TVDB
	}
	return e.TMDB
}

// Place is one of an entry's episodes: its number among the entry's regular episodes, or among
// its specials.
type Place struct {
	Entry   Entry
	Episode int
	Special bool
}

// Episode answers which entry, of a show's, and which of its episodes is episode n of a season of
// the show as p numbers it; false for an episode no entry holds. An entry's own list of where its
// episodes go comes first, then its ranges, then where it says its episodes fall, the entry
// beginning latest before n taking it, as one cour follows another in a season. The lists say
// nothing of how many episodes an entry has, so one past the last entry's last is answered as in
// it, which its provider has nothing of.
func Episode(entries []Entry, p domain.Provider, season, n int) (Place, bool) {
	for _, e := range entries {
		for _, m := range e.Mappings {
			if m.Provider != p || m.Season != season {
				continue
			}
			for _, ep := range slices.Sorted(maps.Keys(m.Episodes)) {
				if slices.Contains(m.Episodes[ep], n) {
					return Place{Entry: e, Episode: ep, Special: m.Special}, true
				}
			}
		}
	}
	for _, e := range entries {
		for _, m := range e.Mappings {
			if m.Provider == p && m.Season == season && m.Range != nil {
				if ep := n - m.Range.Offset; m.Range.holds(ep) {
					return Place{Entry: e, Episode: ep, Special: m.Special}, true
				}
			}
		}
	}
	var best Place
	found := false
	for _, e := range entries {
		at := e.Placing(p)
		ep := n - at.Offset
		// A show's absolute order is a numbering of its own, which no mapping of the entry's
		// episodes into its seasons moves one out of.
		if at.Show == 0 || at.Season != season || ep < 1 || season != Absolute && e.mapsAway(p, ep) {
			continue
		}
		if !found || at.Offset > best.Entry.Placing(p).Offset {
			best, found = Place{Entry: e, Episode: ep}, true
		}
	}
	return best, found
}

// At answers where one of the entry's episodes falls in its show on p, numbered in order: the
// season and the show's episodes it is; false for one the show has not. The entry's own list comes
// first, then its ranges, then where it says its regular episodes fall, as Episode reads them
// back. A show numbered in its absolute order keeps its specials, but none of its seasons.
func (e Entry) At(p domain.Provider, order domain.EpisodeOrder, special bool, ep int) (int, []int, bool) {
	maps := func(m Mapping) bool {
		return m.Provider == p && m.Special == special && (order != domain.OrderAbsolute || m.Season == 0)
	}
	for _, m := range e.Mappings {
		if at, listed := m.Episodes[ep]; maps(m) && listed {
			return m.Season, at, len(at) > 0
		}
	}
	for _, m := range e.Mappings {
		if maps(m) && m.Range != nil && m.Range.holds(ep) {
			return m.Season, []int{ep + m.Range.Offset}, true
		}
	}
	if at := e.Placing(p); !special && at.Show != 0 {
		return at.Season, []int{ep + at.Offset}, true
	}
	return 0, nil, false
}

// In answers the entries with episodes in a season of a show on p.
func In(entries []Entry, p domain.Provider, season int) []Entry {
	var out []Entry
	for _, e := range entries {
		if at := e.Placing(p); at.Show != 0 && at.Season == season ||
			slices.ContainsFunc(e.Mappings, func(m Mapping) bool { return m.Provider == p && m.Season == season }) {
			out = append(out, e)
		}
	}
	return out
}

// First answers the entry a season of a show opens with, as p numbers it: the one its first
// episode is in.
func First(entries []Entry, p domain.Provider, season int) (Entry, bool) {
	place, ok := Episode(entries, p, season, 1)
	return place.Entry, ok
}

// Opening answers the entry a show opens with on p: its first season's first, or its absolute
// order's.
func Opening(entries []Entry, p domain.Provider) (Entry, bool) {
	if e, ok := First(entries, p, 1); ok {
		return e, true
	}
	return First(entries, p, Absolute)
}

// mapsAway reports whether the entry places its regular episode ep on p by a list or range of its
// own, which it then is not where the entry says the rest fall.
func (e Entry) mapsAway(p domain.Provider, ep int) bool {
	return slices.ContainsFunc(e.Mappings, func(m Mapping) bool {
		_, listed := m.Episodes[ep]
		return m.Provider == p && !m.Special && (listed || m.Range != nil && m.Range.holds(ep))
	})
}

func (r Range) holds(ep int) bool {
	return ep >= r.Start && (r.End == 0 || ep <= r.End)
}
