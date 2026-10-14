package testtool

import "encoding/binary"

// PGS is a PGS subtitle stream, which FFmpeg cannot encode, as a .sup file has it: a display set
// shown at each time, in 90 kHz ticks, of a 320x240 screen showing nothing.
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
	for _, at := range times {
		segment(at, 0x16, []byte{0x01, 0x40, 0x00, 0xf0, 0x10, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00})
		segment(at, 0x80, nil)
	}
	return b
}
