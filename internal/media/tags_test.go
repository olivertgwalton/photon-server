package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/testtool"
)

// Two artists tagged apart stay two, in FLAC's Vorbis comments and in MP3's ID3v2.4 alike, where
// ffprobe would read them as one name joined by "; ", or the first alone.
func TestATracksArtistsAreReadApart(t *testing.T) {
	for _, name := range []string{"track.flac", "track.mp3"} {
		path := filepath.Join(t.TempDir(), name)
		testtool.Audio(t, path, map[string][]string{
			"TITLE": {"Timeless"}, "ALBUM": {"Hurry Up Tomorrow"}, "ALBUMARTIST": {"The Weeknd"},
			"ARTIST": {"The Weeknd", "Playboi Carti"}, "GENRE": {"R&B", "Pop"},
			"DATE": {"2025-01-31"}, "DISCNUMBER": {"2/2"}, "TRACKNUMBER": {"2/11"}, "COMPILATION": {"0"},
		}, nil)
		got, err := ReadTags(path)
		if err != nil {
			t.Fatal(err)
		}
		want := Tags{
			Title: "Timeless", Album: "Hurry Up Tomorrow", AlbumArtists: []string{"The Weeknd"},
			Artists: []string{"The Weeknd", "Playboi Carti"}, Genres: []string{"R&B", "Pop"},
			Released: time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC), Year: 2025, Disc: 2, Track: 2,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s (-want +got):\n%s", name, diff)
		}
	}
}

// Picard's ARTISTS names each artist where ARTIST prints their credit; a year alone dates no day.
func TestATracksArtistsAreTakenApartWhereTheTagsSplitThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.flac")
	testtool.Audio(t, path, map[string][]string{
		"ARTIST": {"The Weeknd feat. Playboi Carti"}, "ARTISTS": {"The Weeknd", "Playboi Carti"},
		"DATE": {"2025"}, "COMPILATION": {"1"},
	}, nil)
	got, err := ReadTags(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Tags{Artists: []string{"The Weeknd", "Playboi Carti"}, Year: 2025, Compilation: true}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

// A track marked explicit or clean says so, whether its tags carry iTunes's advisory as a comment
// or frame or, as a file from the iTunes Store does, in its rtng atom; one marked neither says
// nothing.
func TestATracksAdvisoryIsWhatItsTagsMark(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		tags map[string][]string
		rtng byte
		want domain.Advisory
	}{
		{"explicit.flac", map[string][]string{"ITUNESADVISORY": {"1"}}, 0, domain.AdvisoryExplicit},
		{"clean.mp3", map[string][]string{"ITUNESADVISORY": {"2"}}, 0, domain.AdvisoryClean},
		{"store.m4a", map[string][]string{"TITLE": {"Timeless"}}, 1, domain.AdvisoryExplicit},
		{"unmarked.flac", map[string][]string{"TITLE": {"Timeless"}}, 0, ""},
	} {
		path := filepath.Join(dir, tc.name)
		testtool.Audio(t, path, tc.tags, nil)
		if tc.rtng != 0 {
			withRating(t, path, tc.rtng)
		}
		got, err := ReadTags(path)
		if err != nil || got.Advisory != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got.Advisory, err, tc.want)
		}
	}
}

// withRating adds an rtng atom of rating to the MP4 at path, as iTunes writes one, at the end of
// its moov/udta/meta/ilst, growing each box that holds it.
func withRating(t *testing.T, path string, rating byte) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	item := []byte{0, 0, 0, 25, 'r', 't', 'n', 'g', 0, 0, 0, 17, 'd', 'a', 't', 'a', 0, 0, 0, 21, 0, 0, 0, 0, rating}
	var holders []int
	off, end := 0, len(b)
	for _, typ := range []string{"moov", "udta", "meta", "ilst"} {
		for off < end && string(b[off+4:off+8]) != typ {
			off += int(binary.BigEndian.Uint32(b[off:]))
		}
		if off >= end {
			t.Fatalf("%s holds no %s", path, typ)
		}
		holders = append(holders, off)
		end = off + int(binary.BigEndian.Uint32(b[off:]))
		off += 8
		if typ == "meta" {
			off += 4
		}
	}
	for _, h := range holders {
		binary.BigEndian.PutUint32(b[h:], binary.BigEndian.Uint32(b[h:])+uint32(len(item)))
	}
	b = append(b[:end], append(item, b[end:]...)...)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestATracksCoverIsTheOneItHolds(t *testing.T) {
	dir := t.TempDir()
	var cover bytes.Buffer
	pic := image.NewRGBA(image.Rect(0, 0, 4, 4))
	pic.Set(1, 1, color.White)
	if err := png.Encode(&cover, pic); err != nil {
		t.Fatal(err)
	}
	with, without := filepath.Join(dir, "with.flac"), filepath.Join(dir, "without.flac")
	testtool.Audio(t, with, map[string][]string{"TITLE": {"With"}}, cover.Bytes())
	testtool.Audio(t, without, map[string][]string{"TITLE": {"Without"}}, nil)
	if tags, err := ReadTags(with); err != nil || !tags.Pictured {
		t.Errorf("ReadTags(with) = %+v, %v; want it pictured", tags, err)
	}
	if got, err := Picture(with); err != nil || !bytes.Equal(got, cover.Bytes()) {
		t.Errorf("Picture(with) = %d bytes, %v; want the %d it holds", len(got), err, cover.Len())
	}
	if got, err := Picture(without); err != nil || got != nil {
		t.Errorf("Picture(without) = %d bytes, %v; want none", len(got), err)
	}
}

