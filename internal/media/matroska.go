package media

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/bits"
)

var ebmlMagic = []byte{0x1a, 0x45, 0xdf, 0xa3}

// The Matroska elements an index is read from.
const (
	idSegment       = 0x18538067
	idSeekHead      = 0x114d9b74
	idSeek          = 0x4dbb
	idSeekID        = 0x53ab
	idSeekPosition  = 0x53ac
	idInfo          = 0x1549a966
	idTimecodeScale = 0x2ad7b1
	idTracks        = 0x1654ae6b
	idTrackEntry    = 0xae
	idTrackNumber   = 0xd7
	idTrackType     = 0x83
	idCues          = 0x1c53bb6b
	idCuePoint      = 0xbb
	idCueTime       = 0xb3
	idCuePositions  = 0xb7
	idCueTrack      = 0xf7
	idCluster       = 0x1f43b675

	trackVideo = 1
	// unknownSize is an element whose writer did not know how long it would be.
	unknownSize = -1
)

// matroskaKeyframes reads a Matroska or WebM file's Cues for its first video track: found through
// the SeekHead, wherever they are, or before the first Cluster. Every muxer writes a cue for each
// video keyframe, at the block's own timestamp. Only the elements the index needs are read.
func matroskaKeyframes(r *io.SectionReader) ([]int64, error) {
	size := r.Size()
	_, n, length, err := ebmlElement(r, 0)
	if err != nil {
		return nil, err
	}
	if length == unknownSize {
		return nil, fmt.Errorf("%w: an EBML header of unknown size", ErrNoIndex)
	}
	segment := int64(n) + length
	id, n, length, err := ebmlElement(r, segment)
	if err != nil {
		return nil, err
	}
	if id != idSegment {
		return nil, fmt.Errorf("%w: no Segment", ErrNoIndex)
	}
	start := segment + int64(n)
	end := size
	if length != unknownSize {
		end = min(size, start+length)
	}
	found := map[uint32]int64{}
	seen := map[int64]bool{}
	var seekHead func(at int64) error
	seekHead = func(at int64) error {
		if seen[at] {
			return nil
		}
		if len(seen) == maxHeads {
			return fmt.Errorf("%w: more than %d SeekHeads", ErrNoIndex, maxHeads)
		}
		seen[at] = true
		body, err := ebmlBody(r, at)
		if err != nil {
			return err
		}
		return ebmlChildren(body, func(id uint32, b []byte) error {
			if id != idSeek {
				return nil
			}
			var target uint32
			var pos int64 = -1
			err := ebmlChildren(b, func(id uint32, b []byte) error {
				switch id {
				case idSeekID:
					target = uint32(ebmlUint(b))
				case idSeekPosition:
					pos = int64(ebmlUint(b))
				}
				return nil
			})
			if err != nil || pos < 0 || start+pos >= end {
				return err
			}
			if target == idSeekHead {
				return seekHead(start + pos)
			}
			if _, ok := found[target]; !ok {
				found[target] = start + pos
			}
			return nil
		})
	}
	for at, heads := start, 0; at < end; heads++ {
		if heads == maxHeads {
			return nil, fmt.Errorf("%w: more than %d elements before the first Cluster", ErrNoIndex, maxHeads)
		}
		id, n, length, err := ebmlElement(r, at)
		if err != nil {
			return nil, err
		}
		if id == idCluster {
			break
		}
		if id == idSeekHead {
			if err := seekHead(at); err != nil {
				return nil, err
			}
		} else if _, ok := found[id]; !ok {
			found[id] = at
		}
		if length == unknownSize {
			break
		}
		at += int64(n) + length
	}
	for _, id := range []uint32{idInfo, idTracks, idCues} {
		if _, ok := found[id]; !ok {
			return nil, fmt.Errorf("%w: no element %x before the first Cluster or in the SeekHead", ErrNoIndex, id)
		}
	}
	scale, video, err := matroskaTrack(r, found[idInfo], found[idTracks])
	if err != nil {
		return nil, err
	}
	cues, err := ebmlBody(r, found[idCues])
	if err != nil {
		return nil, err
	}
	var pts []int64
	err = ebmlChildren(cues, func(id uint32, b []byte) error {
		if id != idCuePoint {
			return nil
		}
		var at uint64
		var ours bool
		err := ebmlChildren(b, func(id uint32, b []byte) error {
			switch id {
			case idCueTime:
				at = ebmlUint(b)
			case idCuePositions:
				return ebmlChildren(b, func(id uint32, b []byte) error {
					ours = ours || id == idCueTrack && ebmlUint(b) == video
					return nil
				})
			}
			return nil
		})
		if ours {
			pts = append(pts, int64(math.Round(float64(at)*float64(scale)/1e6)))
		}
		return err
	})
	return pts, err
}

