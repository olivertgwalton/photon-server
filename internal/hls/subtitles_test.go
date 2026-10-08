package hls

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
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
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeConverter(t, film)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	open := func() (*os.File, error) { return os.Open("testdata/fragments.mp4") }
	part := func(d time.Duration) Source {
		return Source{Open: open, Part: Part{Duration: d, Keyframes: Forced(d)}}
	}
	playback := uuid.NewV7()
	// An external file over a film in two parts, the second an hour in.
	err = r.Open(t.Context(), playback, Copy{
		Parts:     []Source{part(time.Hour), part(time.Hour)},
		Subtitles: []Subtitle{{Name: "English", Language: "en", Default: true, HearingImpaired: true, File: &SubtitleSource{Open: open}}},
		Variant:   Variant{BandwidthKbps: 8000, Codecs: []string{"avc1.640029", "mp4a.40.2"}, Range: "SDR", Width: 1920, Height: 1080, FrameRate: 24000.0 / 1001},
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
		"#EXT-X-STREAM-INF:BANDWIDTH=8000000,CODECS=\"avc1.640029,mp4a.40.2\",VIDEO-RANGE=SDR,RESOLUTION=1920x1080,FRAME-RATE=23.976,SUBTITLES=\"subs\"\nvideo.m3u8\n",
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
	m := Master([]Subtitle{{Name: "English"}, {Name: "English", Forced: true}, {Language: "fr"}, {Name: "The \"Director\"\nCommentary"}}, Variant{BandwidthKbps: 1}, 7, "v", subtitleName)
	for _, want := range []string{`NAME="English",`, `NAME="English 2",DEFAULT=NO,AUTOSELECT=YES,FORCED=YES`, `NAME="fr",LANGUAGE="fr"`, `NAME="The 'Director' Commentary"`} {
		if !strings.Contains(m, want) {
			t.Errorf("master =\n%s\nwant %s", m, want)
		}
	}
}

// styledFilm is an ASS file a film carries as a styled stream: placed, coloured, in its own font.
const styledFilm = "[Script Info]\nScriptType: v4.00+\nPlayResX: 320\nPlayResY: 240\n\n[V4+ Styles]\n" +
	"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
	"Style: Sign,Film Sans,24,&H0000FFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,8,10,10,10,1\n\n[Events]\n" +
	"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
	"Dialogue: 0,0:00:01.00,0:00:03.00,Sign,,0,0,0,,{\\pos(160,40)}Bakery\n"

// A part's styled subtitle streams are read out as they are, with the fonts the file carries for
// them, in one read of the file, which a request given up does not stop and a later one does not
// repeat.
func TestStyledSubtitlesAreReadOutOnce(t *testing.T) {
	ffmpeg := tool(t, "ffmpeg", "PHOTON_FFMPEG")
	dir := t.TempDir()
	for name, text := range map[string]string{"en.srt": "Hello.", "fr.srt": "Bonjour."} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("1\n00:00:01,000 --> 00:00:03,000\n"+text+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{"signs.ass": styledFilm, "Film Sans.ttf": "a font"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	film := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=24", "-i", filepath.Join(dir, "en.srt"), "-i", filepath.Join(dir, "fr.srt"),
		"-i", filepath.Join(dir, "signs.ass"), "-attach", filepath.Join(dir, "Film Sans.ttf"), "-metadata:s:t:0", "mimetype=font/ttf",
		"-t", "10", "-map", "0", "-map", "1", "-map", "2", "-map", "3", "-c:v", "libx264", "-c:s:0", "srt", "-c:s:1", "srt", "-c:s:2", "copy", film)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	runs := filepath.Join(dir, "runs")
	counting := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(counting, []byte("#!/bin/sh\necho >> '"+runs+"'\nexec '"+ffmpeg+"' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: counting}, FFprobe: media.Tool{Path: tool(t, "ffprobe", "PHOTON_FFPROBE")}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	open := func() (*os.File, error) { return os.Open(film) }
	part := uuid.NewV7()
	english, french, signs := 1, 2, 3
	streams := []domain.Stream{
		{Index: 0, Kind: domain.StreamVideo, Codec: "h264"},
		{Index: english, Kind: domain.StreamSubtitle, Codec: "subrip"},
		{Index: french, Kind: domain.StreamSubtitle, Codec: "subrip"},
		{Index: signs, Kind: domain.StreamSubtitle, Codec: "ass"},
	}
	src := SubtitleSource{Open: open, Stream: &signs, Part: part, Streams: streams}
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Extracted(gone, src, StyledName(signs)); !errors.Is(err, context.Canceled) {
		t.Fatalf("extracting for a client gone: %v, want %v", err, context.Canceled)
	}
	if _, err := r.Extracted(t.Context(), src, FontsDir); err != nil {
		t.Fatal(err)
	}
	out, err := r.Extracted(t.Context(), src, StyledName(signs))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(out, StyledName(signs))); err != nil || !strings.Contains(string(got), `Style: Sign,Film Sans,24`) || !strings.Contains(string(got), `{\pos(160,40)}Bakery`) {
		t.Errorf("the styled stream = %q, %v; want it as it was, its style and placing kept", got, err)
	}
	// The font is the film's fifth stream, after the video and three subtitles.
	if got, err := os.ReadFile(filepath.Join(out, FontsDir, "4.ttf")); err != nil || string(got) != "a font" {
		t.Errorf("the film's font = %q, %v; want it beside the styled stream", got, err)
	}
	read, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(read), "\n"); n != 1 {
		t.Errorf("the film was read %d times, want once", n)
	}
}

// A subtitle read out before styled ones were read out as they are is read out again, and its
// fonts with it.
func TestAPartReadOutWithoutAStreamIsReadAgain(t *testing.T) {
	ffmpeg := tool(t, "ffmpeg", "PHOTON_FFMPEG")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "signs.ass"), []byte(styledFilm), 0o644); err != nil {
		t.Fatal(err)
	}
	film := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=24", "-i", filepath.Join(dir, "signs.ass"),
		"-t", "4", "-map", "0", "-map", "1", "-c:v", "libx264", "-c:s", "copy", film)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	cache := t.TempDir()
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}, FFprobe: media.Tool{Path: tool(t, "ffprobe", "PHOTON_FFPROBE")}}, t.TempDir(), cache, Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	part, signs := uuid.NewV7(), 1
	if err := os.MkdirAll(filepath.Join(cache, part.String()), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, part.String(), "1.vtt"), []byte("WEBVTT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := SubtitleSource{
		Open: func() (*os.File, error) { return os.Open(film) }, Stream: &signs, Part: part,
		Streams: []domain.Stream{{Index: signs, Kind: domain.StreamSubtitle, Codec: "ass"}},
	}
	out, err := r.Extracted(t.Context(), src, StyledName(signs))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(out, StyledName(signs))); err != nil || !strings.Contains(string(got), "Bakery") {
		t.Errorf("the styled stream = %q, %v; want it read out", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, FontsDir)); err != nil {
		t.Errorf("fonts: %v, want their folder", err)
	}
}

