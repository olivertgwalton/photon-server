package hls

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/olivertgwalton/photon-server/internal/dovi"
)

// profile81 makes a stream of Dolby Vision profile 7 one of 8.1, as Apple's players take it: the
// initialisation's configuration record, patched here, says 8.1 with no enhancement layer, and
// write takes each sample's enhancement layer out and rewrites its RPU.
func (s *stream) profile81(init []byte) error {
	length := 0
	found := false
	err := descend(init, []string{"moov", "trak", "mdia", "minf", "stbl", "stsd"}, func(stsd []byte) error {
		if len(stsd) < 8 {
			return fmt.Errorf("%w: an empty stsd", errNotFMP4)
		}
		return children(stsd[8:], func(typ string, entry []byte) error {
			if typ != "dvh1" || len(entry) < 78 {
				return nil
			}
			return children(entry[78:], func(typ string, b []byte) error {
				switch {
				case typ == "hvcC" && len(b) > 21:
					length = int(b[21]&3) + 1
				case typ == "dvcC" && len(b) >= 5:
					if b[2]>>1 != 7 {
						return fmt.Errorf("hls: Dolby Vision profile %d, not 7", b[2]>>1)
					}
					level := (b[2]&1)<<5 | b[3]>>3
					b[2] = 8<<1 | level>>5
					b[3] = (level&31)<<3 | 1<<2 | 1 // an RPU and a base layer, no enhancement layer
					b[4] = 1 << 4                   // the base layer is HDR10
					found = true
				}
				return nil
			})
		})
	})
	if err == nil && (!found || length == 0) {
		err = fmt.Errorf("%w: no Dolby Vision HEVC sample entry", errNotFMP4)
	}
	if err != nil {
		return err
	}
	s.rewrite = func(sample []byte) ([]byte, error) { return profile81Sample(sample, length) }
	return nil
}

// descend calls f with the body of every box at path's end.
func descend(b []byte, path []string, f func([]byte) error) error {
	return children(b, func(typ string, body []byte) error {
		switch {
		case typ != path[0]:
			return nil
		case len(path) == 1:
			return f(body)
		}
		return descend(body, path[1:], f)
	})
}

// profile81Sample takes the enhancement layer's NAL units out of an HEVC sample, its NAL units
// each after its length in length bytes, and rewrites its RPU as profile 8.1.
func profile81Sample(b []byte, length int) ([]byte, error) {
	out := make([]byte, 0, len(b))
	for len(b) > 0 {
		if len(b) < length {
			return nil, errors.New("hls: a sample ends inside a NAL unit's length")
		}
		n := 0
		for _, c := range b[:length] {
			n = n<<8 | int(c)
		}
		if n < 2 || len(b)-length < n {
			return nil, errors.New("hls: a NAL unit overruns its sample")
		}
		nal := b[length : length+n]
		b = b[length+n:]
		switch nal[0] >> 1 & 0x3f {
		case 63: // UNSPEC63, the enhancement layer
			continue
		case 62: // UNSPEC62, the RPU
			var err error
			if nal, err = dovi.Profile81(nal); err != nil {
				return nil, err
			}
		}
		for i := length - 1; i >= 0; i-- {
			out = append(out, byte(len(nal)>>(8*i)))
		}
		out = append(out, nal...)
	}
	return out, nil
}

// trackRun is a trun: its track, where its samples begin in the mdat, their sizes, and the fields
// in the moof to rewrite once they move or change size.
type trackRun struct {
	track  uint32
	start  int
	sizes  []int
	offset []byte
	sized  [][]byte
}

