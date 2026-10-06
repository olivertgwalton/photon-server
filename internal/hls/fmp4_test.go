package hls

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// testdata/fragments.mp4 is thirty seconds of 12 fps H.264 with B-frames and a keyframe every two
// seconds, and AAC, written by jellyfin-ffmpeg 8.1 as the remuxer writes it: fragmented at each
// keyframe, with frag_discont and the clock offset.
func TestFragmentsAreReadWithWhenTheyAreShown(t *testing.T) {
	data, err := os.ReadFile("testdata/fragments.mp4")
	if err != nil {
		t.Fatal(err)
	}
	s, init, err := readInit(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if string(init[4:8]) != "ftyp" || !bytes.Contains(init, []byte("moov")) {
		t.Errorf("init begins %q, want ftyp then moov", init[4:8])
	}
	var shown []time.Duration
	total := len(init)
	for {
		frag, err := s.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err == nil {
			var b bytes.Buffer
			err = s.write(&b, frag)
			total += b.Len()
		}
		if err != nil {
			t.Fatal(err)
		}
		shown = append(shown, frag.shown)
	}
	var want []time.Duration
	for k := range 15 {
		want = append(want, time.Duration(2*k)*time.Second)
	}
	if len(shown) != len(want) {
		t.Fatalf("fragments shown at %v, want %v", shown, want)
	}
	for i := range want {
		if d := shown[i] - want[i]; d < -time.Millisecond || d > time.Millisecond {
			t.Errorf("fragment %d shown at %v, want %v: the decode time plus the B-frames' delay", i, shown[i], want[i])
		}
	}
	if total != len(data) {
		t.Errorf("read %d bytes of %d: every box should land in the init or a fragment", total, len(data))
	}

	s, _, err = readInit(bytes.NewReader(data[:len(data)-100]))
	if err != nil {
		t.Fatal(err)
	}
	for err == nil {
		var frag fragment
		if frag, err = s.next(); err == nil {
			err = s.write(io.Discard, frag)
		}
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a file cut short ended with %v, want io.ErrUnexpectedEOF", err)
	}
}
