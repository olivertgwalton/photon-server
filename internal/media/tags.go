package media

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.senan.xyz/taglib"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
	// Pictured is a file holding a picture, which Picture reads.
	Pictured bool
	// AlbumArtistMBID and ReleaseGroupMBID are the MusicBrainz ids of its album's artist and of the
	// album, as Picard tags them.
	AlbumArtistMBID, ReleaseGroupMBID string
	// An audiobook's tags, as Audible's and AAXClean's: its series and its place there, what it is
	// about, and a cue sheet of its chapters.
	Series, SeriesPart, Comment, CueSheet string
	Gain                                  domain.ReplayGain
	Advisory                              domain.Advisory
	// Lyrics is how the lyrics the tags hold are timed, "" for none.
	Lyrics domain.LyricsTiming
}

// ErrNotTagged is a file TagLib reads as no sound file it knows.
var ErrNotTagged = errors.New("not a sound file taglib reads")

// ReadTags reads the tags of the audio file at path, an absolute path on this machine.
func ReadTags(path string) (Tags, error) {
	raw, err := taglib.ReadTags(path)
	if errors.Is(err, taglib.ErrInvalidFile) {
		return Tags{}, fmt.Errorf("%w: %w", ErrNotTagged, err)
	}
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
		Compilation:     first(taglib.Compilation) == "1",
		AlbumArtistMBID: first(taglib.MusicBrainzAlbumArtistID), ReleaseGroupMBID: first(taglib.MusicBrainzReleaseGroupID),
		Series: cmp.Or(first("SERIES"), first(taglib.MovementName)), SeriesPart: cmp.Or(first("PART"), first(taglib.MovementNumber)),
		Comment: first(taglib.Comment), CueSheet: first("CUESHEET"),
	}
	// Picard writes each artist apart in ARTISTS, and their credit as printed in ARTIST.
	if len(t.Artists) == 0 {
		for _, credit := range raw[taglib.Artist] {
			t.Artists = append(t.Artists, credited(credit)...)
		}
	}
	t.Title, t.Artists = featuring(t.Title, t.Artists)
	t.Artists = names(t.Artists)
	t.Released, t.Year = dated(first(taglib.Date))
	t.Gain = replayGain(first)
	t.Advisory = advisory(first(itunesAdvisory))
	if t.Advisory == "" && isMP4(path) {
		t.Advisory = mp4Advisory(path)
	}
	t.Lyrics = TimingOf(strings.Join(raw[taglib.Lyrics], "\n"))
	props, err := taglib.ReadProperties(path)
	t.Pictured = len(props.Images) > 0
	return t, err
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

// itunesAdvisory is iTunes's advisory as Vorbis comments and ID3 frames carry it, and as TagLib
// names an MP4's freeform atom of it.
const itunesAdvisory = "ITUNESADVISORY"

// advisory reads iTunes's advisory: 1, or 4 as older iTunes wrote it, is explicit, and 2 clean.
func advisory(s string) domain.Advisory {
	switch strings.TrimSpace(s) {
	case "1", "4":
		return domain.AdvisoryExplicit
	case "2":
		return domain.AdvisoryClean
	}
	return ""
}

func isMP4(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m4a", ".m4b", ".m4p", ".mp4":
		return true
	}
	return false
}

// mp4Advisory is the advisory of an MP4 as iTunes Store files carry it, in the rtng atom, which
// TagLib gives no property: "" where it has none or cannot be read.
func mp4Advisory(path string) domain.Advisory {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	r := io.NewSectionReader(f, 0, st.Size())
	meta, ok, err := within(r, box{end: st.Size()}, "moov", "udta", "meta")
	if err != nil || !ok {
		return ""
	}
	// meta is a full box, its version and flags before what it holds; data holds its type and
	// locale before its value.
	data, ok, err := within(r, box{body: meta.body + 4, end: meta.end}, "ilst", "rtng", "data")
	if err != nil || !ok || data.end-data.body < 9 {
		return ""
	}
	var value [1]byte
	if readAt(r, value[:], data.body+8) != nil {
		return ""
	}
	return advisory(strconv.Itoa(int(value[0])))
}

