package hls

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// testdata/segments.ts is thirty seconds of 12 fps H.264 with B-frames and a keyframe every two
// seconds, and AAC, written by ffmpeg 9.0 as the remuxer writes MPEG-TS, with the clock offset.
func TestMPEGTSIsReadAsFragmentsThatEachDecodeAlone(t *testing.T) {
	data, err := os.ReadFile("testdata/segments.ts")
	if err != nil {
		t.Fatal(err)
	}
	s := readTS(bytes.NewReader(data), 0)
	var shown []time.Duration
	var out bytes.Buffer
	for {
		frag, err := s.next()
		if errors.Is(err, io.EOF) {
			break
		}
		at := out.Len()
		if err == nil {
			err = s.write(&out, frag)
		}
		if err != nil {
			t.Fatal(err)
		}
		shown = append(shown, frag.shown)
		// A PAT, a PMT, then the keyframe's first packet, marked a random access point.
		p := out.Bytes()[at:]
		if len(p) < 3*tsPacket || pid(p) != patPID || pid(p[tsPacket:]) != s.pmt || pid(p[2*tsPacket:]) != s.video {
			t.Fatalf("fragment %d begins with PIDs %d, %d, %d; want the PAT, the PMT (%d) and the video (%d)",
				len(shown)-1, pid(p), pid(p[tsPacket:]), pid(p[2*tsPacket:]), s.pmt, s.video)
		}
		if _, random := tsPayload(p[2*tsPacket:]); !random {
			t.Errorf("fragment %d begins with a packet that is no random access point", len(shown)-1)
		}
	}
	var want []time.Duration
	for k := range 15 {
		want = append(want, time.Duration(2*k)*time.Second)
	}
	if len(shown) != len(want) {
		t.Fatalf("fragments shown at %v, want %v", shown, want)
	}
	for i := range want {
		if shown[i] != want[i] {
			t.Errorf("fragment %d shown at %v, want %v", i, shown[i], want[i])
		}
	}
	// The tables added are numbered among the rest, so nothing reading it all takes one for lost.
	next := map[int]byte{}
	for p := out.Bytes(); len(p) >= tsPacket; p = p[tsPacket:] {
		if id := pid(p); id == patPID || id == s.pmt {
			cc := p[3] & 0x0f
			if want, ok := next[id]; ok && cc != want {
				t.Fatalf("PID %d counts %d where %d follows", id, cc, want)
			}
			next[id] = (cc + 1) & 0x0f
		}
	}

	s = readTS(bytes.NewReader(data[:len(data)-100]), 0)
	for err == nil {
		var frag fragment
		if frag, err = s.next(); err == nil {
			err = s.write(io.Discard, frag)
		}
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a stream cut short ended with %v, want io.ErrUnexpectedEOF", err)
	}
}

// A timestamp past 2^33 ticks wraps to nought; the keyframe after it is still later.
func TestAWrappedTimestampCountsOn(t *testing.T) {
	s := readTS(nil, 26*time.Hour)
	at := func(ticks int64) []byte {
		ticks %= tsWrap
		return []byte{
			0, 0, 1, 0xe0, 0, 0, 0x80, 0x80, 5,
			byte(0x21 | (ticks>>30&7)<<1), byte(ticks >> 22), byte(ticks>>15<<1 | 1), byte(ticks >> 7), byte(ticks<<1 | 1),
		}
	}
	for _, d := range []time.Duration{26 * time.Hour, 26*time.Hour + 30*time.Minute, 27 * time.Hour} {
		got, err := s.shown(at(int64(d+clockOffset) * 9 / 100_000))
		if err != nil || got != d {
			t.Errorf("a keyframe at %v read as %v (%v)", d, got, err)
		}
	}
}

func pid(p []byte) int { return int(p[1]&0x1f)<<8 | int(p[2]) }
