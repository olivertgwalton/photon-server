package domain

import (
	"fmt"
	"slices"
)

// Fetcher is what a library asks its sources for about an item, ranked apart as Jellyfin ranks
// metadata downloaders, image fetchers and lyrics fetchers.
type Fetcher string

const (
	FetcherMetadata Fetcher = "metadata"
	FetcherImages   Fetcher = "images"
	// FetcherLyrics finds a track's lyrics where neither a file beside it nor its tags hold any.
	FetcherLyrics Fetcher = "lyrics"
)

func Fetchers() []Fetcher {
	return []Fetcher{FetcherMetadata, FetcherImages, FetcherLyrics}
}

// ItemKinds are the kinds of item a library ranks its sources for, its titles' first.
func (k LibraryKind) ItemKinds() []ItemKind {
	switch k {
	case LibraryMovies:
		return []ItemKind{ItemMovie}
	case LibraryShows:
		return []ItemKind{ItemShow, ItemSeason, ItemEpisode}
	case LibraryMixed:
		return []ItemKind{ItemMovie, ItemShow, ItemSeason, ItemEpisode}
	case LibraryMusic:
		return []ItemKind{ItemArtist, ItemAlbum, ItemTrack}
	case LibraryBooks:
		return []ItemKind{ItemAuthor, ItemBook}
	}
	return nil
}

// Titles are the kinds of title a library holds, which its wall lists: a music library's artists,
// as Plex's lists them, and a books library's authors.
func (k LibraryKind) Titles() []ItemKind {
	return slices.DeleteFunc(k.ItemKinds(), func(i ItemKind) bool {
		return i != ItemMovie && i != ItemShow && i != ItemArtist && i != ItemAuthor
	})
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
// describes no season and has a poster only for a film or a show, AniList knows no episode,
// AniDB pictures no episode, and fanart.tv pictures films, shows, artists and albums alone.
// MusicBrainz alone describes music, an artist and an album but no track, which its file's tags
// describe, and pictures an album by its cover. Open Library alone describes and pictures books
// and their authors. A plugin may be ranked for anything.
func Fetchable(f Fetcher, kind ItemKind) []FieldSource {
	if f == FetcherLyrics {
		// Only a track is sung, and LRCLIB alone finds it its words.
		if kind == ItemTrack {
			return []FieldSource{SourceLRCLIB}
		}
		return nil
	}
	switch {
	case kind.Music():
		return musicFetchable(f, kind)
	case kind.Books():
		return []FieldSource{SourceOpenLibrary}
	}
	title := kind == ItemMovie || kind == ItemShow
	var out []FieldSource
	// Lyrics were answered above, so what is asked is metadata or images.
	if f == FetcherMetadata {
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
		if kind != ItemEpisode {
			out = append(out, SourceAniList)
		}
		out = append(out, SourceAniDB)
		return out
	}
	out = append(out, SourceTMDB)
	if kind != ItemMovie {
		out = append(out, SourceTVDB)
	}
	if title {
		out = append(out, SourceOMDb, SourceFanart)
	}
	if kind != ItemEpisode {
		out = append(out, SourceAniList, SourceAniDB)
	}
	return out
}

func musicFetchable(f Fetcher, kind ItemKind) []FieldSource {
	switch {
	case kind == ItemTrack:
		return nil
	case f == FetcherMetadata:
		return []FieldSource{SourceMusicBrainz}
	case kind == ItemAlbum:
		return []FieldSource{SourceMusicBrainz, SourceFanart}
	}
	return []FieldSource{SourceFanart}
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
	Lyrics   []RankedSource
}

func (k KindSources) Of(f Fetcher) []RankedSource {
	switch f {
	case FetcherMetadata:
		return k.Metadata
	case FetcherImages:
		return k.Images
	case FetcherLyrics:
		return k.Lyrics
	}
	return nil
}

// DefaultSources trust an NFO beside the file over TMDB, as Jellyfin's default order does, and
// take TMDB's pictures, for each kind a library holds; and for music and books, take whatever
// MusicBrainz and fanart.tv, or Open Library, may say, as Jellyfin's providers of them are on by
// default.
func DefaultSources(lib LibraryKind) []KindSources {
	var out []KindSources
	for _, kind := range lib.ItemKinds() {
		sources := KindSources{Kind: kind}
		switch {
		case kind.Music(), kind.Books():
			for _, f := range Fetchers() {
				for _, src := range Fetchable(f, kind) {
					switch f {
					case FetcherMetadata:
						sources.Metadata = append(sources.Metadata, RankedSource{src, true})
					case FetcherImages:
						sources.Images = append(sources.Images, RankedSource{src, true})
					case FetcherLyrics:
						sources.Lyrics = append(sources.Lyrics, RankedSource{src, true})
					}
				}
			}
		default:
			sources.Metadata = []RankedSource{{SourceNFO, true}, {SourceTMDB, true}}
			sources.Images = []RankedSource{{SourceTMDB, true}}
		}
		out = append(out, sources)
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