// writeRewritten writes fragment f with its video samples rewritten, every sample moved to where
// the ones rewritten before it leave it, and the moof's offsets and sizes made to match.
func (s *stream) writeRewritten(w io.Writer, f fragment) error {
	moof := slices.Clone(f.head[:len(f.head)-8])
	body := make([]byte, f.body)
	if _, err := io.ReadFull(s.r, body); err != nil {
		return io.ErrUnexpectedEOF
	}
	runs, err := trackRuns(moof[8:], len(moof)+8)
	if err != nil {
		return err
	}
	slices.SortFunc(runs, func(a, b trackRun) int { return cmp.Compare(a.start, b.start) })
	out := make([]byte, 0, len(body))
	at := 0
	for _, r := range runs {
		if r.start < at || r.start > len(body) {
			return fmt.Errorf("%w: trun data overlaps or overruns its mdat", errNotFMP4)
		}
		out = append(out, body[at:r.start]...)
		at = r.start
		binary.BigEndian.PutUint32(r.offset, uint32(len(moof)+8+len(out)))
		for i, n := range r.sizes {
			if n > len(body)-at {
				return fmt.Errorf("%w: a sample overruns its mdat", errNotFMP4)
			}
			sample := body[at : at+n]
			at += n
			if r.track == s.video {
				if r.sized == nil {
					return fmt.Errorf("%w: a video trun with no sample sizes", errNotFMP4)
				}
				if sample, err = s.rewrite(sample); err != nil {
					return err
				}
				binary.BigEndian.PutUint32(r.sized[i], uint32(len(sample)))
			}
			out = append(out, sample...)
		}
	}
	out = append(out, body[at:]...)
	head := binary.BigEndian.AppendUint32(nil, uint32(len(out)+8))
	for _, b := range [][]byte{moof, append(head, "mdat"...), out} {
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	return nil
}

// trackRuns reads a moof's truns, whose data offsets count from the moof's start, as
// default_base_moof writes them; base is where its mdat's body begins.
func trackRuns(moof []byte, base int) ([]trackRun, error) {
	var runs []trackRun
	err := children(moof, func(typ string, traf []byte) error {
		if typ != "traf" {
			return nil
		}
		var track uint32
		size := -1
		return children(traf, func(typ string, b []byte) error {
			switch typ {
			case "tfhd":
				head, err := be32(b, 0)
				if err != nil {
					return err
				}
				if head&0x1 != 0 {
					return fmt.Errorf("%w: a tfhd with a base data offset", errNotFMP4)
				}
				if track, err = be32(b, 4); err != nil {
					return err
				}
				at := 8
				for _, f := range []uint32{0x2, 0x8} { // sample description index, default duration
					if head&f != 0 {
						at += 4
					}
				}
				if head&0x10 != 0 {
					v, err := be32(b, at)
					size = int(v)
					return err
				}
			case "trun":
				r, err := readTrun(b, size)
				r.track = track
				r.start -= base
				runs = append(runs, r)
				return err
			}
			return nil
		})
	})
	return runs, err
}

// readTrun reads a trun's data offset and sample sizes, each sample size size where it gives none.
func readTrun(b []byte, size int) (trackRun, error) {
	var r trackRun
	head, err := be32(b, 0)
	if err != nil {
		return r, err
	}
	count, err := be32(b, 4)
	if err != nil {
		return r, err
	}
	if head&0x1 == 0 {
		return r, fmt.Errorf("%w: a trun with no data offset", errNotFMP4)
	}
	offset, err := be32(b, 8)
	if err != nil {
		return r, err
	}
	r.start, r.offset = int(int32(offset)), b[8:12]
	at := 12
	if head&0x4 != 0 { // first sample flags
		at += 4
	}
	for range count {
		if head&0x100 != 0 { // duration
			at += 4
		}
		switch n, err := be32(b, at); {
		case head&0x200 != 0 && err != nil:
			return r, err
		case head&0x200 != 0:
			r.sizes = append(r.sizes, int(n))
			r.sized = append(r.sized, b[at:at+4])
			at += 4
		case size < 0:
			return r, fmt.Errorf("%w: a trun with no sample sizes", errNotFMP4)
		default:
			r.sizes = append(r.sizes, size)
		}
		for _, f := range []uint32{0x400, 0x800} { // flags, composition offset
			if head&f != 0 {
				at += 4
			}
		}
	}
	return r, nil
}
