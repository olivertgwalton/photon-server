// Package hls serves a version of a title as HLS: fragmented MP4 or MPEG-TS cut at the version's own
// keyframes, so video is copied rather than encoded and every segment is exactly the length its
// playlist says.
package hls

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SegmentLength is the target: Apple's recommendation, and what Jellyfin uses for remuxing.
const SegmentLength = 6 * time.Second

// Part is one file of a version: how long it runs, and its video keyframes' presentation times.
type Part struct {
	Duration  time.Duration
	Keyframes []time.Duration
}

// Forced is the keyframes an encode of a part is told to make, one every SegmentLength, so its
// segments are all that long.
func Forced(duration time.Duration) []time.Duration {
	var k []time.Duration
	for t := time.Duration(0); t < duration; t += SegmentLength {
		k = append(k, t)
	}
	return k
}

// extent is how far of a part is planned: what it says it runs, or its last keyframe where that
// reaches further. ffprobe answers no duration at all for a raw elementary stream, which cannot
// have one without being read to its end, and a container may say less than its own frames show.
// Planning to the last keyframe loses what follows it, at most a segment, instead of the part.
func (p Part) extent() time.Duration {
	if len(p.Keyframes) == 0 {
		return p.Duration
	}
	return max(p.Duration, slices.Max(p.Keyframes))
}

// Segment is one segment of the playlist: which part it is cut from, and where it starts and ends
// in that part's own time.
type Segment struct {
	Part       int
	Start, End time.Duration
}

// SubtitlePieceLength is the least a piece of a subtitle playlist runs, as Jellyfin cuts its: cut
// at every segment, a subtitle playlist is as long as the video's for a few bytes of text a line.
const SubtitlePieceLength = 30 * time.Second

// piece is the segments, first to last, of one part, that a subtitle playlist names as one.
type piece struct{ first, last int }

// pieces groups a plan's segments into pieces of at least SubtitlePieceLength, the last of a part
// what is left of it.
func pieces(plan []Segment) []piece {
	var out []piece
	for n, seg := range plan {
		if k := len(out) - 1; k >= 0 && plan[out[k].first].Part == seg.Part &&
			plan[out[k].last].End-plan[out[k].first].Start < SubtitlePieceLength {
			out[k].last = n
			continue
		}
		out = append(out, piece{n, n})
	}
	return out
}

// Plan cuts each part at its first keyframe at or after every SegmentLength, so a segment always
// begins on a keyframe and copied video can start it. Segments never cross from one part to the
// next.
func Plan(parts []Part) []Segment {
	var segs []Segment
	for p, part := range parts {
		end := part.extent()
		start, next := time.Duration(0), SegmentLength
		for _, k := range part.Keyframes {
			if k >= next && k > start && k < end {
				segs = append(segs, Segment{Part: p, Start: start, End: k})
				start = k
				for next <= k {
					next += SegmentLength
				}
			}
		}
		if end > start {
			segs = append(segs, Segment{Part: p, Start: start, End: end})
		}
	}
	return segs
}

// Playlist writes the media playlist of a plan at EXT-X-VERSION version: every segment's address
// from segment(n), each part's initialisation from init(part) where there is one, and a
// discontinuity where one part gives way to the next.
func Playlist(segs []Segment, version int, init func(part int) string, segment func(n int) string) string {
	var b strings.Builder
	longest := time.Duration(0)
	for _, s := range segs {
		longest = max(longest, s.End-s.Start)
	}
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:%d\n#EXT-X-TARGETDURATION:%d\n", version, int(math.Ceil(longest.Seconds())))
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	part := -1
	for n, s := range segs {
		if s.Part != part {
			if part >= 0 {
				b.WriteString("#EXT-X-DISCONTINUITY\n")
			}
			if init != nil {
				fmt.Fprintf(&b, "#EXT-X-MAP:URI=%q\n", init(s.Part))
			}
			part = s.Part
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", (s.End - s.Start).Seconds(), segment(n))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// Master writes the master playlist: the video's variant and its subtitles as renditions in one
// group, the first marked default chosen by default. Names within a group must differ, so a
// repeated one is numbered.
func Master(subs []Subtitle, v Variant, version int, video string, subtitle func(track int) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:%d\n#EXT-X-INDEPENDENT-SEGMENTS\n", version)
	seen := map[string]int{}
	chosen := false
	for n, s := range subs {
		name := cmp.Or(s.Name, s.Language, "Subtitles")
		if seen[name]++; seen[name] > 1 {
			name += " " + strconv.Itoa(seen[name])
		}
		fmt.Fprintf(&b, "#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=\"%s\"", quotable.Replace(name))
		if s.Language != "" {
			fmt.Fprintf(&b, ",LANGUAGE=%q", s.Language)
		}
		def := s.Default && !chosen
		chosen = chosen || def
		fmt.Fprintf(&b, ",DEFAULT=%s,AUTOSELECT=YES,FORCED=%s", yes(def), yes(s.Forced))
		if s.HearingImpaired {
			b.WriteString(",CHARACTERISTICS=\"public.accessibility.transcribes-spoken-dialog,public.accessibility.describes-music-and-sound\"")
		}
		fmt.Fprintf(&b, ",URI=%q\n", subtitle(n))
	}
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d", max(v.BandwidthKbps, 1)*1000)
	if len(v.Codecs) > 0 {
		fmt.Fprintf(&b, ",CODECS=\"%s\"", strings.Join(v.Codecs, ","))
	}
	if v.SupplementalCodecs != "" {
		fmt.Fprintf(&b, ",SUPPLEMENTAL-CODECS=%q", v.SupplementalCodecs)
	}
	if v.Range != "" {
		b.WriteString(",VIDEO-RANGE=" + v.Range)
	}
	if v.Width > 0 && v.Height > 0 {
		fmt.Fprintf(&b, ",RESOLUTION=%dx%d", v.Width, v.Height)
	}
	// Apple's players refuse an HDR variant that does not say its frame rate.
	if v.FrameRate > 0 {
		fmt.Fprintf(&b, ",FRAME-RATE=%.3f", v.FrameRate)
	}
	if len(subs) > 0 {
		b.WriteString(",SUBTITLES=\"subs\"")
	}
	b.WriteString("\n" + video + "\n")
	return b.String()
}

// quotable makes a name an RFC 8216 quoted-string, which holds no double quote or line break.
var quotable = strings.NewReplacer(`"`, "'", "\r", " ", "\n", " ")

func yes(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}
