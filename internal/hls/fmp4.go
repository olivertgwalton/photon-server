package hls

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// clockOffset is added to every timestamp ffmpeg writes (-output_ts_offset), so a film's first
// frames, decoded before they are shown, never have a negative decode time that tfdt cannot hold.
const clockOffset = 10 * time.Second

// maxBox bounds one box read whole. Only an mdat, which is a fragment's pictures, is not read
// whole but copied to where it is kept.
const maxBox = 256 << 20

var errNotFMP4 = errors.New("hls: not fragmented MP4")

// stream reads the fragmented MP4 ffmpeg writes: its initialisation (ftyp and moov), then one
// fragment (moof and mdat) per video keyframe.
type stream struct {
	r     io.Reader
	video uint32
	scale uint32
}

// readInit reads the initialisation and learns the video track and its timescale.
func readInit(r io.Reader) (*stream, []byte, error) {
	s := &stream{r: r}
	var init []byte
	for {
		typ, box, err := s.box()
		if err != nil {
			return nil, nil, err
		}
		init = append(init, box...)
		if typ == "moov" {
			if err := s.tracks(box[8:]); err != nil {
				return nil, nil, err
			}
			return s, init, nil
		}
	}
}

// fragment is one fragment as far as it has been read: its moof and its mdat's header, the mdat's
// body of body bytes still to be read by write, and when its first video frame is shown, from the
// start of the file it was cut from.
type fragment struct {
	head  []byte
	body  int64
	shown time.Duration
}

// next reads up to the next fragment's pictures. io.EOF after the last.
func (s *stream) next() (fragment, error) {
	var f fragment
	for {
		head, err := s.header()
		if err != nil {
			if errors.Is(err, io.EOF) && f.head != nil {
				return f, io.ErrUnexpectedEOF
			}
			return f, err
		}
		typ := string(head[4:8])
		if typ == "mdat" {
			if f.head == nil {
				return f, fmt.Errorf("%w: an mdat with no moof", errNotFMP4)
			}
			f.head = append(f.head, head...)
			f.body = int64(binary.BigEndian.Uint32(head)) - 8
			return f, nil
		}
		box, err := s.rest(head)
		if err != nil {
			return f, err
		}
		if typ == "moof" {
			if f.head != nil {
				return f, fmt.Errorf("%w: a moof with no mdat", errNotFMP4)
			}
			if f.shown, err = s.firstShown(box[8:]); err != nil {
				return f, err
			}
			f.head = box
		}
	}
}

// write writes fragment f whole to w, reading its pictures as they are written.
func (s *stream) write(w io.Writer, f fragment) error {
	if _, err := w.Write(f.head); err != nil {
		return err
	}
	_, err := io.CopyN(w, s.r, f.body)
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// box reads one top-level box whole.
func (s *stream) box() (string, []byte, error) {
	head, err := s.header()
	if err != nil {
		return "", nil, err
	}
	box, err := s.rest(head)
	return string(head[4:8]), box, err
}

// header reads a box's size and type.
func (s *stream) header() ([]byte, error) {
	head := make([]byte, 8)
	if _, err := io.ReadFull(s.r, head); err != nil {
		return nil, err
	}
	if size := binary.BigEndian.Uint32(head); size < 8 {
		return nil, fmt.Errorf("%w: a box of %d bytes", errNotFMP4, size)
	}
	return head, nil
}

// rest reads the rest of the box head begins.
func (s *stream) rest(head []byte) ([]byte, error) {
	size := binary.BigEndian.Uint32(head)
	if size > maxBox {
		return nil, fmt.Errorf("%w: a box of %d bytes", errNotFMP4, size)
	}
	box := make([]byte, size)
	copy(box, head)
	if _, err := io.ReadFull(s.r, box[8:]); err != nil {
		return nil, io.ErrUnexpectedEOF
	}
	return box, nil
}

// children calls f with each box inside b.
func children(b []byte, f func(typ string, body []byte) error) error {
	for len(b) >= 8 {
		size := binary.BigEndian.Uint32(b)
		if size < 8 || int(size) > len(b) {
			return fmt.Errorf("%w: a box overruns its parent", errNotFMP4)
		}
		if err := f(string(b[4:8]), b[8:size]); err != nil {
			return err
		}
		b = b[size:]
	}
	return nil
}

// be32 reads a big-endian word, refusing to read past the end of b.
func be32(b []byte, at int) (uint32, error) {
	if at < 0 || at+4 > len(b) {
		return 0, fmt.Errorf("%w: a box too short for its fields", errNotFMP4)
	}
	return binary.BigEndian.Uint32(b[at:]), nil
}

// versioned reads a field placed by a full box's version: at v0 in version 0, at v1 in version 1.
func versioned(b []byte, v0, v1 int) (uint32, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("%w: an empty box", errNotFMP4)
	}
	if b[0] == 1 {
		return be32(b, v1)
	}
	return be32(b, v0)
}

