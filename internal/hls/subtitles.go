package hls

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/media"
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
	// Part is the part a stream is read from, whose text streams are kept together under its id.
	Part   uuid.UUID
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

const (
	// subtitlesKept is how long a part's extracted text streams are kept unread: a month, as the
	// previews of a missing file are.
	subtitlesKept = 30 * 24 * time.Hour
	// extractRetry is how long an extraction that failed is answered with its failure before a file
	// is read again, so a file ffmpeg cannot read is not read whole for every segment asked for.
	extractRetry = 10 * time.Minute
	// extracting names the folders extractions are written in before they are moved into place.
	extracting = ".making-"
)

// extraction is a part's text streams being read out, which every request for any of them waits on.
type extraction struct {
	done chan struct{}
	err  error
	at   time.Time
}

// cues reads a subtitle source's cues onto the copy's timeline. streams are the text streams of
// the source's part that the playback carries.
func (r *Remuxer) cues(ctx context.Context, src SubtitleSource, streams []int) ([]Cue, error) {
	var cues []Cue
	var err error
	if src.Stream == nil {
		cues, err = r.convert(ctx, src)
	} else {
		cues, err = r.embedded(ctx, src, streams)
	}
	for i := range cues {
		cues[i].Start += src.Offset
		cues[i].End += src.Offset
	}
	return cues, err
}

// convert reads a subtitle file's cues.
func (r *Remuxer) convert(ctx context.Context, src SubtitleSource) ([]Cue, error) {
	vtt, err := r.WebVTT(ctx, src.Open, src.Language)
	if err != nil {
		return nil, err
	}
	return readVTT(strings.NewReader(vtt))
}

// WebVTT reads a text subtitle file as WebVTT, in what its byte order mark says it is written in,
// else UTF-8, else the codepage of its language.
func (r *Remuxer) WebVTT(ctx context.Context, open func() (*os.File, error), language string) (string, error) {
	f, err := open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	a := fdInput()
	charset, err := subtitleCharset(f, language)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if charset != "" {
		a = append(a, "-sub_charenc", charset)
	}
	a = append(a, "-i", "fd:", "-map", "0:s:0", "-c:s", "webvtt", "-f", "webvtt", "-")
	cmd := media.NewCommand(ctx, []*os.File{f}, r.ffmpeg, a...)
	out, err := cmd.Output()
	return string(out), cmd.Err(err)
}

// embedded reads a stream of a part as WebVTT. Reading one means reading the whole file, so every
// text stream of the part is read out in the same pass and kept, for this playback and the next.
// The pass outlives the request that started it: a player that gives up waiting finds it further
// on when it asks again.
func (r *Remuxer) embedded(ctx context.Context, src SubtitleSource, streams []int) ([]Cue, error) {
	dir := filepath.Join(r.subtitles, src.Part.String())
	if err := r.extracted(ctx, src, streams, dir); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, strconv.Itoa(*src.Stream)+".vtt"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readVTT(f)
}

// extracted waits for a part's text streams to be in dir, reading them out if no one is.
func (r *Remuxer) extracted(ctx context.Context, src SubtitleSource, streams []int, dir string) error {
	r.mu.Lock()
	x, ok := r.extractions[src.Part]
	if ok && closed(x.done) && time.Since(x.at) > extractRetry {
		ok = false
	}
	if !ok {
		if _, err := os.Stat(dir); err == nil {
			r.mu.Unlock()
			now := time.Now()
			return os.Chtimes(dir, now, now)
		}
		x = &extraction{done: make(chan struct{})}
		r.extractions[src.Part] = x
		go func() {
			x.err = r.extract(context.WithoutCancel(ctx), src, streams, dir)
			r.mu.Lock()
			if x.err == nil {
				delete(r.extractions, src.Part)
			}
			x.at = time.Now()
			r.mu.Unlock()
			close(x.done)
		}()
	}
	r.mu.Unlock()
	select {
	case <-x.done:
		return x.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// extract writes each of a part's text streams into dir as {index}.vtt, in one read of the file.
// A container's text is UTF-8 by its specification.
func (r *Remuxer) extract(ctx context.Context, src SubtitleSource, streams []int, dir string) error {
	f, err := src.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	ctx, cancel := media.Within(ctx, r.ffmpeg, media.WholeRun(f))
	defer cancel()
	made, err := os.MkdirTemp(r.subtitles, extracting)
	if err != nil {
		return err
	}
	defer os.RemoveAll(made)
	a := append(fdInput(), "-i", "fd:")
	for _, n := range streams {
		a = append(a, "-map", "0:"+strconv.Itoa(n), "-c:s", "webvtt", "-f", "webvtt", filepath.Join(made, strconv.Itoa(n)+".vtt"))
	}
	cmd := media.NewCommand(ctx, []*os.File{f}, r.ffmpeg, a...)
	if err := cmd.Run(); err != nil {
		return cmd.Err(err)
	}
	return os.Rename(made, dir)
}

// SweepSubtitles removes the text streams of parts no one has read for subtitlesKept.
func (r *Remuxer) SweepSubtitles() {
	entries, err := os.ReadDir(r.subtitles)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && !strings.HasPrefix(e.Name(), extracting) && time.Since(info.ModTime()) > subtitlesKept {
			_ = os.RemoveAll(filepath.Join(r.subtitles, e.Name()))
		}
	}
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
