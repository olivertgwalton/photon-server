package hls

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Subtitle is a text subtitle published beside the video as WebVTT: a stream every part of the copy
// carries, read by the runs that remux them, or a file beside the copy, timed on its timeline.
type Subtitle struct {
	Name     string
	Language string
	Default  bool
	Forced   bool
	// HearingImpaired is published as Apple's characteristic for it.
	HearingImpaired bool
	Stream          *int
	File            *SubtitleSource
}

// SubtitleSource is a file holding a subtitle: an external file, or a stream of one of a copy's
// parts.
type SubtitleSource struct {
	Open func() (*os.File, error)
	// Stream is the file's stream to read, by its index; nil for a subtitle file.
	Stream *int
	// Part is the part a stream is read from, and Streams every stream it has: its subtitles are
	// read out together and kept under its id.
	Part    uuid.UUID
	Streams []domain.Stream
	// Language is a subtitle file's, which says what it was written in where it is not UTF-8.
	Language string
}

// Cue is one WebVTT cue on the copy's timeline: its timing line's settings and its text.
type Cue struct {
	Start, End time.Duration
	Settings   string
	Text       string
}

// TextSubtitle reports whether a subtitle codec is plain text, which WebVTT carries: not pictures,
// and not styled.
func TextSubtitle(codec string) bool {
	switch codec {
	case "subrip", "webvtt", "mov_text", "text", "sami", "microdvd", "subviewer", "realtext":
		return true
	}
	return false
}

// StyledSubtitle reports whether a subtitle codec is text whose look is its own (fonts, placing,
// motion), which WebVTT would lose: drawn by the player, or by libass into the video.
func StyledSubtitle(codec string) bool { return codec == "ass" || codec == "ssa" }

// StyledName is the name a styled stream of a part is read out as, as it is, in the folder
// Extracted answers.
func StyledName(stream int) string { return strconv.Itoa(stream) + ".ass" }

// FontsDir is the folder of the fonts a part carries for its styled streams, in the folder
// Extracted answers.
const FontsDir = "fonts"

const (
	// subtitlesKept is how long a part's extracted subtitles are kept unread: a month, as the
	// previews of a missing file are.
	subtitlesKept = 30 * 24 * time.Hour
	// extractRetry is how long an extraction that failed is answered with its failure before a file
	// is read again, so a file ffmpeg cannot read is not read whole for every request.
	extractRetry = 10 * time.Minute
	// extracting names the folders extractions are written in before they are moved into place.
	extracting = ".making-"
)

// extraction is a part's subtitles being read out, which every request for any of them waits on.
type extraction struct {
	done chan struct{}
	err  error
	at   time.Time
}

// convert reads a subtitle file's cues.
func (r *Remuxer) convert(ctx context.Context, src SubtitleSource) ([]Cue, error) {
	vtt, err := r.WebVTT(ctx, src.Open, src.Language)
	if err != nil {
		return nil, err
	}
	var cues []Cue
	err = readVTT(strings.NewReader(vtt), func(c Cue) { cues = append(cues, c) })
	return cues, err
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
	cmd := media.NewCommand(ctx, media.Foreground, []*os.File{f}, r.tools.FFmpeg.Path, a...)
	out, err := cmd.Output()
	return string(out), cmd.Err(err)
}