// A track's ReplayGain is read from its tags in FLAC and MP3 alike, and Opus's R128 gain is taken
// to ReplayGain's loudness.
func TestATracksReplayGainIsReadFromItsTags(t *testing.T) {
	tags := map[string][]string{
		"REPLAYGAIN_TRACK_GAIN": {"-6.48 dB"}, "REPLAYGAIN_TRACK_PEAK": {"0.988556"},
		"REPLAYGAIN_ALBUM_GAIN": {"-7.10 dB"}, "REPLAYGAIN_ALBUM_PEAK": {"1.000000"},
	}
	want := domain.ReplayGain{Track: &domain.Gain{DB: -6.48, Peak: 0.988556}, Album: &domain.Gain{DB: -7.10, Peak: 1}}
	for _, name := range []string{"track.flac", "track.mp3"} {
		path := filepath.Join(t.TempDir(), name)
		testtool.Audio(t, path, tags, nil)
		got, err := ReadTags(path)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want, got.Gain); diff != "" {
			t.Errorf("%s (-want +got):\n%s", name, diff)
		}
	}
	r128 := replayGain(func(key string) string { return map[string]string{"R128_TRACK_GAIN": "-1280"}[key] })
	if diff := cmp.Diff(domain.ReplayGain{Track: &domain.Gain{DB: 0}}, r128); diff != "" {
		t.Errorf("R128 (-want +got):\n%s", diff)
	}
}

// A track tagged with no gain has none, rather than a gain of nothing.
func TestATrackWithoutReplayGainHasNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.flac")
	testtool.Audio(t, path, map[string][]string{"TITLE": {"Untagged"}}, nil)
	got, err := ReadTags(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Gain != (domain.ReplayGain{}) {
		t.Errorf("gain = %+v, want none", got.Gain)
	}
}

// An artist credited as featured, in the title or in the artist's credit, is the track's artist
// after its own, and leaves the title, as Apple Music credits them; a title that only reads like a
// credit, and a name with "&" in it, are left as they are.
func TestFeaturedArtistsAreTheTracksArtists(t *testing.T) {
	for _, tc := range []struct {
		title, artist string
		artists       []string
		want          Tags
	}{
		{"Wake Me Up (Feat. Justice)", "The Weeknd", nil, Tags{Title: "Wake Me Up", Artists: []string{"The Weeknd", "Justice"}}},
		{"Song (ft. A, B & C) [Remix]", "Lead", nil, Tags{Title: "Song [Remix]", Artists: []string{"Lead", "A", "B", "C"}}},
		{"Timeless", "The Weeknd featuring Playboi Carti", nil, Tags{Title: "Timeless", Artists: []string{"The Weeknd", "Playboi Carti"}}},
		{"Timeless [feat. Playboi Carti]", "", []string{"The Weeknd", "Playboi Carti"}, Tags{Title: "Timeless", Artists: []string{"The Weeknd", "Playboi Carti"}}},
		{"Ghost (feat. Florence and the Machine)", "Lead", nil, Tags{Title: "Ghost", Artists: []string{"Lead", "Florence and the Machine"}}},
		{"Defeat the Feather", "Simon & Garfunkel", nil, Tags{Title: "Defeat the Feather", Artists: []string{"Simon & Garfunkel"}}},
	} {
		path := filepath.Join(t.TempDir(), "track.flac")
		tags := map[string][]string{"TITLE": {tc.title}}
		if tc.artist != "" {
			tags["ARTIST"] = []string{tc.artist}
		}
		if tc.artists != nil {
			tags["ARTISTS"] = tc.artists
		}
		testtool.Audio(t, path, tags, nil)
		got, err := ReadTags(path)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(tc.want, got); diff != "" {
			t.Errorf("%q by %q (-want +got):\n%s", tc.title, tc.artist, diff)
		}
	}
}

// What a track at an address is tagged, as ffprobe reports it: an Ogg file's comments on its
// stream, the container's tags over them, FFmpeg's names for ID3 frames read as TagLib's, artists
// split where FFmpeg joined them and a title left whole.
func TestTagsReadThroughFfprobe(t *testing.T) {
	out := []byte(`{
		"format": {"format_name": "ogg", "probe_score": 100, "tags": {
			"title": "Part One; Part Two", "album_artist": "Radiohead", "track": "3/12"
		}},
		"streams": [
			{"index": 0, "codec_type": "video", "tags": {"title": "Cover"}},
			{"index": 1, "codec_type": "audio", "tags": {
				"TITLE": "Stream Title", "ALBUM": "OK Computer", "ARTIST": "Thom Yorke; Jonny Greenwood", "DISC": "2"
			}}
		]
	}`)
	tags, err := probedTags(out)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Title != "Part One; Part Two" || tags.Album != "OK Computer" {
		t.Errorf("title %q, album %q: want the container's title whole and the stream's album", tags.Title, tags.Album)
	}
	if !slices.Equal(tags.AlbumArtists, []string{"Radiohead"}) || tags.Track != 3 || tags.Disc != 2 {
		t.Errorf("album artists %q, track %d, disc %d: want FFmpeg's names read", tags.AlbumArtists, tags.Track, tags.Disc)
	}
	if !slices.Equal(tags.Artists, []string{"Thom Yorke", "Jonny Greenwood"}) {
		t.Errorf("artists %q, want the joined pair apart", tags.Artists)
	}
}
