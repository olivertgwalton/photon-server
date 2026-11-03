package media

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/testtool"
)

// An LRC file's lines are timed, in the order they are sung, a line sung twice under both its
// times, an enhanced file's word times left out, and its offset taken off.
func TestLyricsAreTimedAsAnLRCFileTimesThem(t *testing.T) {
	got := ParseLyrics("[ar:The Weeknd]\r\n[offset:+500]\n[00:12.00]Wake me up\n[01:02.5][00:20.250]When it's <00:21.00>over\n\n[00:30]\n")
	want := Lyrics{Timing: TimedByLine, Lines: []LyricLine{
		{Start: 11500 * time.Millisecond, Text: "Wake me up"},
		{Start: 19750 * time.Millisecond, Text: "When it's over"},
		{Start: 29500 * time.Millisecond, Text: ""},
		{Start: 62 * time.Second, Text: "When it's over"},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

// Lyrics no line of which is timed are shown as they are written.
func TestUntimedLyricsAreLinesAsWritten(t *testing.T) {
	got := ParseLyrics("Wake me up\n\nWhen it's over\n")
	want := Lyrics{Timing: Untimed, Lines: []LyricLine{{Text: "Wake me up"}, {Text: "When it's over"}}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestLyricsAreReadFromATracksTags(t *testing.T) {
	for _, name := range []string{"track.flac", "track.mp3"} {
		path := filepath.Join(t.TempDir(), name)
		testtool.Audio(t, path, map[string][]string{"LYRICS": {"Wake me up\nWhen it's over"}}, nil)
		got, err := ReadLyrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != "Wake me up\nWhen it's over" {
			t.Errorf("%s: lyrics = %q", name, got)
		}
	}
}