// Extracted answers the folder a part's styled subtitle streams are read out into, with want in
// it, reading them out if no one is. Reading one means reading the whole file, so every styled
// stream of the part is read out in the same pass and kept as it is (see StyledName), for this
// playback and the next, with the fonts the file carries for them (see FontsDir). A folder lacking
// want, kept from before a stream was read out so, is read again. The pass outlives the request
// that started it: a player that gives up waiting finds it further on when it asks again.
func (r *Remuxer) Extracted(ctx context.Context, src SubtitleSource, want string) (string, error) {
	dir := filepath.Join(r.subtitles, src.Part.String())
	r.mu.Lock()
	x, ok := r.extractions[src.Part]
	if ok && closed(x.done) && time.Since(x.at) > extractRetry {
		ok = false
	}
	if !ok {
		if _, err := os.Stat(filepath.Join(dir, want)); err == nil {
			r.mu.Unlock()
			now := time.Now()
			return dir, os.Chtimes(dir, now, now)
		}
		x = &extraction{done: make(chan struct{})}
		r.extractions[src.Part] = x
		go func() {
			x.err = r.extract(context.WithoutCancel(ctx), src, dir)
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
		return dir, x.err
	case <-ctx.Done():
		return "", context.Cause(ctx)
	}
}

// extract writes each of a part's styled subtitle streams into dir in one read of the file, with
// the fonts its header lists, each named by its stream: FFmpeg refuses to write one under the name
// the file gives it unless that is plain ASCII with no spaces.
func (r *Remuxer) extract(ctx context.Context, src SubtitleSource, dir string) error {
	f, err := src.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	ctx, cancel := media.Within(ctx, r.tools.FFmpeg.Path, media.WholeRun(f))
	defer cancel()
	made, err := os.MkdirTemp(r.subtitles, extracting)
	if err != nil {
		return err
	}
	defer os.RemoveAll(made)
	fonts := filepath.Join(made, FontsDir)
	if err := os.Mkdir(fonts, 0o750); err != nil {
		return err
	}
	a := append(fdInput(), "-y")
	if slices.ContainsFunc(src.Streams, func(s domain.Stream) bool { return StyledSubtitle(s.Codec) }) {
		carried, err := r.tools.Fonts(ctx, f)
		if err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		for _, font := range carried {
			n := strconv.Itoa(font.Index)
			a = append(a, "-dump_attachment:"+n, filepath.Join(fonts, n+font.Ext))
		}
	}
	a = append(a, "-i", "fd:")
	for _, s := range src.Streams {
		n := strconv.Itoa(s.Index)
		if s.Kind == domain.StreamSubtitle && StyledSubtitle(s.Codec) {
			a = append(a, "-map", "0:"+n, "-c:s", "copy", "-f", "ass", filepath.Join(made, StyledName(s.Index)))
		}
	}
	cmd := media.NewCommand(ctx, media.Foreground, []*os.File{f}, r.tools.FFmpeg.Path, a...)
	if err := cmd.Run(); err != nil {
		return cmd.Err(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(made, dir)
}

// styledLayer is styled text libass draws into a part's video: its file, the fonts the part
// carries for it, the charset of a file beside the copy that is not UTF-8, and where on the copy's
// timeline, which such a file is timed on, the part starts.
type styledLayer struct {
	file, fonts, charset string
	offset               time.Duration
}

// styledName is what a styled file beside the copy is copied to in a playback's folder.
const styledName = "styled.ass"

// styled readies styled text for drawing into the video of a part starting at offset: a stream of
// it, read out with its fonts, or a file beside the copy, copied into the playback's folder, where
// FFmpeg opens it by name.
func (r *Remuxer) styled(ctx context.Context, s *session, src SubtitleSource, offset time.Duration) (*styledLayer, error) {
	if src.Stream != nil {
		dir, err := r.Extracted(ctx, src, StyledName(*src.Stream))
		if err != nil {
			return nil, err
		}
		return &styledLayer{file: filepath.Join(dir, StyledName(*src.Stream)), fonts: filepath.Join(dir, FontsDir)}, nil
	}
	f, err := src.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		return nil, err
	}
	charset, err := subtitleCharset(bytes.NewReader(b), src.Language)
	if err != nil {
		return nil, err
	}
	if err := s.root.WriteFile(styledName, b, 0o640); err != nil {
		return nil, err
	}
	return &styledLayer{file: filepath.Join(s.dir, styledName), charset: charset, offset: offset}, nil
}

// filter is the layer drawn in by FFmpeg's subtitles filter. A file beside the copy is drawn with
// the part's frames moved onto the copy's timeline and back.
func (l styledLayer) filter() string {
	f := "subtitles=f=" + filterValue(l.file)
	if l.fonts != "" {
		f += ":fontsdir=" + filterValue(l.fonts)
	}
	if l.charset != "" {
		f += ":charenc=" + filterValue(l.charset)
	}
	if l.offset == 0 {
		return f
	}
	at := strconv.FormatFloat(l.offset.Seconds(), 'f', 6, 64)
	return "setpts=PTS+" + at + "/TB," + f + ",setpts=PTS-" + at + "/TB"
}

// filterValue escapes a filter's option value twice over, as FFmpeg reads a filtergraph: once
// for the option's own reading, once for the graph's.
func filterValue(v string) string {
	escape := func(s, special string) string {
		var b strings.Builder
		for _, c := range s {
			if strings.ContainsRune(special, c) {
				b.WriteByte('\\')
			}
			b.WriteRune(c)
		}
		return b.String()
	}
	return escape(escape(v, `\':`), `\'[],;`)
}

// SweepSubtitles removes the subtitles of parts no one has read for subtitlesKept.
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

// readVTT reads the cues of a WebVTT or SubRip file as each is whole, dropping a header, notes,
// styles and cue names or numbers.
func readVTT(r io.Reader, each func(Cue)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	var cue *Cue
	var text []string
	flush := func() {
		if cue != nil {
			cue.Text = strings.Join(text, "\n")
			each(*cue)
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
	return sc.Err()
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

// vttTime reads "hh:mm:ss.ttt" or "mm:ss.ttt", or SubRip's "hh:mm:ss,ttt".
func vttTime(s string) (time.Duration, bool) {
	i := strings.LastIndexAny(s, ".,")
	if i < 0 {
		return 0, false
	}
	clock, frac := s[:i], s[i+1:]
	if len(frac) != 3 {
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

// subRipMarkup is what FFmpeg's SubRip encoder writes that its WebVTT encoder leaves out: colours
// and ASS overrides such as placing. Bold, italic and underline are written alike by both.
var subRipMarkup = regexp.MustCompile(`(?i)</?font[^>]*>|\{\\[^}]*\}`)

// fromSubRip is SubRip text as FFmpeg's WebVTT encoder would have written it.
func fromSubRip(text string) string { return subRipMarkup.ReplaceAllString(text, "") }

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
