package domain

import (
	"fmt"
	"slices"
)

// Fetcher is what a library asks its sources for about an item, ranked apart as Jellyfin ranks
// metadata downloaders and image fetchers.
type Fetcher string

const (
	FetcherMetadata Fetcher = "metadata"
	FetcherImages   Fetcher = "images"
)

func Fetchers() []Fetcher {
	return []Fetcher{FetcherMetadata, FetcherImages}
}

// ItemKinds are the kinds of item a library ranks its sources for, its titles' first.
func (k LibraryKind) ItemKinds() []ItemKind {
	switch k {
	case LibraryMovies:
		return []ItemKind{ItemMovie}
	case LibraryShows:
		return []ItemKind{ItemShow, ItemSeason, ItemEpisode}
	}
	return nil
}

// RankedAs is the kind whose sources an item takes: its own, or for a collection or an extra, its
// library's titles'.
func (k LibraryKind) RankedAs(item ItemKind) ItemKind {
	kinds := k.ItemKinds()
	if slices.Contains(kinds, item) {
		return item
	}
	return kinds[0]
}

// Fetchable are the built-in sources a library may rank to fetch f for an item of a kind, as
// Jellyfin lists only the providers that support a type: an NFO describes whatever it sits beside
// but holds no pictures, TheTVDB knows only shows, MDBList rates films and shows alone, and OMDb
// describes no season and has a poster only for a film or a show. A plugin may be ranked for
// anything.
func Fetchable(f Fetcher, kind ItemKind) []FieldSource {
	title := kind == ItemMovie || kind == ItemShow
	var out []FieldSource
	switch f {
	case FetcherMetadata:
		out = append(out, SourceNFO, SourceTMDB)
		if kind != ItemMovie {
			out = append(out, SourceTVDB)
		}
		if title {
			out = append(out, SourceMDBList)
		}
		if kind != ItemSeason {
			out = append(out, SourceOMDb)
		}
	case FetcherImages:
		out = append(out, SourceTMDB)
		if kind != ItemMovie {
			out = append(out, SourceTVDB)
		}
		if title {
			out = append(out, SourceOMDb)
		}
	}
	return out
}

// RankedSource is a source in a library's ranking. One not enabled keeps its place but is not
// asked.
type RankedSource struct {
	Source  FieldSource
	Enabled bool
}

// KindSources are where a library takes an item kind's metadata and its pictures from, most
// trusted first: a lower source only fills what those above it left empty.
type KindSources struct {
	Kind     ItemKind
	Metadata []RankedSource
	Images   []RankedSource
}

func (k KindSources) Of(f Fetcher) []RankedSource {
	switch f {
	case FetcherMetadata:
		return k.Metadata
	case FetcherImages:
		return k.Images
	}
	return nil
}

// DefaultSources trust an NFO beside the file over TMDB, as Jellyfin's default order does, and
// take TMDB's pictures, for each kind a library holds.
func DefaultSources(lib LibraryKind) []KindSources {
	var out []KindSources
	for _, kind := range lib.ItemKinds() {
		out = append(out, KindSources{
			Kind:     kind,
			Metadata: []RankedSource{{SourceNFO, true}, {SourceTMDB, true}},
			Images:   []RankedSource{{SourceTMDB, true}},
		})
	}
	return out
}

// CheckSources refuses a kind the library does not hold or one given twice, and a source that
// cannot fetch what it is ranked for or is ranked twice.
func CheckSources(lib LibraryKind, list []KindSources) error {
	for i, k := range list {
		if !slices.Contains(lib.ItemKinds(), k.Kind) {
			return fmt.Errorf("a library of %s ranks sources for %v, not %q", lib, lib.ItemKinds(), k.Kind)
		}
		if slices.ContainsFunc(list[:i], func(o KindSources) bool { return o.Kind == k.Kind }) {
			return fmt.Errorf("sources for %s are given twice", k.Kind)
		}
		for _, f := range Fetchers() {
			ranked := k.Of(f)
			for n, r := range ranked {
				if _, plugin := r.Source.Plugin(); !plugin && !slices.Contains(Fetchable(f, k.Kind), r.Source) {
					return fmt.Errorf("%s of %s come from %v or a plugin, not %q", f, k.Kind, Fetchable(f, k.Kind), r.Source)
				}
				if slices.ContainsFunc(ranked[:n], func(o RankedSource) bool { return o.Source == r.Source }) {
					return fmt.Errorf("%s is ranked twice for %s of %s", r.Source, f, k.Kind)
				}
			}
		}
	}
	return nil
}

// Takes is whether a library asks a source for any kind's metadata.
func (l Library) Takes(src FieldSource) bool {
	return slices.ContainsFunc(l.Sources, func(k KindSources) bool {
		return slices.Contains(k.Metadata, RankedSource{src, true})
	})
}
