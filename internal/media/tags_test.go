package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

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
