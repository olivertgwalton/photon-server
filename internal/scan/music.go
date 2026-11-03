package scan

import (
	"cmp"
	"context"
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// variousArtists is the album artist of an album of several artists' tracks that names none, as
// Picard and iTunes name one; unknownArtist is that of an album whose tracks name no artist, and
// unknownAlbum the album of a track at the library's root that names none, as Plex files them.
const (
	variousArtists = "Various Artists"
	unknownArtist  = "Unknown Artist"
	unknownAlbum   = "Unknown Album"
)

// trackPlan is a sound file of a music library's folder, as its tags and name place it.
type trackPlan struct {
	file    library.File
	tags    media.Tags
	disc    int
	number  int
	title   string
	version versionPlan
	// lyrics is how the lyrics it carries are timed, as Parts.Lyrics would read them.
	lyrics domain.LyricsTiming
}

// discFolder is a folder holding one disc of an album, "CD2" or "Disc 1".
var discFolder = regexp.MustCompile(`(?i)^(?:cd|dis[ck])[ ._-]*(\d{1,2})$`)

// leadingNumber is a track's number before its name, "07 - Hey Jude"; three digits are a disc's
// and its track's, "203-take_me_back_to_la", as releases of several discs number them.
var leadingNumber = regexp.MustCompile(`^(\d{1,3})(?:[ ._-]+|$)`)

// planTracks reads the tags of a folder's sound files, answering each, and why each it could not
// read was not, by its path.
func planTracks(root string, f library.Folder) ([]trackPlan, map[string]error) {
	var plans []trackPlan
	unread := map[string]error{}
	for _, file := range f.Files {
		if !naming.IsAudio(file.Name) {
			continue
		}
		rel := path.Join(f.Path, file.Name)
		tags, err := media.ReadTags(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			unread[rel] = err
			continue
		}
		plans = append(plans, trackPlan{
			file: file, tags: tags, disc: tags.Disc, number: tags.Track,
			title: tags.Title, version: versionPlan{parts: []library.File{file}},
			lyrics: ownLyrics(root, f, rel, tags),
		})
	}
	numberTracks(path.Base(f.Path), plans)
	return plans, unread
}

// ownLyrics is how the lyrics a track carries are timed: its LRC file's where it has one beside it,
// else its tags'.
func ownLyrics(root string, f library.Folder, rel string, tags media.Tags) domain.LyricsTiming {
	beside := slices.ContainsFunc(f.Files, func(o library.File) bool {
		return path.Join(f.Path, o.Name) == library.LyricsFile(rel)
	})
	if !beside {
		return tags.Lyrics
	}
	text, err := library.ReadLyricsFile(root, rel)
	if err != nil {
		return tags.Lyrics
	}
	return media.TimingOf(text)
}

// numberTracks places the tracks a folder's tags leave unplaced: on the disc its folder names, by
// the numbers their names begin with, and under their names. Three-digit numbers are taken as a
// disc's and a track's only where they name two discs or more, so a hundred-track album keeps
// its numbers.
func numberTracks(folder string, plans []trackPlan) {
	disc := 0
	if m := discFolder.FindStringSubmatch(folder); m != nil {
		disc = number(m[1])
	}
	leads := make([]string, len(plans))
	discs := map[byte]bool{}
	for i, t := range plans {
		if m := leadingNumber.FindStringSubmatch(stem(t.file.Name)); m != nil {
			leads[i] = m[1]
			if len(m[1]) == 3 {
				discs[m[1][0]] = true
			}
		}
	}
	split := len(discs) > 1 && !slices.ContainsFunc(leads, func(l string) bool { return len(l) != 3 })
	for i := range plans {
		t := &plans[i]
		lead := leads[i]
		if split {
			if t.disc == 0 {
				t.disc = int(lead[0] - '0')
			}
			lead = lead[1:]
		}
		if t.disc == 0 {
			t.disc = disc
		}
		if t.number == 0 && lead != "" {
			t.number = number(lead)
		}
		if t.title == "" {
			name := stem(t.file.Name)
			if m := leadingNumber.FindStringIndex(name); m != nil {
				name = name[m[1]:]
			}
			t.title = strings.TrimSpace(strings.NewReplacer("_", " ").Replace(name))
		}
	}
}

// number is the digits a pattern matched as a number.
func number(digits string) int {
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}

// albumFolder is the folder an album's tracks in folder are of: its parent, for a folder of one
// of its discs.
func albumFolder(folder string) string {
	if folder != "." && discFolder.MatchString(path.Base(folder)) {
		return path.Dir(folder)
	}
	return folder
}

// albumsOf groups a folder's tracks into the albums their tags name, each by the artist they name
// the album's. An album named by no artist of its own is its tracks' artist's where they share
// one, as Plex reads it, various artists' where they name several, and an unknown artist's where
// they name none; an album named by nothing is its folder's.
func albumsOf(folder string, plans []trackPlan, read [][]store.Version) []store.Album {
	dir := albumFolder(folder)
	var albums []store.Album
	for i, t := range plans {
		if len(read[i]) == 0 {
			continue
		}
		title := t.tags.Album
		if title == "" {
			title = unknownAlbum
			if dir != "." {
				title = path.Base(dir)
			}
		}
		n := slices.IndexFunc(albums, func(a store.Album) bool { return strings.EqualFold(a.Title, title) })
		if n < 0 {
			albums = append(albums, store.Album{Title: title, Folder: dir})
			n = len(albums) - 1
		}
		a := &albums[n]
		if len(t.tags.AlbumArtists) > 0 && a.Artist == "" {
			a.Artist, a.ArtistSort = t.tags.AlbumArtists[0], t.tags.AlbumArtistSort
		}
		if t.tags.Compilation {
			a.Artist = cmp.Or(a.Artist, variousArtists)
		}
		a.SortTitle = cmp.Or(a.SortTitle, t.tags.AlbumSort)
		a.IDs = musicBrainz(a.IDs, t.tags.ReleaseGroupMBID)
		a.ArtistIDs = musicBrainz(a.ArtistIDs, t.tags.AlbumArtistMBID)
		a.Year = cmp.Or(a.Year, t.tags.Year)
		if a.Released.IsZero() {
			a.Released = t.tags.Released
		}
		if len(a.Genres) == 0 {
			a.Genres = t.tags.Genres
		}
		a.Tracks = append(a.Tracks, store.Track{
			Title: t.title, SortTitle: t.tags.TitleSort, Disc: t.disc, Number: t.number, Artists: t.tags.Artists,
			Advisory: t.tags.Advisory, Year: t.tags.Year, Released: t.tags.Released, Genres: t.tags.Genres, Folder: folder, Version: read[i][0],
		})
		a.Tracks[len(a.Tracks)-1].Version.Gain = t.tags.Gain
		a.Tracks[len(a.Tracks)-1].Version.Lyrics = t.lyrics
		if t.tags.Pictured && !slices.ContainsFunc(a.Artwork, func(p domain.Artwork) bool { return p.Kind == domain.ArtworkPoster }) {
			a.Artwork = append(a.Artwork, domain.Artwork{Kind: domain.ArtworkPoster, Path: path.Join(folder, t.file.Name)})
		}
	}
	for i := range albums {
		if albums[i].Artist == "" {
			albums[i].Artist = sharedArtist(albums[i].Tracks)
		}
	}
	return albums
}

// musicBrainz is ids with the MusicBrainz id mbid, where they have none yet and it is one.
func musicBrainz(ids map[domain.Provider]string, mbid string) map[domain.Provider]string {
	if mbid == "" || ids[domain.ProviderMusicBrainz] != "" {
		return ids
	}
	return map[domain.Provider]string{domain.ProviderMusicBrainz: mbid}
}

// sharedArtist is the first artist every track names, variousArtists where they differ, and
// unknownArtist where none names one.
func sharedArtist(tracks []store.Track) string {
	shared := ""
	for _, t := range tracks {
		switch {
		case len(t.Artists) == 0:
		case shared == "":
			shared = t.Artists[0]
		case !strings.EqualFold(shared, t.Artists[0]):
			return variousArtists
		}
	}
	return cmp.Or(shared, unknownArtist)
}

// saveAlbums reads a folder of a music library: every sound file in it is a track of an album.
// The folder's own pictures are its album's where it holds one, before any cover its tracks hold.
func (s *Scanner) saveAlbums(ctx context.Context, r reading, folder library.Folder) (store.Saved, error) {
	lib := r.run.lib
	plans, unread := planTracks(lib.Root, folder)
	for rel, err := range unread {
		// A file TagLib takes for no sound file is passed over, as one ffprobe reads no media in;
		// any other failure is the file's passing trouble, read again at the next scan.
		if errors.Is(err, media.ErrNotTagged) {
			s.skip(ctx, r.run, rel, err)
		} else {
			s.unreadable(ctx, r, rel, err)
		}
	}
	read, err := s.readVersions(ctx, r, plansOf(plans, func(t trackPlan) []versionPlan { return []versionPlan{t.version} }))
	if err != nil {
		return store.Saved{}, err
	}
	albums := albumsOf(folder.Path, plans, read)
	if len(albums) == 1 {
		pics := picturesIn(lib.Root, folder.Path, fileNames(folder))
		albums[0].Artwork = append(pics.own, albums[0].Artwork...)
	}
	for i := range albums {
		for j, p := range albums[i].Artwork {
			if p.Blurhash == "" {
				albums[i].Artwork[j] = picture(lib.Root, p.Path, p.Kind)
			}
		}
	}
	r.run.saving.Lock()
	defer r.run.saving.Unlock()
	return s.store.SaveMusicFolder(ctx, lib.ID, folder.Path, r.fingerprint(), albums)
}