// matroskaTrack reads the timestamp scale, in nanoseconds, and the first video track's number.
func matroskaTrack(r *io.SectionReader, info, tracks int64) (scale, video uint64, err error) {
	scale = 1_000_000
	body, err := ebmlBody(r, info)
	if err != nil {
		return 0, 0, err
	}
	err = ebmlChildren(body, func(id uint32, b []byte) error {
		if id == idTimecodeScale {
			scale = ebmlUint(b)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if scale == 0 {
		return 0, 0, fmt.Errorf("%w: a timestamp scale of zero", ErrNoIndex)
	}
	if body, err = ebmlBody(r, tracks); err != nil {
		return 0, 0, err
	}
	err = ebmlChildren(body, func(id uint32, b []byte) error {
		if id != idTrackEntry || video != 0 {
			return nil
		}
		var number, kind uint64
		err := ebmlChildren(b, func(id uint32, b []byte) error {
			switch id {
			case idTrackNumber:
				number = ebmlUint(b)
			case idTrackType:
				kind = ebmlUint(b)
			}
			return nil
		})
		if kind == trackVideo {
			video = number
		}
		return err
	})
	if err == nil && video == 0 {
		err = fmt.Errorf("%w: no video track", ErrNoIndex)
	}
	return scale, video, err
}

// ebmlElement reads the head of the element at off: its id, the head's length, and the body's
// length or unknownSize.
func ebmlElement(r *io.SectionReader, off int64) (id uint32, n int, length int64, err error) {
	var head [12]byte
	// A head is at most 12 bytes; one near the end of the file is shorter.
	got, rerr := r.ReadAt(head[:], off)
	if got == 0 {
		if rerr == nil || rerr == io.EOF {
			return 0, 0, 0, fmt.Errorf("%w: the file ends at %d", ErrNoIndex, off)
		}
		return 0, 0, 0, rerr
	}
	if rerr != nil && rerr != io.EOF {
		return 0, 0, 0, rerr
	}
	idLen, idValue, ok := vint(head[:got], 4)
	if !ok {
		return 0, 0, 0, fmt.Errorf("%w: a malformed element id at %d", ErrNoIndex, off)
	}
	sizeLen, size, ok := vint(head[idLen:got], 8)
	if !ok {
		return 0, 0, 0, fmt.Errorf("%w: a malformed element size at %d", ErrNoIndex, off)
	}
	// The id keeps its length marker; the size does not, and all ones means unknown.
	id = uint32(idValue | 1<<(7*idLen))
	length = int64(size)
	if size == 1<<(7*sizeLen)-1 {
		length = unknownSize
	} else if size > math.MaxInt64/2 {
		return 0, 0, 0, fmt.Errorf("%w: an element of %d bytes", ErrNoIndex, size)
	}
	return id, idLen + sizeLen, length, nil
}

// ebmlBody reads the body of the element at off whole.
func ebmlBody(r *io.SectionReader, off int64) ([]byte, error) {
	_, n, length, err := ebmlElement(r, off)
	if err != nil {
		return nil, err
	}
	if length == unknownSize {
		return nil, fmt.Errorf("%w: an index element of unknown size", ErrNoIndex)
	}
	return readWhole(r, off+int64(n), length)
}

// ebmlChildren calls f with each element inside b.
func ebmlChildren(b []byte, f func(id uint32, body []byte) error) error {
	for len(b) > 0 {
		idLen, idValue, ok := vint(b, 4)
		if !ok {
			return fmt.Errorf("%w: a malformed element id", ErrNoIndex)
		}
		sizeLen, size, ok := vint(b[idLen:], 8)
		if !ok || size > uint64(len(b)-idLen-sizeLen) {
			return fmt.Errorf("%w: an element overruns its parent", ErrNoIndex)
		}
		body := b[idLen+sizeLen : idLen+sizeLen+int(size)]
		if err := f(uint32(idValue|1<<(7*idLen)), body); err != nil {
			return err
		}
		b = b[idLen+sizeLen+int(size):]
	}
	return nil
}

// vint reads a variable-length integer of at most limit bytes: its length, and its value without
// the length marker.
func vint(b []byte, limit int) (int, uint64, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n := bits.LeadingZeros8(b[0]) + 1
	if n > limit || n > len(b) {
		return 0, 0, false
	}
	v := uint64(b[0]) & (0xff >> n)
	for _, c := range b[1:n] {
		v = v<<8 | uint64(c)
	}
	return n, v, true
}

// ebmlUint reads an unsigned integer element of up to eight bytes.
func ebmlUint(b []byte) uint64 {
	if len(b) > 8 {
		return 0
	}
	var full [8]byte
	copy(full[8-len(b):], b)
	return binary.BigEndian.Uint64(full[:])
}
