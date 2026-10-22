package hls

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
)

// testdata/dovi7.mp4 is two seconds of a UHD Blu-ray's Dolby Vision 7 FEL, and AAC, written as the
// remuxer writes them, each picture's slices then cut to a few bytes to keep it small; its RPUs and
// enhancement layer are whole. ../dovi/testdata/fel81.rpu is what dovi_tool makes of its RPUs as
// 8.1.
func TestProfile7IsSentAsProfile81(t *testing.T) {
	data, err := os.ReadFile("testdata/dovi7.mp4")
	if err != nil {
		t.Fatal(err)
	}
	s, init, err := readInit(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.profile81(init); err != nil {
		t.Fatal(err)
	}
	if i := bytes.Index(init, []byte("dvcC")); i < 0 || !bytes.Equal(init[i+4:i+9], []byte{1, 0, 8 << 1, 6<<3 | 0b101, 1 << 4}) {
		t.Errorf("dvcC is %x, want version 1.0, profile 8 level 6, an RPU and base layer, HDR10", init[i+4:i+9])
	}
	out := bytes.NewBuffer(bytes.Clone(init))
	for {
		f, err := s.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err == nil {
			err = s.write(out, f)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	before, after := samples(t, data), samples(t, out.Bytes())
	if !slices.EqualFunc(before[2], after[2], bytes.Equal) || len(before[2]) == 0 {
		t.Error("the audio changed")
	}
	if len(before[1]) != len(after[1]) {
		t.Fatalf("%d pictures, want %d", len(after[1]), len(before[1]))
	}
	var rpus [][]byte
	for i := range after[1] {
		in, got := nalUnits(t, before[1][i]), nalUnits(t, after[1][i])
		var want [][]byte
		for _, n := range in {
			if typ := n[0] >> 1 & 0x3f; typ != 62 && typ != 63 {
				want = append(want, n)
			}
		}
		var picture [][]byte
		for _, n := range got {
			switch n[0] >> 1 & 0x3f {
			case 62:
				rpus = append(rpus, n)
			case 63:
				t.Fatalf("picture %d keeps its enhancement layer", i)
			default:
				picture = append(picture, n)
			}
		}
		if !slices.EqualFunc(picture, want, bytes.Equal) {
			t.Fatalf("picture %d's base layer changed", i)
		}
	}
	converted, err := os.ReadFile("../dovi/testdata/fel81.rpu")
	if err != nil {
		t.Fatal(err)
	}
	var wantRPUs [][]byte
	for rpu := range bytes.SplitSeq(converted, []byte{0, 0, 0, 1}) {
		if len(rpu) > 0 {
			wantRPUs = append(wantRPUs, append([]byte{0x7c, 0x01}, bytes.TrimRight(rpu, "\x00")...))
		}
	}
	// dovi_tool writes them in the order shown, a file in the order decoded.
	slices.SortFunc(rpus, bytes.Compare)
	slices.SortFunc(wantRPUs, bytes.Compare)
	if !slices.EqualFunc(rpus, wantRPUs, bytes.Equal) {
		t.Errorf("%d RPUs, not dovi_tool's %d as 8.1", len(rpus), len(wantRPUs))
	}
}

// samples reads a fragmented MP4's samples by track, as a player finds them: by each trun's data
// offset and sizes.
func samples(t *testing.T, data []byte) map[uint32][][]byte {
	t.Helper()
	s, _, err := readInit(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	out := map[uint32][][]byte{}
	for {
		f, err := s.next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		moof := f.head[:len(f.head)-8]
		body := make([]byte, f.body)
		if _, err := io.ReadFull(s.r, body); err != nil {
			t.Fatal(err)
		}
		runs, err := trackRuns(moof[8:], len(moof)+8)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			at := r.start
			for _, n := range r.sizes {
				out[r.track] = append(out[r.track], body[at:at+n])
				at += n
			}
		}
	}
}

func nalUnits(t *testing.T, sample []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for len(sample) > 0 {
		n := int(sample[0])<<24 | int(sample[1])<<16 | int(sample[2])<<8 | int(sample[3])
		if n > len(sample)-4 {
			t.Fatal("a NAL unit overruns its sample")
		}
		out = append(out, sample[4:4+n])
		sample = sample[4+n:]
	}
	return out
}

// FuzzReadTrun is seeded with every trun in the fMP4 testdata. A trun gives every sample's size
// or none's.
func FuzzReadTrun(f *testing.F) {
	for _, name := range []string{"fragments.mp4", "dovi7.mp4"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		s, _, err := readInit(bytes.NewReader(data))
		if err != nil {
			f.Fatal(err)
		}
		for {
			frag, err := s.next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err == nil {
				_, err = io.CopyN(io.Discard, s.r, frag.body)
			}
			if err == nil {
				err = children(frag.head[8:len(frag.head)-8], func(typ string, traf []byte) error {
					if typ != "traf" {
						return nil
					}
					return children(traf, func(typ string, b []byte) error {
						if typ == "trun" {
							f.Add(b, -1)
						}
						return nil
					})
				})
			}
			if err != nil {
				f.Fatal(err)
			}
		}
	}
	f.Fuzz(func(t *testing.T, b []byte, size int) {
		r, err := readTrun(b, size)
		if err == nil && r.sized != nil && len(r.sized) != len(r.sizes) {
			t.Fatalf("%d sample sizes, %d of them given", len(r.sizes), len(r.sized))
		}
	})
}

// A fragment's pictures to rewrite are read whole, so a fragment claiming more than maxBox of them
// is refused before any is read.
func TestAnOversizedFragmentIsNotRewritten(t *testing.T) {
	data, err := os.ReadFile("testdata/dovi7.mp4")
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Clone(data)
	at := bytes.Index(data, []byte("mdat")) - 4
	binary.BigEndian.PutUint32(data[at:], maxBox+9)
	s, init, err := readInit(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.profile81(init); err != nil {
		t.Fatal(err)
	}
	f, err := s.next()
	if err == nil {
		err = s.write(io.Discard, f)
	}
	if !errors.Is(err, errNotFMP4) {
		t.Errorf("a fragment of %d bytes: err = %v, want %v", maxBox+1, err, errNotFMP4)
	}
}