// tracks finds the video track in moov and its timescale.
func (s *stream) tracks(moov []byte) error {
	err := children(moov, func(typ string, trak []byte) error {
		if typ != "trak" {
			return nil
		}
		var id, scale uint32
		var handler string
		err := children(trak, func(typ string, b []byte) error {
			var err error
			switch typ {
			case "tkhd":
				id, err = versioned(b, 12, 20)
			case "mdia":
				err = children(b, func(typ string, b []byte) error {
					var err error
					switch typ {
					case "mdhd":
						scale, err = versioned(b, 12, 20)
					case "hdlr":
						var h uint32
						h, err = be32(b, 8)
						handler = string(binary.BigEndian.AppendUint32(nil, h))
					}
					return err
				})
			}
			return err
		})
		if err == nil && handler == "vide" && s.video == 0 {
			s.video, s.scale = id, scale
		}
		return err
	})
	if err == nil && (s.video == 0 || s.scale == 0) {
		err = fmt.Errorf("%w: no video track", errNotFMP4)
	}
	return err
}

// firstShown reads when a fragment's first video sample is shown: its decode time (tfdt) plus its
// composition offset (trun), less the clock offset.
func (s *stream) firstShown(moof []byte) (time.Duration, error) {
	found := false
	var shown time.Duration
	err := children(moof, func(typ string, traf []byte) error {
		if typ != "traf" || found {
			return nil
		}
		var track uint32
		var decode uint64
		var offset int64
		err := children(traf, func(typ string, b []byte) error {
			var err error
			switch typ {
			case "tfhd":
				track, err = be32(b, 4)
			case "tfdt":
				var hi, lo uint32
				if lo, err = be32(b, 4); err == nil && len(b) > 0 && b[0] == 1 {
					hi = lo
					lo, err = be32(b, 8)
				}
				decode = uint64(hi)<<32 | uint64(lo)
			case "trun":
				offset, err = firstCompositionOffset(b)
			}
			return err
		})
		if err == nil && track == s.video {
			found = true
			units := int64(decode) + offset
			shown = time.Duration(units)*time.Second/time.Duration(s.scale) - clockOffset
		}
		return err
	})
	if err == nil && !found {
		err = fmt.Errorf("%w: a fragment with no video", errNotFMP4)
	}
	return shown, err
}

// firstCompositionOffset reads the first sample's composition offset from a trun, zero where it
// carries none.
func firstCompositionOffset(trun []byte) (int64, error) {
	head, err := be32(trun, 0)
	if err != nil {
		return 0, err
	}
	version, flags := byte(head>>24), head&0xffffff
	count, err := be32(trun, 4)
	if err != nil || flags&0x800 == 0 || count == 0 {
		return 0, err
	}
	at := 8
	for _, f := range []uint32{0x1, 0x4, 0x100, 0x200, 0x400} { // data offset, first sample flags; the sample's duration, size and flags
		if flags&f != 0 {
			at += 4
		}
	}
	raw, err := be32(trun, at)
	if version == 1 {
		return int64(int32(raw)), err
	}
	return int64(raw), err
}