// replayGain reads ReplayGain's tags, or Opus's R128 gains where it has none: R128 is in 1/256 dB
// from EBU R128's -23 LUFS, five decibels under ReplayGain 2's -18.
func replayGain(first func(string) string) domain.ReplayGain {
	gain := func(gainKey, peakKey, r128Key string) *domain.Gain {
		db, ok := decibels(first(gainKey))
		if !ok {
			q, err := strconv.Atoi(first(r128Key))
			if err != nil {
				return nil
			}
			db = float64(q)/256 + 5
		}
		peak, err := strconv.ParseFloat(first(peakKey), 64)
		if err != nil || peak < 0 {
			peak = 0
		}
		return &domain.Gain{DB: db, Peak: peak}
	}
	return domain.ReplayGain{
		Track: gain("REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK", "R128_TRACK_GAIN"),
		Album: gain("REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK", "R128_ALBUM_GAIN"),
	}
}

// decibels reads a gain as taggers write it, "-6.48 dB".
func decibels(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 2 && strings.EqualFold(s[len(s)-2:], "db") {
		s = strings.TrimSpace(s[:len(s)-2])
	}
	db, err := strconv.ParseFloat(s, 64)
	return db, err == nil
}

// featured is a credit to artists a title or an artist's name marks as featured, "(feat. Justice)"
// or "ft. A & B". In a title it is bracketed, so "Defeat" and "Left Behind" are titles alone.
var (
	featuredInTitle  = regexp.MustCompile(`(?i)\s*[(\[](?:feat\.?|ft\.?|featuring)\s+([^)\]]+)[)\]]`)
	featuredInArtist = regexp.MustCompile(`(?i)\s+[(\[]?(?:feat\.?|ft\.?|featuring)\s+([^)\]]+)[)\]]?$`)
	// together joins the featured, "A, B & C"; it is not read in a name alone, Simon & Garfunkel
	// being one.
	together = regexp.MustCompile(`\s*[,&]\s*`)
)

// featuring is a title less the credits it gives featured artists, and artists with them after,
// as Apple Music and MusicBrainz credit them: "Wake Me Up (Feat. Justice)" by The Weeknd is "Wake
// Me Up" by The Weeknd and Justice. What follows the credit stays, "Song (feat. A) [Remix]" being
// "Song [Remix]".
func featuring(title string, artists []string) (string, []string) {
	for _, m := range featuredInTitle.FindAllStringSubmatch(title, -1) {
		artists = append(artists, together.Split(m[1], -1)...)
	}
	return strings.TrimSpace(featuredInTitle.ReplaceAllString(title, "")), artists
}

// credited is the artists an artist's credit names, "The Weeknd feat. Justice" being The Weeknd
// and Justice.
func credited(credit string) []string {
	m := featuredInArtist.FindStringSubmatchIndex(credit)
	if m == nil {
		return []string{credit}
	}
	return append([]string{credit[:m[0]]}, together.Split(credit[m[2]:m[3]], -1)...)
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

// Chapters reads a cue sheet's tracks as chapters, as an audiobook in one file carries its own:
// each from its INDEX 01, in minutes, seconds and frames of 1/75 s, to the next's, the last to the
// end of a file that runs length.
func Chapters(cue string, length time.Duration) []domain.Chapter {
	var out []domain.Chapter
	title := ""
	for line := range strings.Lines(cue) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "TRACK "):
			title = ""
		case strings.HasPrefix(line, "TITLE "):
			title = strings.Trim(strings.TrimPrefix(line, "TITLE "), `"`)
		case strings.HasPrefix(line, "INDEX 01 "):
			at, ok := cueTime(strings.TrimPrefix(line, "INDEX 01 "))
			if !ok {
				continue
			}
			if n := len(out); n > 0 {
				out[n-1].End = at
			}
			out = append(out, domain.Chapter{Start: at, End: length, Title: title})
		}
	}
	return out
}

// cueTime reads a cue sheet's mm:ss:ff.
func cueTime(s string) (time.Duration, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		n[i] = v
	}
	return time.Duration(n[0])*time.Minute + time.Duration(n[1])*time.Second + time.Duration(n[2])*time.Second/75, true
}
