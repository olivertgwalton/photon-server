package media

import (
	"encoding/binary"
	"fmt"
	"io"
)

// isobmffStarts are the boxes an MP4 or QuickTime file begins with.
var isobmffStarts = []string{"ftyp", "moov", "mdat", "free", "skip", "wide"}

// box is one box of an MP4: its type, where its body starts and where it ends.
type box struct {
	typ       string
	body, end int64
}

// boxAt reads the head of the box at off, which must end by limit.
func boxAt(r *io.SectionReader, off, limit int64) (box, error) {
	var head [16]byte
	if err := readAt(r, head[:8], off); err != nil {
		return box{}, err
	}
	b := box{typ: string(head[4:8]), body: off + 8}
	switch size := int64(binary.BigEndian.Uint32(head[:])); size {
	case 0:
		b.end = limit
	case 1:
		if err := readAt(r, head[8:], off+8); err != nil {
			return box{}, err
		}
		b.body += 8
		b.end = off + int64(binary.BigEndian.Uint64(head[8:]))
	default:
		b.end = off + size
	}
	if b.end < b.body || b.end > limit {
		return box{}, fmt.Errorf("%w: a %q box overruns its parent", ErrNoIndex, b.typ)
	}
	return b, nil
}

// boxes calls f with each box from off to end, until f answers false.
func boxes(r *io.SectionReader, off, end int64, f func(box) (bool, error)) error {
	for n := 0; off < end; n++ {
		if n == maxHeads {
			return fmt.Errorf("%w: more than %d boxes", ErrNoIndex, maxHeads)
		}
		b, err := boxAt(r, off, end)
		if err != nil {
			return err
		}
		if more, err := f(b); err != nil || !more {
			return err
		}
		off = b.end
	}
	return nil
}

// within finds the box at a path below parent, or answers false.
func within(r *io.SectionReader, parent box, path ...string) (box, bool, error) {
	var found box
	ok := false
	err := boxes(r, parent.body, parent.end, func(b box) (bool, error) {
		if b.typ != path[0] {
			return true, nil
		}
		found, ok = b, true
		return false, nil
	})
	if err != nil || !ok || len(path) == 1 {
		return found, ok, err
	}
	return within(r, found, path[1:]...)
}

// fullBox reads a full box's body whole: its version, and what follows its flags.
func fullBox(r *io.SectionReader, b box) (byte, []byte, error) {
	body, err := readWhole(r, b.body, b.end-b.body)
	if err != nil {
		return 0, nil, err
	}
	if len(body) < 4 {
		return 0, nil, fmt.Errorf("%w: a %q box too short for its fields", ErrNoIndex, b.typ)
	}
	return body[0], body[4:], nil
}

// table checks that a full box's body holds the count of entries of width bytes it begins with,
// and answers them.
func table(body []byte, width int) ([]byte, int, error) {
	if len(body) < 4 {
		return nil, 0, fmt.Errorf("%w: a table with no count", ErrNoIndex)
	}
	n := binary.BigEndian.Uint32(body)
	if uint64(n)*uint64(width) > uint64(len(body)-4) {
		return nil, 0, fmt.Errorf("%w: a table of %d entries in %d bytes", ErrNoIndex, n, len(body))
	}
	return body[4:], int(n), nil
}

// track is an MP4 video track: its id, timescale, and what its edit list adds to every sample's
// composition time to give the time it is shown.
type track struct {
	id    uint32
	scale uint32
	shift int64
}

