package testtool

import "encoding/binary"

// PGS is a PGS subtitle stream, which FFmpeg cannot encode, as a .sup file has it: a display set
// at each time, in 90 kHz ticks, showing a white box 200x40 low on a 640x360 screen.
func PGS(times ...uint32) []byte {
	var b []byte
	segment := func(at uint32, kind byte, payload []byte) {
		b = append(b, "PG"...)
		b = binary.BigEndian.AppendUint32(b, at)
		b = binary.BigEndian.AppendUint32(b, at)
		b = append(b, kind)
		b = binary.BigEndian.AppendUint16(b, uint16(len(payload)))
		b = append(b, payload...)
	}
	const x, y, w, h = 220, 280, 200, 40
	be := binary.BigEndian.AppendUint16
	// Each line of the box is a run of w pixels of colour 1, then the line's end.
	var rle []byte
	for range h {
		rle = append(rle, 0x00, 0xc0|w>>8, w&0xff, 0x01, 0x00, 0x00)
	}
	object := append(be(be(nil, w), h), rle...)
	for n, at := range times {
		composition := be(be(nil, 640), 360)
		composition = append(be(append(composition, 0x10), uint16(n)), 0x80, 0x00, 0x00, 0x01)
		composition = be(be(append(be(composition, 0), 0x00, 0x00), x), y)
		segment(at, 0x16, composition)
		segment(at, 0x17, be(be(be(be([]byte{0x01, 0x00}, x), y), w), h))
		segment(at, 0x14, []byte{0x00, 0x00, 0x01, 235, 128, 128, 255})
		length := len(object)
		segment(at, 0x15, append([]byte{0x00, 0x00, 0x00, 0xc0, byte(length >> 16), byte(length >> 8), byte(length)}, object...))
		segment(at, 0x80, nil)
	}
	return b
}