// A path holding what a filtergraph reads as syntax (colons, quotes, commas, brackets) reaches the
// filter whole.
func TestAFilterTakesAnyPath(t *testing.T) {
	ffmpeg := tool(t, "ffmpeg", "PHOTON_FFMPEG")
	dir := filepath.Join(t.TempDir(), `it's [a]:b,c;d\e`)
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	film := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x48:rate=24", "-t", "1", "-c:v", "libx264", film)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	read := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "movie=filename="+filterValue(film)+",scale=32:24", "-frames:v", "1", "-f", "null", "-")
	if out, err := read.CombinedOutput(); err != nil {
		t.Errorf("the filter read %q: %v: %s", film, err, out)
	}
}

// A SubRip file written in Windows' Western European codepage comes out as UTF-8 WebVTT, its
// italics kept as WebVTT's.
func TestASubtitleFileReadsAsWebVTT(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: tool(t, "ffmpeg", "PHOTON_FFMPEG")}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, tc := range []struct{ name, file, lang, want string }{
		{"a.srt", "1\r\n00:00:01,000 --> 00:00:03,500\r\nCaf\xe9 <i>au lait</i>\r\n", "fr", "00:01.000 --> 00:03.500\nCafé <i>au lait</i>"},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.file), 0o644); err != nil {
			t.Fatal(err)
		}
		vtt, err := r.WebVTT(t.Context(), func() (*os.File, error) { return os.Open(path) }, tc.lang)
		if err != nil || !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, tc.want) {
			t.Errorf("%s: %q, %v; want WebVTT holding %q", tc.name, vtt, err, tc.want)
		}
	}
}