// mp4Keyframes reads an MP4 or QuickTime file's first video track's sync samples: from its sample
// table (stss, timed by stts and ctts), and for a fragmented file from the random access index
// (mfra) at its end. The moov is found whether it is at the front or the end; nothing past the
// first fragment is walked.
func mp4Keyframes(r *io.SectionReader) ([]int64, error) {
	size := r.Size()
	var moov box
	found := false
	err := boxes(r, 0, size, func(b box) (bool, error) {
		switch b.typ {
		case "moov":
			moov, found = b, true
			return false, nil
		case "moof":
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: no moov before the first fragment", ErrNoIndex)
	}
	trak, t, err := videoTrack(r, moov)
	if err != nil {
		return nil, err
	}
	pts, err := sampleTable(r, trak, t)
	if err != nil {
		return nil, err
	}
	if _, fragmented, err := within(r, moov, "mvex"); err != nil || !fragmented {
		return pts, err
	}
	more, err := randomAccess(r, t)
	return append(pts, more...), err
}

// videoTrack finds the first video trak, its timescale and its edit list's shift.
func videoTrack(r *io.SectionReader, moov box) (box, track, error) {
	mvhd, ok, err := within(r, moov, "mvhd")
	if err != nil || !ok {
		return box{}, track{}, cmpErr(err, "no mvhd")
	}
	v, body, err := fullBox(r, mvhd)
	if err != nil {
		return box{}, track{}, err
	}
	movieScale, err := field32(body, 8, 16, v)
	if err != nil {
		return box{}, track{}, err
	}
	var trak box
	var t track
	err = boxes(r, moov.body, moov.end, func(b box) (bool, error) {
		if b.typ != "trak" {
			return true, nil
		}
		hdlr, ok, err := within(r, b, "mdia", "hdlr")
		if err != nil || !ok {
			return false, cmpErr(err, "a trak with no hdlr")
		}
		_, h, err := fullBox(r, hdlr)
		if err != nil || len(h) < 8 || string(h[4:8]) != "vide" {
			return true, err
		}
		trak = b
		return false, nil
	})
	if err != nil {
		return box{}, track{}, err
	}
	if trak.end == 0 {
		return box{}, track{}, fmt.Errorf("%w: no video track", ErrNoIndex)
	}
	if t, err = describe(r, trak, movieScale); err != nil {
		return box{}, track{}, err
	}
	return trak, t, nil
}

// describe reads a trak's id, timescale and edit list. Empty edits before the first that plays
// delay it; that one's media time is where it starts. Later edits are not followed.
func describe(r *io.SectionReader, trak box, movieScale uint32) (track, error) {
	var t track
	tkhd, ok, err := within(r, trak, "tkhd")
	if err != nil || !ok {
		return t, cmpErr(err, "no tkhd")
	}
	v, body, err := fullBox(r, tkhd)
	if err != nil {
		return t, err
	}
	if t.id, err = field32(body, 8, 16, v); err != nil {
		return t, err
	}
	mdhd, ok, err := within(r, trak, "mdia", "mdhd")
	if err != nil || !ok {
		return t, cmpErr(err, "no mdhd")
	}
	if v, body, err = fullBox(r, mdhd); err != nil {
		return t, err
	}
	if t.scale, err = field32(body, 8, 16, v); err != nil {
		return t, err
	}
	if t.scale == 0 || movieScale == 0 {
		return t, fmt.Errorf("%w: a timescale of zero", ErrNoIndex)
	}
	elst, ok, err := within(r, trak, "edts", "elst")
	if err != nil || !ok {
		return t, err
	}
	if v, body, err = fullBox(r, elst); err != nil {
		return t, err
	}
	width := 12
	if v == 1 {
		width = 20
	}
	entries, n, err := table(body, width)
	if err != nil {
		return t, err
	}
	var empty uint64
	for e := range n {
		entry := entries[e*width:]
		var duration uint64
		var media int64
		if v == 1 {
			duration, media = binary.BigEndian.Uint64(entry), int64(binary.BigEndian.Uint64(entry[8:]))
		} else {
			duration, media = uint64(binary.BigEndian.Uint32(entry)), int64(int32(binary.BigEndian.Uint32(entry[4:])))
		}
		if media != -1 {
			t.shift = int64(empty*uint64(t.scale)/uint64(movieScale)) - media
			break
		}
		empty += duration
	}
	return t, nil
}

// sampleTable reads the presentation times of a trak's sync samples: each one's decode time from
// stts plus its composition offset from ctts. With no stss every sample is a sync sample.
func sampleTable(r *io.SectionReader, trak box, t track) ([]int64, error) {
	stbl, ok, err := within(r, trak, "mdia", "minf", "stbl")
	if err != nil || !ok {
		return nil, cmpErr(err, "no stbl")
	}
	read := func(typ string, width int) ([]byte, int, bool, error) {
		b, ok, err := within(r, stbl, typ)
		if err != nil || !ok {
			return nil, 0, ok, err
		}
		_, body, err := fullBox(r, b)
		if err != nil {
			return nil, 0, true, err
		}
		entries, n, err := table(body, width)
		return entries, n, true, err
	}
	stts, runs, _, err := read("stts", 8)
	if err != nil {
		return nil, err
	}
	ctts, offsets, _, err := read("ctts", 8)
	if err != nil {
		return nil, err
	}
	stss, syncs, listed, err := read("stss", 4)
	if err != nil {
		return nil, err
	}
	var samples uint64
	for e := range runs {
		samples += uint64(binary.BigEndian.Uint32(stts[e*8:]))
	}
	if !listed {
		// A sample is at least a byte.
		syncs = int(min(samples, uint64(r.Size())))
	}
	decode, composition := runner{table: stts, n: runs}, runner{table: ctts, n: offsets}
	var pts []int64
	for s := range syncs {
		sample := uint64(s)
		if listed {
			sample = uint64(binary.BigEndian.Uint32(stss[s*4:])) - 1
		}
		if sample >= samples {
			return nil, fmt.Errorf("%w: a sync sample past the last sample", ErrNoIndex)
		}
		dts, ok := decode.time(sample)
		if !ok {
			return nil, fmt.Errorf("%w: sync samples out of order", ErrNoIndex)
		}
		var cts int64
		if offsets > 0 {
			_, ok := composition.time(sample)
			if !ok {
				return nil, fmt.Errorf("%w: a ctts shorter than the track", ErrNoIndex)
			}
			cts = int64(int32(composition.value))
		}
		pts = append(pts, millis(dts+cts+t.shift, t.scale))
	}
	return pts, nil
}

// runner walks a table of runs, a count and a value each, forwards: the sum of the values of every
// sample before one, and that sample's own value.
type runner struct {
	table []byte
	n     int
	at    int
	first uint64 // the first sample of the run at
	sum   int64  // the sum of the values before first
	value uint32
}

func (u *runner) time(sample uint64) (int64, bool) {
	for u.at < u.n {
		count := uint64(binary.BigEndian.Uint32(u.table[u.at*8:]))
		u.value = binary.BigEndian.Uint32(u.table[u.at*8+4:])
		if sample < u.first {
			return 0, false
		}
		if sample < u.first+count {
			return u.sum + int64(sample-u.first)*int64(u.value), true
		}
		u.sum += int64(count) * int64(u.value)
		u.first += count
		u.at++
	}
	return 0, false
}

// randomAccess reads a fragmented file's tfra for the track, found through the mfro that ends the
// file. Each entry is a sync sample at its presentation time.
func randomAccess(r *io.SectionReader, t track) ([]int64, error) {
	size := r.Size()
	if size < 16 {
		return nil, fmt.Errorf("%w: no mfro", ErrNoIndex)
	}
	mfro, err := boxAt(r, size-16, size)
	if err != nil {
		return nil, err
	}
	if mfro.typ != "mfro" {
		return nil, fmt.Errorf("%w: a fragmented file with no mfra", ErrNoIndex)
	}
	_, body, err := fullBox(r, mfro)
	if err != nil || len(body) < 4 {
		return nil, cmpErr(err, "a short mfro")
	}
	mfra, err := boxAt(r, size-int64(binary.BigEndian.Uint32(body)), size)
	if err != nil {
		return nil, err
	}
	if mfra.typ != "mfra" {
		return nil, fmt.Errorf("%w: an mfro pointing at no mfra", ErrNoIndex)
	}
	var pts []int64
	err = boxes(r, mfra.body, mfra.end, func(b box) (bool, error) {
		if b.typ != "tfra" {
			return true, nil
		}
		v, body, err := fullBox(r, b)
		if err != nil || len(body) < 8 || binary.BigEndian.Uint32(body) != t.id {
			return true, err
		}
		lengths := binary.BigEndian.Uint32(body[4:])
		width := 8 + int(lengths>>4&3+lengths>>2&3+lengths&3) + 3
		if v == 1 {
			width += 8
		}
		entries, n, err := table(body[8:], width)
		if err != nil {
			return false, err
		}
		for e := range n {
			entry := entries[e*width:]
			at := int64(binary.BigEndian.Uint32(entry))
			if v == 1 {
				at = int64(binary.BigEndian.Uint64(entry))
			}
			pts = append(pts, millis(at+t.shift, t.scale))
		}
		return false, nil
	})
	return pts, err
}

// field32 reads a full box's 32-bit field at v0 in version 0, at v1 in version 1.
func field32(body []byte, v0, v1 int, version byte) (uint32, error) {
	at := v0
	if version == 1 {
		at = v1
	}
	if at+4 > len(body) {
		return 0, fmt.Errorf("%w: a box too short for its fields", ErrNoIndex)
	}
	return binary.BigEndian.Uint32(body[at:]), nil
}

// cmpErr answers err, or where there is none, ErrNoIndex saying what was missing.
func cmpErr(err error, missing string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrNoIndex, missing)
}
