package hls

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakeConverter is an ffmpeg that converts any subtitle to the same WebVTT.
func fakeConverter(t *testing.T, vtt string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "out.vtt"), []byte(vtt), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec cat '"+filepath.Join(dir, "out.vtt")+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const film = `WEBVTT

NOTE made by hand

1
00:00:01.000 --> 00:00:03.000 align:start
Hello.

00:00:05.500 --> 00:00:07.000
Across the cut,
on two lines.

01:00:00.000 --> 01:00:02.000
After the interval.
`

func TestSubtitlesAreCutWithTheVideo(t *testing.T) {
	r, err := NewRemuxer(fakeConverter(t, film), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	open := func() (*os.File, error) { return os.Open("testdata/fragments.mp4") }
	part := func(d time.Duration) Source {
		return Source{Open: open, Part: Part{Duration: d, Keyframes: Forced(d)}}
	}
	playback := uuid.NewV7()
	// An external file over a film in two parts, the second an hour in.
	err = r.Open(playback, Copy{
		Parts:         []Source{part(time.Hour), part(time.Hour)},
		Subtitles:     []Subtitle{{Name: "English", Language: "en", Default: true, HearingImpaired: true, Sources: []SubtitleSource{{Open: open}}}},
		BandwidthKbps: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	master, err := r.Playlist(playback, "main.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="English",LANGUAGE="en",DEFAULT=YES,AUTOSELECT=YES,FORCED=NO,CHARACTERISTICS="public.accessibility.transcribes-spoken-dialog,public.accessibility.describes-music-and-sound",URI="sub0.m3u8"`,
		"#EXT-X-STREAM-INF:BANDWIDTH=8000000,SUBTITLES=\"subs\"\nvideo.m3u8\n",
	} {
		if !strings.Contains(master, want) {
			t.Errorf("master =\n%s\nwant %s", master, want)
		}
	}
	subs, err := r.Playlist(playback, "sub0.m3u8")
	if err != nil || strings.Contains(subs, "EXT-X-MAP") || !strings.Contains(subs, "#EXT-X-DISCONTINUITY\n#EXTINF:6.000000,\nsub0-600.vtt\n") {
		t.Fatalf("subtitle playlist = %v\n%s\nwant the video's segments with no initialisation", err, subs)
	}
	for _, tc := range []struct {
		n    int
		want []string
		not  []string
	}{
		{0, []string{"X-TIMESTAMP-MAP=MPEGTS:900000,LOCAL:00:00:00.000", "00:00:01.000 --> 00:00:03.000 align:start\nHello.", "00:00:05.500 --> 00:00:07.000\nAcross the cut,\non two lines."}, []string{"After"}},
		{1, []string{"Across the cut"}, []string{"Hello"}},
		// The second part's first segment, on its own clock.
		{600, []string{"00:00:00.000 --> 00:00:02.000\nAfter the interval."}, []string{"Hello"}},
	} {
		got, err := r.SubtitleSegment(t.Context(), playback, 0, tc.n)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("segment %d =\n%s\nwant %q", tc.n, got, w)
			}
		}
		for _, w := range tc.not {
			if strings.Contains(got, w) {
				t.Errorf("segment %d =\n%s\nwant no %q", tc.n, got, w)
			}
		}
	}
	if _, err := r.SubtitleSegment(t.Context(), playback, 1, 0); !errors.Is(err, ErrNoRemux) {
		t.Errorf("a track there is not: %v, want ErrNoRemux", err)
	}
}

func TestRepeatedNamesAreNumbered(t *testing.T) {
	m := Master([]Subtitle{{Name: "English"}, {Name: "English", Forced: true}, {Language: "fr"}}, 1, "v", subtitleName)
	for _, want := range []string{`NAME="English",`, `NAME="English 2",DEFAULT=NO,AUTOSELECT=YES,FORCED=YES`, `NAME="fr",LANGUAGE="fr"`} {
		if !strings.Contains(m, want) {
			t.Errorf("master =\n%s\nwant %s", m, want)
		}
	}
}