// A film's own subtitles come with the run that remuxes its video, a segment's as soon as the run
// is past it, wherever the player starts: the film is read once, never whole beforehand. Italics
// are kept, as WebVTT has them, and colours, which it has not, are left out.
func TestEmbeddedSubtitlesComeWithTheVideo(t *testing.T) {
	ffmpeg := tool(t, "ffmpeg", "PHOTON_FFMPEG")
	dir := t.TempDir()
	for name, cues := range map[string]string{
		"en.srt": "1\n00:00:01,000 --> 00:00:03,000\n<i>Hello.</i>\n\n2\n00:00:19,000 --> 00:00:20,000\n<font color=\"#ffff00\">Halfway.</font>\n\n3\n00:00:25,000 --> 00:00:27,000\nGoodbye.\n",
		"fr.srt": "1\n00:00:07,000 --> 00:00:09,000\nBonjour.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(cues), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	film := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=24", "-i", filepath.Join(dir, "en.srt"), "-i", filepath.Join(dir, "fr.srt"),
		"-t", "30", "-map", "0", "-map", "1", "-map", "2", "-c:v", "libx264", "-g", "24", "-c:s", "srt", film)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	runs := filepath.Join(dir, "runs")
	counting := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(counting, []byte("#!/bin/sh\necho >> '"+runs+"'\nexec '"+ffmpeg+"' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		start time.Duration
		asked []int
		want  map[int]string
	}{
		{"from the start", 0, []int{0, 1, 3, 4}, map[int]string{0: "00:00:01.000 --> 00:00:03.000\n<i>Hello.</i>\n", 1: "Bonjour.", 3: "00:00:19.000 --> 00:00:20.000\nHalfway.\n", 4: "Goodbye."}},
		{"from a seek", 18 * time.Second, []int{3, 4}, map[int]string{3: "00:00:19.000 --> 00:00:20.000\nHalfway.\n", 4: "Goodbye."}},
	} {
		_ = os.Remove(runs)
		extracted := t.TempDir()
		r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: counting}}, t.TempDir(), extracted, Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		english, french := 1, 2
		playback := uuid.NewV7()
		if err := r.Open(t.Context(), playback, Copy{
			Parts:     []Source{{Open: func() (*os.File, error) { return os.Open(film) }, Part: Part{Duration: 30 * time.Second, Keyframes: Forced(30 * time.Second)}, Video: domain.VideoPlan{Codec: "h264"}}},
			Subtitles: []Subtitle{{Name: "English", Stream: &english}, {Name: "French", Stream: &french}},
			Start:     tc.start,
		}); err != nil {
			t.Fatal(err)
		}
		for _, n := range tc.asked {
			track := 0
			if n == 1 {
				track = 1
			}
			got, err := r.SubtitleSegment(t.Context(), playback, track, n)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.want[n]) {
				t.Errorf("%s: segment %d of track %d =\n%s\nwant %q", tc.name, n, track, got, tc.want[n])
			}
		}
		r.Close(playback)
		if read, err := os.ReadFile(runs); err != nil || strings.Count(string(read), "\n") != 1 {
			t.Errorf("%s: the film was read %q times, %v; want once", tc.name, read, err)
		}
		left, err := os.ReadDir(extracted)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != 0 {
			t.Errorf("%s: %d read out whole, want none", tc.name, len(left))
		}
	}
}
