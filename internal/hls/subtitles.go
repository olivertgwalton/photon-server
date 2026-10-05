package hls

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Subtitle is a text subtitle published beside the video as WebVTT, read from one or more files.
type Subtitle struct {
	Name     string
	Language string
	Default  bool
	Forced   bool
	// HearingImpaired is published as Apple's characteristic for it.
	HearingImpaired bool
	Sources         []SubtitleSource
}

// SubtitleSource is a file holding some of a subtitle's cues: an external file, or a stream of one
// of a copy's parts, and where its time zero sits on the copy's timeline.
type SubtitleSource struct {
	Open func() (*os.File, error)
	// Stream is the file's stream to read, by its index; nil for a subtitle file.
	Stream *int
	Offset time.Duration
	// Language is a subtitle file's, which says what it was written in where it is not UTF-8.
	Language string
}

// Cue is one WebVTT cue on the copy's timeline: its timing line's settings and its text.
type Cue struct {
	Start, End time.Duration
	Settings   string
	Text       string
}

// TextSubtitle reports whether FFmpeg converts a subtitle codec to WebVTT: text, not pictures.
func TextSubtitle(codec string) bool {
	switch codec {
	case "subrip", "ass", "ssa", "webvtt", "mov_text", "text", "sami", "microdvd", "subviewer", "realtext":
		return true
	}
	return false
}

// extract converts a subtitle source to WebVTT and reads its cues onto the copy's timeline.
func (r *Remuxer) extract(ctx context.Context, src SubtitleSource) ([]Cue, error) {
	f, err := src.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "fd", "-fd", "3"}
	stream := "0:s:0"
	if src.Stream != nil {
		// A container's text is UTF-8 by its specification.
		stream = "0:" + strconv.Itoa(*src.Stream)
	} else {
		charset, err := subtitleCharset(f, src.Language)
		if err != nil {
			return nil, err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if charset != "" {
			a = append(a, "-sub_charenc", charset)
		}
	}
	a = append(a, "-i", "fd:", "-map", stream, "-c:s", "webvtt", "-f", "webvtt", "-")
	cmd := exec.CommandContext(ctx, r.ffmpeg, a...) //nolint:gosec // the configured ffmpeg; every argument is built here
	cmd.ExtraFiles = []*os.File{f}
	stderr := &tail{}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, stderr)
	}
	cues, err := readVTT(strings.NewReader(string(out)))
	for i := range cues {
		cues[i].Start += src.Offset
		cues[i].End += src.Offset
	}
	return cues, err
}

// readVTT reads the cues of a WebVTT file, dropping its header, notes, styles and cue names.
func readVTT(r io.Reader) ([]Cue, error) {
	var cues []Cue
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	var cue *Cue
	var text []string
	flush := func() {
		if cue != nil {
			cue.Text = strings.Join(text, "\n")
			cues = append(cues, *cue)
		}
		cue, text = nil, nil
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			flush()
		case cue != nil:
			text = append(text, line)
		case strings.Contains(line, "-->"):
			c, ok := timing(line)
			if ok {
				cue = &c
			}
		}
	}
	flush()
	return cues, sc.Err()
}

// timing reads a cue's timing line: "00:01:02.500 --> 00:01:04.000 align:start".
func timing(line string) (Cue, bool) {
	from, rest, _ := strings.Cut(line, "-->")
	rest = strings.TrimSpace(rest)
	to, settings, _ := strings.Cut(rest, " ")
	start, ok1 := vttTime(strings.TrimSpace(from))
	end, ok2 := vttTime(to)
	return Cue{Start: start, End: end, Settings: strings.TrimSpace(settings)}, ok1 && ok2
}

// vttTime reads "hh:mm:ss.ttt" or "mm:ss.ttt".
func vttTime(s string) (time.Duration, bool) {
	clock, frac, ok := strings.Cut(s, ".")
	if !ok || len(frac) != 3 {
		return 0, false
	}
	ms, err := strconv.Atoi(frac)
	if err != nil {
		return 0, false
	}
	t := time.Duration(ms) * time.Millisecond
	fields := strings.Split(clock, ":")
	if len(fields) < 2 || len(fields) > 3 {
		return 0, false
	}
	unit := time.Second
	for _, field := range slices.Backward(fields) {
		n, err := strconv.Atoi(field)
		if err != nil {
			return 0, false
		}
		t += time.Duration(n) * unit
		unit *= 60
	}
	return t, true
}

// writeVTT writes the cues shown during [from, to) of a part whose time zero is at offset on the
// copy's timeline, timed on the part's own clock and mapped to the video's (see clockOffset).
func writeVTT(cues []Cue, offset, from, to time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WEBVTT\nX-TIMESTAMP-MAP=MPEGTS:%d,LOCAL:00:00:00.000\n", clockOffset.Milliseconds()*90)
	for _, c := range cues {
		start, end := c.Start-offset, c.End-offset
		if end <= from || start >= to {
			continue
		}
		b.WriteString("\n" + vttStamp(start) + " --> " + vttStamp(end))
		if c.Settings != "" {
			b.WriteString(" " + c.Settings)
		}
		b.WriteString("\n" + c.Text + "\n")
	}
	return b.String()
}

func vttStamp(d time.Duration) string {
	d = max(d, 0)
	h, m := d/time.Hour, d%time.Hour/time.Minute
	s, ms := d%time.Minute/time.Second, d%time.Second/time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
