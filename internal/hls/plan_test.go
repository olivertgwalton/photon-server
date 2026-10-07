package hls

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func seconds(fs ...float64) []time.Duration {
	out := make([]time.Duration, len(fs))
	for i, f := range fs {
		out[i] = time.Duration(f * float64(time.Second))
	}
	return out
}

func TestPlanCutsAtTheFirstKeyframeOfEachSixSeconds(t *testing.T) {
	parts := []Part{
		// Keyframes every 2.5 s, one missing around 15 s, and a part that ends mid-gop.
		{Duration: 23 * time.Second, Keyframes: seconds(0, 2.5, 5, 7.5, 10, 12.5, 17.5, 20, 22.5)},
		{Duration: 4 * time.Second, Keyframes: seconds(0, 2)},
	}
	var got []string
	for _, s := range Plan(parts) {
		got = append(got, strconv.Itoa(s.Part)+":"+s.Start.String()+"-"+s.End.String())
	}
	want := []string{"0:0s-7.5s", "0:7.5s-12.5s", "0:12.5s-20s", "0:20s-23s", "1:0s-4s"}
	if !slices.Equal(got, want) {
		t.Errorf("Plan = %q, want %q", got, want)
	}
}

func TestPlaylistMarksWhereOnePartGivesWayToTheNext(t *testing.T) {
	segs := []Segment{{0, 0, 7500 * time.Millisecond}, {0, 7500 * time.Millisecond, 12 * time.Second}, {1, 0, 4 * time.Second}}
	got := Playlist(segs, 7, func(p int) string { return "init" + strconv.Itoa(p) + ".mp4" }, func(n int) string { return strconv.Itoa(n) + ".m4s" })
	want := strings.Join([]string{
		"#EXTM3U", "#EXT-X-VERSION:7", "#EXT-X-TARGETDURATION:8", "#EXT-X-PLAYLIST-TYPE:VOD", "#EXT-X-INDEPENDENT-SEGMENTS",
		`#EXT-X-MAP:URI="init0.mp4"`, "#EXTINF:7.500000,", "0.m4s", "#EXTINF:4.500000,", "1.m4s",
		"#EXT-X-DISCONTINUITY", `#EXT-X-MAP:URI="init1.mp4"`, "#EXTINF:4.000000,", "2.m4s", "#EXT-X-ENDLIST", "",
	}, "\n")
	if got != want {
		t.Errorf("Playlist =\n%s\nwant\n%s", got, want)
	}
}
