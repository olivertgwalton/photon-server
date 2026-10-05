// Package hls serves a copy of a title as HLS: fragmented MP4 cut at the copy's own keyframes, so
// video is copied rather than encoded and every segment is exactly the length its playlist says.
package hls

import (
	"cmp"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// SegmentLength is the target: Apple's recommendation, and what Jellyfin uses for remuxing.
const SegmentLength = 6 * time.Second

// Part is one file of a copy: how long it runs, and its video keyframes' presentation times.
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

// Segment is one segment of the playlist: which part it is cut from, and where it starts and ends
// in that part's own time.
type Segment struct {
	Part       int
	Start, End time.Duration
}

// Plan cuts each part at its first keyframe at or after every SegmentLength, so a segment always
// begins on a keyframe and copied video can start it. Segments never cross from one part to the
// next.
func Plan(parts []Part) []Segment {
	var segs []Segment
	for p, part := range parts {
		start, next := time.Duration(0), SegmentLength
		for _, k := range part.Keyframes {
			if k >= next && k > start && k < part.Duration {
				segs = append(segs, Segment{Part: p, Start: start, End: k})
				start = k
				for next <= k {
					next += SegmentLength
				}
			}
		}
		if part.Duration > start {
			segs = append(segs, Segment{Part: p, Start: start, End: part.Duration})
		}
	}
	return segs
}

// Playlist writes the media playlist of a plan: every segment's address from segment(n), each
// part's initialisation from init(part) where there is one, and a discontinuity where one part
// gives way to the next.
func Playlist(segs []Segment, init func(part int) string, segment func(n int) string) string {
	var b strings.Builder
	longest := time.Duration(0)
	for _, s := range segs {
		longest = max(longest, s.End-s.Start)
	}
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n", int(math.Ceil(longest.Seconds())))
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

// Master writes the master playlist: the video's variant at its bandwidth and its subtitles as
// renditions in one group, the first marked default chosen by default. Names within a group must
// differ, so a repeated one is numbered.
func Master(subs []Subtitle, kbps int, video string, subtitle func(track int) string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	seen := map[string]int{}
	chosen := false
	for n, s := range subs {
		name := cmp.Or(s.Name, s.Language, "Subtitles")
		if seen[name]++; seen[name] > 1 {
			name += " " + strconv.Itoa(seen[name])
		}
		fmt.Fprintf(&b, "#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=%q", name)
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
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d", max(kbps, 1)*1000)
	if len(subs) > 0 {
		b.WriteString(",SUBTITLES=\"subs\"")
	}
	b.WriteString("\n" + video + "\n")
	return b.String()
}

func yes(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}
