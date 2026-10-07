package analysis

import (
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// look is how a keyframe looks.
type look int

const (
	picture  look = iota // a scene in colour
	letters              // white lettering on black
	darkness             // black, with nothing on it
	tint                 // a dark scene tinted blue
)

// shadesOf are keyframes every 2 seconds from from to end, each as look says, in a picture whose
// letterbox's bars are bars percent of it.
func shadesOf(from, end time.Duration, bars int, look func(at time.Duration) look) []media.Shade {
	var out []media.Shade
	for at := from; at < end; at += 2 * time.Second {
		s := media.Shade{At: at, Black: bars + 3, Contrast: 180, Saturation: 60}
		switch look(at) {
		case picture:
		case letters:
			s.Black, s.Contrast, s.Saturation = 96, 200, 0
		case darkness:
			s.Black, s.Contrast, s.Saturation = 99, 4, 0
		case tint:
			s.Black, s.Contrast, s.Saturation = 92, 30, 24
		}
		out = append(out, s)
	}
	return out
}

func TestAFilmsCreditsAreItsLetteringOnBlack(t *testing.T) {
	end := 2 * time.Hour
	from := end - 15*time.Minute
	crawl := end - 8*time.Minute
	after := end - 90*time.Second // a scene after the credits
	for _, tc := range []struct {
		name     string
		bars     int
		look     func(at time.Duration) look
		from, to time.Duration // from 0 is none
	}{
		{"credits to the end", 0, func(at time.Duration) look { return pick(at >= crawl, letters, picture) }, crawl, end},
		{"a scene after the credits is not in them", 0, func(at time.Duration) look {
			return pick(at >= crawl && at < after, letters, picture)
		}, crawl, after - 2*time.Second},
		{"a fade to black first, then credits", 0, func(at time.Duration) look {
			return pick(at >= crawl-10*time.Second && at < crawl, darkness, pick(at >= crawl, letters, picture))
		}, crawl - 10*time.Second, end},
		{"a long dark scene with nothing written is not credits", 0, func(at time.Duration) look {
			return pick(at >= crawl-4*time.Minute && at < crawl-2*time.Minute, darkness, picture)
		}, 0, 0},
		{"a dark tinted scene is not credits", 0, func(at time.Duration) look { return pick(at >= crawl, tint, picture) }, 0, 0},
		{"a letterboxed film's credits, its bars no darkness", 35, func(at time.Duration) look {
			return pick(at >= crawl, letters, picture)
		}, crawl, end},
		{"lettering only in the last seconds", 0, func(at time.Duration) look { return pick(at >= end-10*time.Second, letters, picture) }, 0, 0},
		{"no darkness", 0, func(time.Duration) look { return picture }, 0, 0},
	} {
		m := filmCredits(shadesOf(from, end, tc.bars, tc.look), end)
		switch {
		case tc.from == 0 && m != nil:
			t.Errorf("%s: %+v, want none", tc.name, m)
		case tc.from != 0 && (m == nil || m.Kind != domain.MarkerCredits || m.StartMS != tc.from.Milliseconds() || m.EndMS != tc.to.Milliseconds()):
			t.Errorf("%s: %+v, want credits from %s to %s", tc.name, m, tc.from, tc.to)
		}
	}
}

func pick(cond bool, yes, no look) look {
	if cond {
		return yes
	}
	return no
}

// A film's end read by FFmpeg, its keyframes alone: a minute of picture, then credits of lines of
// white text rising on black.
func TestAFilmsCreditsAreFoundInItsFile(t *testing.T) {
	ffmpeg, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFMPEG"), "ffmpeg"))
	if err != nil {
		t.Skipf("needs ffmpeg: %v", err)
	}
	film := filepath.Join(t.TempDir(), "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:r=24:d=60",
		"-f", "lavfi", "-i", "color=c=black:s=640x360:r=24:d=60[bg];color=c=white:s=200x10:r=24:d=60[text];[bg][text]overlay=x=220:y='mod(360-t*40,360)'",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1[v]", "-map", "[v]", "-c:v", "libx264", "-g", "48", film)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	f, err := os.Open(film)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	shades, err := media.Tools{FFmpeg: media.Tool{Path: ffmpeg}}.Shades(t.Context(), f, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := filmCredits(shades, 2*time.Minute)
	if m == nil || m.StartMS < 59_000 || m.StartMS > 63_000 {
		t.Errorf("credits %+v from %d keyframes, want them from about 1:00", m, len(shades))
	}
}
