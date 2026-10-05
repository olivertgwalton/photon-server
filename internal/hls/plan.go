// Package hls serves a copy of a title as HLS: fragmented MP4 cut at the copy's own keyframes, so
// video is copied rather than encoded and every segment is exactly the length its playlist says.
package hls

import (
	"fmt"
	"math"
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
// part's initialisation from init(part), and a discontinuity where one part gives way to the next.
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
			fmt.Fprintf(&b, "#EXT-X-MAP:URI=%q\n", init(s.Part))
			part = s.Part
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", (s.End - s.Start).Seconds(), segment(n))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}
