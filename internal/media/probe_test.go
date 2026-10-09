package media

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// The fixtures are ffprobe 9.0.1's output for files made with ffmpeg (sdr.json, hdr10.json); dv8
// and hdr10plus add the side data FFmpeg reports for those to hdr10.json.
func probeFixture(t *testing.T, name string) domain.Facts {
	t.Helper()
	out, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := parseProbe(out)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

var tagString = cmp.Transformer("tag", func(t language.Tag) string { return t.String() })

func TestProbeHDR10Matroska(t *testing.T) {
	got := probeFixture(t, "hdr10.json")
	want := domain.Facts{
		Container:   "matroska,webm",
		Duration:    2023 * time.Millisecond,
		BitrateKbps: 1050,
		Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Profile: "Main 10", Width: 640, Height: 360, FrameRate: 24, BitDepth: 10, Level: 63, Range: domain.RangeHDR10},
			{Index: 1, Kind: domain.StreamAudio, Codec: "eac3", Language: language.English, Title: "Surround", Default: true, Channels: 6, ChannelLayout: "5.1(side)", SampleRate: 44100, BitrateKbps: 448},
			{Index: 2, Kind: domain.StreamAudio, Codec: "aac", Profile: "LC", Language: language.French, Title: "Commentary", Commentary: true, Channels: 1, ChannelLayout: "mono", SampleRate: 44100},
			{Index: 3, Kind: domain.StreamSubtitle, Codec: "subrip", Language: language.English, Forced: true, HearingImpaired: true},
		},
		Chapters: []domain.Chapter{
			{Start: 0, End: time.Second, Title: "Opening"},
			{Start: time.Second, End: 2 * time.Second, Title: "Credits"},
		},
	}
	if diff := cmp.Diff(want, got, tagString); diff != "" {
		t.Errorf("facts (-want +got):\n%s", diff)
	}
}

func TestProbeRange(t *testing.T) {
	tests := []struct {
		fixture string
		want    domain.Range
		dv      *domain.DolbyVision
	}{
		{fixture: "sdr.json", want: domain.RangeSDR},
		{fixture: "hdr10.json", want: domain.RangeHDR10},
		{fixture: "hdr10plus.json", want: domain.RangeHDR10Plus},
		{fixture: "dv8.json", want: domain.RangeDV, dv: &domain.DolbyVision{Profile: 8, Level: 6, Compatibility: domain.CompatibleHDR10}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			v := probeFixture(t, tt.fixture).Streams[0]
			if v.Range != tt.want {
				t.Errorf("range = %q, want %q", v.Range, tt.want)
			}
			if diff := cmp.Diff(tt.dv, v.DolbyVision); diff != "" {
				t.Errorf("dolby vision (-want +got):\n%s", diff)
			}
		})
	}
}

// interlaced.json is ffprobe's output for MPEG-2 encoded by fields, top first, in MPEG-TS.
func TestProbeFindsAnInterlacedPicture(t *testing.T) {
	for fixture, want := range map[string]bool{"interlaced.json": true, "sdr.json": false} {
		if got := probeFixture(t, fixture).Streams[0].Interlaced; got != want {
			t.Errorf("%s: interlaced %t, want %t", fixture, got, want)
		}
	}
}

// The file reaches ffprobe as descriptor 3, never as a path.
func TestProbeReadsTheOpenFile(t *testing.T) {
	dir := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("testdata", "sdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		`case "$*" in *"-fd 3"*"-i fd:"*) ;; *) echo "unexpected arguments: $*" >&2; exit 2 ;; esac` + "\n" +
		`[ "$(cat <&3)" = "media bytes" ] || { echo "descriptor 3 is not the file" >&2; exit 3; }` + "\n" +
		"cat '" + fixture + "'\n"
	ffprobe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(ffprobe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "Movie (2010).mkv")
	if err := os.WriteFile(media, []byte("media bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(media)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	facts, err := Tools{FFprobe: Tool{Path: ffprobe}}.Probe(t.Context(), Input{File: f})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Container != "mov,mp4,m4a,3gp,3g2,mj2" {
		t.Errorf("container = %q", facts.Container)
	}
}

func TestProbeLeavesAChapterNamedByItsTimeUnnamed(t *testing.T) {
	facts, err := parseProbe([]byte(`{"format": {"format_name": "matroska,webm", "duration": "120"}, "chapters": [
		{"start_time": "0", "end_time": "60", "tags": {"title": "00:00:00.000"}},
		{"start_time": "60", "end_time": "90", "tags": {"title": "(02)00:01:00:000"}},
		{"start_time": "90", "end_time": "120", "tags": {"title": "Act 3"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range facts.Chapters {
		titles = append(titles, c.Title)
	}
	if want := []string{"", "", "Act 3"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

func TestAFileWithNoMediaInIsNotMediaAndOneUnreadIsNot(t *testing.T) {
	ffprobe, err := Look("ffprobe")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	junk := filepath.Join(dir, "Movie (2010).mkv")
	if err := os.WriteFile(junk, []byte("<html>link expired</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(junk)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := (Tools{FFprobe: Tool{Path: ffprobe}}).Probe(t.Context(), Input{File: f}); !errors.Is(err, ErrNotMedia) {
		t.Errorf("probing a page of HTML: %v, want ErrNotMedia", err)
	}

	failing := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\necho 'fd:: Input/output error' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Tools{FFprobe: Tool{Path: failing}}).Probe(t.Context(), Input{File: f}); err == nil || errors.Is(err, ErrNotMedia) {
		t.Errorf("a read that failed: %v, want an error that is not ErrNotMedia", err)
	}
}
