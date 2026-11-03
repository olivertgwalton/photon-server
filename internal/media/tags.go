package media

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"go.senan.xyz/taglib"
)

// Tags is what an audio file's own tags say of it. TagLib reads them, as Navidrome's scanner
// does, rather than ffprobe: FFmpeg keeps the first of an ID3v2.4 frame's values and joins a
// Vorbis comment given twice with "; ", so two artists come back as one name, as Jellyfin's
// split artists come of reading tags that way.
type Tags struct {
	Title, Album string
	// TitleSort and AlbumSort are what to sort the track and its album by, where the tags say.
	TitleSort, AlbumSort string
	// AlbumArtists are the album's artists, and Artists the track's.
	AlbumArtists, Artists []string
	AlbumArtistSort       string
	Genres                []string
	// Released is the day the tags date the release to, zero where they give only its Year.
	Released time.Time
	Year     int
	// Disc and Track are where the track is on its album, 0 where the tags do not say.
	Disc, Track int
	// Compilation is an album of several artists' tracks, as iTunes marks one.
	Compilation bool
}

// ReadTags reads the tags of the audio file at path, an absolute path on this machine.
func ReadTags(path string) (Tags, error) {
	raw, err := taglib.ReadTags(path)
	if err != nil {
		return Tags{}, err
	}
	first := func(key string) string {
		if v := raw[key]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	t := Tags{
		Title: first(taglib.Title), Album: first(taglib.Album),
		TitleSort: first(taglib.TitleSort), AlbumSort: first(taglib.AlbumSort),
		AlbumArtists: names(raw[taglib.AlbumArtist]), AlbumArtistSort: first(taglib.AlbumArtistSort),
		Artists: names(raw[taglib.Artists]), Genres: names(raw[taglib.Genre]),
		Disc: ordinal(first(taglib.DiscNumber)), Track: ordinal(first(taglib.TrackNumber)),
		Compilation: first(taglib.Compilation) == "1",
	}
	// Picard writes each artist apart in ARTISTS, and their credit as printed in ARTIST.
	if len(t.Artists) == 0 {
		t.Artists = names(raw[taglib.Artist])
	}
	t.Released, t.Year = dated(first(taglib.Date))
	return t, nil
}

// Picture is the picture an audio file at path holds: the one its tags mark as the front cover,
// else its first; nil where it holds none.
func Picture(path string) ([]byte, error) {
	props, err := taglib.ReadProperties(path)
	if err != nil || len(props.Images) == 0 {
		return nil, err
	}
	front := max(slices.IndexFunc(props.Images, func(i taglib.ImageDesc) bool { return i.Type == "Front Cover" }), 0)
	return taglib.ReadImageOptions(path, front)
}

// names is values without blanks or repeats, in their order.
func names(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// ordinal is a disc or track number as tags write it, "3" or "3/12"; 0 for none.
func ordinal(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(s, "/", 2)[0]))
	if err != nil {
		return 0
	}
	return max(n, 0)
}

// dated reads a tag's date, "2025-05-09" or "2025", as the day it names, if it names one, and its
// year.
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
