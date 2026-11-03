package media

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.senan.xyz/taglib"
)

// Lyrics are a track's words, line by line, each where it is sung when they are timed, as an LRC
// file times them.
type Lyrics struct {
	Timing Timing
	Lines  []LyricLine
}

// Timing is how far lyrics say where they are sung: line by line, or not at all.
type Timing string

const (
	TimedByLine Timing = "line"
	Untimed     Timing = "none"
)

// LyricLine is a line of lyrics, and where it starts where they are timed.
type LyricLine struct {
	Start time.Duration
	Text  string
}

var (
	// lrcStamp is a line's time, [mm:ss], [mm:ss.xx] or [mm:ss.xxx], several of which may lead a line
	// sung more than once; lrcTag is an LRC file's [key:value], which names no time.
	lrcStamp = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	lrcTag   = regexp.MustCompile(`^\[([a-zA-Z#]+):(.*)\]$`)
	// lrcWord is an enhanced LRC file's time of a word, <mm:ss.xx>, which lyrics shown by the line
	// leave out.
	lrcWord = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)
)

// ParseLyrics reads lyrics as an LRC file or a tag gives them: timed where any line is, in the
// order they are sung, an LRC file's offset taken off each; else each line as it is written.
func ParseLyrics(text string) Lyrics {
	var timed, plain []LyricLine
	var offset time.Duration
	for line := range strings.Lines(strings.ReplaceAll(text, "\r\n", "\n")) {
		line = strings.TrimSpace(line)
		var starts []time.Duration
		for {
			m := lrcStamp.FindStringSubmatch(line)
			if m == nil {
				break
			}
			starts = append(starts, lrcTime(m[1], m[2], m[3]))
			line = line[len(m[0]):]
		}
		if len(starts) == 0 {
			if m := lrcTag.FindStringSubmatch(line); m != nil {
				// A positive offset shows the lines that much sooner.
				if strings.EqualFold(m[1], "offset") {
					if ms, err := strconv.Atoi(strings.TrimSpace(m[2])); err == nil {
						offset = time.Duration(ms) * time.Millisecond
					}
				}
				continue
			}
			if line != "" {
				plain = append(plain, LyricLine{Text: line})
			}
			continue
		}
		words := strings.TrimSpace(lrcWord.ReplaceAllString(line, ""))
		for _, start := range starts {
			timed = append(timed, LyricLine{Start: start, Text: words})
		}
	}
	if len(timed) == 0 {
		return Lyrics{Timing: Untimed, Lines: plain}
	}
	for i := range timed {
		timed[i].Start = max(timed[i].Start-offset, 0)
	}
	slices.SortStableFunc(timed, func(a, b LyricLine) int { return cmp.Compare(a.Start, b.Start) })
	return Lyrics{Timing: TimedByLine, Lines: timed}
}

// lrcTime is an LRC stamp's minutes, seconds and fraction of a second, which is in hundredths
// where it has two digits and thousandths where it has three.
func lrcTime(minutes, seconds, fraction string) time.Duration {
	d := time.Duration(digits(minutes))*time.Minute + time.Duration(digits(seconds))*time.Second
	if fraction != "" {
		f := digits(fraction)
		for range 3 - len(fraction) {
			f *= 10
		}
		d += time.Duration(f) * time.Millisecond
	}
	return d
}

// digits is the number a run of ASCII digits writes, as lrcStamp matches them.
func digits(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// ReadLyrics reads the lyrics a sound file's tags hold, ID3's USLT, a Vorbis comment's LYRICS or
// MP4's ©lyr alike, as TagLib names them: none where it holds none.
func ReadLyrics(path string) (string, error) {
	raw, err := taglib.ReadTags(path)
	if err != nil {
		return "", err
	}
	return strings.Join(raw[taglib.Lyrics], "\n"), nil
}
