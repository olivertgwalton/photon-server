// Package dovi rewrites Dolby Vision RPUs, as dovi_tool's libdovi does.
package dovi

import (
	"errors"
	"fmt"
)

// ErrNotProfile7 is an RPU that is not a dual-layer profile 7 one.
var ErrNotProfile7 = errors.New("dovi: not a profile 7 RPU")

var errShort = errors.New("dovi: RPU ends early")

// layerDepth is the bit depth of both layers, the only one libdovi takes.
const layerDepth = 10

// The fixed part of an uncompressed DM payload: YCbCr to RGB, then RGB to LMS, coefficients.
const ycc, lms = 9*16 + 3*32, 9 * 16

// Profile81 rewrites a profile 7 RPU NAL unit (UNSPEC62, its two-byte header included) as profile
// 8.1, as libdovi's conversion mode 2: the enhancement layer's residual goes, and with a full
// enhancement layer the mapping becomes an identity, the base layer being shown without it. The
// display metadata is kept, only its colour matrices made 8.1's. Everything from the first byte
// boundary of the DM extension blocks on is copied as it is: its layout depends on no bit before it.
func Profile81(nal []byte) ([]byte, error) {
	if len(nal) < 3 || nal[0] != 0x7c || nal[1] != 0x01 {
		return nil, ErrNotProfile7
	}
	rpu := unescape(nal[2:])
	for len(rpu) > 0 && rpu[len(rpu)-1] == 0 {
		rpu = rpu[:len(rpu)-1]
	}
	if len(rpu) < 7 || rpu[0] != 0x19 || rpu[len(rpu)-1] != 0x80 {
		return nil, ErrNotProfile7
	}
	end := len(rpu) - 5
	if crc32MPEG2(rpu[1:end]) != uint32(rpu[end])<<24|uint32(rpu[end+1])<<16|uint32(rpu[end+2])<<8|uint32(rpu[end+3]) {
		return nil, errors.New("dovi: RPU fails its CRC")
	}
	c := &conversion{rpu: rpu, r: &reader{b: rpu[:end]}, w: &writer{}}
	r, w := c.r, c.w

	// rpu_nal_prefix, rpu_type, rpu_format, vdr_rpu_profile and level, vdr_seq_info_present_flag
	r.skip(8 + 6)
	format := r.bits(11)
	profile := r.bits(4)
	r.skip(4)
	if !r.flag() || format&0x700 != 0 || profile != 1 {
		return nil, ErrNotProfile7
	}
	r.skip(1) // chroma_resampling_explicit_filter_flag
	c.coefType = r.bits(2)
	c.coefLen = 32
	switch c.coefType {
	case 0:
		// libdovi's bounds: past them it refuses an RPU, as this does.
		n := r.ue()
		if n > 23 {
			return nil, fmt.Errorf("dovi: coefficient_log2_denom %d", n)
		}
		c.coefLen = int(n)
	case 1:
	default:
		return nil, fmt.Errorf("dovi: coefficient_data_type %d", c.coefType)
	}
	r.skip(2 + 1) // vdr_rpu_normalized_idc, bl_video_full_range_flag
	blDepth, elDepth := r.ue()+8, r.ue()&0xff+8
	if blDepth != layerDepth || elDepth != layerDepth {
		return nil, fmt.Errorf("dovi: base and enhancement layer depths %d and %d", blDepth, elDepth)
	}
	vdrDepth := r.ue() + 8
	r.skip(1)
	compressed := r.bits(3) == 1
	c.copy(0)
	if !r.flag() || r.flag() || vdrDepth != 12 { // el_spatial_resampling_filter_flag, disable_residual_flag
		return nil, ErrNotProfile7
	}
	w.bits(0, 1)
	w.bits(1, 1)
	at := r.at
	dm := r.flag()
	if r.flag() { // use_prev_vdr_rpu_flag
		r.ue()
		c.copy(at)
	} else if err := c.mapping(at); err != nil {
		return nil, err
	}
	if dm {
		c.dm(compressed)
	}
	r.align()
	w.align()
	if r.err != nil {
		return nil, r.err
	}
	out := append(w.b, rpu[r.at/8:end]...)
	crc := crc32MPEG2(out[1:])
	out = append(out, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc), 0x80)
	return append([]byte{0x7c, 0x01}, escape(out)...), nil
}

// conversion is an RPU being read and its conversion being written.
type conversion struct {
	rpu      []byte
	r        *reader
	w        *writer
	coefType uint64
	coefLen  int
}

// copy writes the bits read since from as they are.
func (c *conversion) copy(from int) { c.w.copy(c.rpu, from, c.r.at) }

// mapping rewrites the mapping from the base layer to the picture shown, from the header's flags
// after the two rewritten, at: its residual's quantisation goes, and with a full enhancement layer,
// the curves.
func (c *conversion) mapping(at int) error {
	r, w := c.r, c.w
	r.ue() // vdr_rpu_id
	r.ue() // mapping_color_space
	r.ue() // mapping_chroma_format_idc
	ids := r.at
	pieces := make([]int, 3)
	for i := range pieces {
		n := int(r.ue()) + 2
		if n > 9 {
			return fmt.Errorf("dovi: %d pivots", n)
		}
		r.skip(n * layerDepth)
		pieces[i] = n - 1
	}
	pivots := r.at
	r.skip(3 + 2*layerDepth) // nlq_method_idc, nlq_pred_pivot_value
	r.ue()                   // num_x_partitions_minus1
	r.ue()                   // num_y_partitions_minus1
	curves := r.at
	for _, n := range pieces {
		for range n {
			r.curve(c.coefType, c.coefLen)
		}
	}
	curvesEnd := r.at
	mel := true
	for range 3 {
		mel = r.bits(layerDepth) == 0 && mel                       // nlq_offset
		mel = r.coef(c.coefType, c.coefLen) == [2]uint64{1} && mel // vdr_in_max
		mel = r.coef(c.coefType, c.coefLen) == [2]uint64{} && mel  // linear_deadzone_slope
		mel = r.coef(c.coefType, c.coefLen) == [2]uint64{} && mel  // linear_deadzone_threshold
	}
	if mel {
		w.copy(c.rpu, at, pivots)
		w.ue(0)
		w.ue(0)
		w.copy(c.rpu, curves, curvesEnd)
		return nil
	}
	w.copy(c.rpu, at, ids)
	for range 3 {
		w.ue(0)
		w.bits(0, layerDepth)
		w.bits(1023, layerDepth)
	}
	w.ue(0)
	w.ue(0)
	for range 3 { // a first-order polynomial, y = x
		w.ue(0) // mapping_idc
		w.ue(0) // poly_order_minus1
		w.bits(0, 1)
		for _, k := range []int64{0, 1} {
			if c.coefType == 0 {
				w.se(k)
			}
			w.bits(0, c.coefLen)
		}
	}
	return nil
}

// dm rewrites the display metadata's colour matrices, up to the extension blocks' alignment.
func (c *conversion) dm(compressed bool) {
	r, w := c.r, c.w
	at := r.at
	r.ue() // affected_dm_metadata_id
	r.ue() // current_dm_metadata_id
	r.ue() // scene_refresh_flag
	c.copy(at)
	if !compressed {
		r.skip(ycc + lms)
		for _, k := range p81Matrices {
			w.bits(uint64(uint32(k.v)), k.n)
		}
		at = r.at
		r.skip(16 + 16 + 16 + 32 + 5) // signal_eotf and its parameters, signal_bit_depth
		c.copy(at)
		r.skip(2) // signal_color_space
		w.bits(0, 2)
		at = r.at
		r.skip(2 + 2 + 12 + 12 + 10) // chroma format, full range, source min and max PQ, diagonal
		c.copy(at)
	}
	at = r.at
	r.ue() // num_ext_blocks
	c.copy(at)
}

// p81Matrices are profile 8.1's YCbCr to RGB and RGB to LMS matrices and offsets, as libdovi's.
var p81Matrices = func() []struct{ v, n int } {
	var m []struct{ v, n int }
	for _, v := range []int{9574, 0, 13802, 9574, -1540, -5348, 9574, 17610, 0} {
		m = append(m, struct{ v, n int }{v, 16})
	}
	for _, v := range []int{16777216, 134217728, 134217728} {
		m = append(m, struct{ v, n int }{v, 32})
	}
	for _, v := range []int{7222, 8771, 390, 2654, 12430, 1300, 0, 422, 15962} {
		m = append(m, struct{ v, n int }{v, 16})
	}
	return m
}()

// curve skips one piece of a mapping curve: a polynomial or a multivariate multiple regression.
func (r *reader) curve(coefType uint64, coefLen int) {
	switch r.ue() {
	case 0:
		order := r.ue()
		if order > 1 {
			r.fail(fmt.Errorf("dovi: polynomial order %d", order+1))
		}
		if order == 0 && r.flag() {
			r.fail(errors.New("dovi: linear interpolation"))
		}
		if r.err != nil {
			return
		}
		for range order + 2 {
			r.coef(coefType, coefLen)
		}
	case 1:
		order := r.bits(2)
		if order > 2 {
			r.fail(fmt.Errorf("dovi: MMR order %d", order+1))
		}
		r.coef(coefType, coefLen)
		for range (order + 1) * 7 {
			r.coef(coefType, coefLen)
		}
	default:
		r.fail(errors.New("dovi: unknown mapping"))
	}
}

// coef reads a coefficient: its integer part where the coefficients are fixed point, then its
// fraction.
func (r *reader) coef(coefType uint64, coefLen int) [2]uint64 {
	var c [2]uint64
	if coefType == 0 {
		c[0] = r.ue()
	}
	c[1] = r.bits(coefLen)
	return c
}

// unescape takes the emulation prevention bytes out of a NAL unit's payload.
func unescape(b []byte) []byte {
	out := make([]byte, 0, len(b))
	zeros := 0
	for _, c := range b {
		if zeros >= 2 && c == 3 {
			zeros = 0
			continue
		}
		out = append(out, c)
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

// escape puts them back.
func escape(b []byte) []byte {
	out := make([]byte, 0, len(b)+len(b)/64)
	zeros := 0
	for _, c := range b {
		if zeros >= 2 && c <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, c)
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

func crc32MPEG2(b []byte) uint32 {
	crc := ^uint32(0)
	for _, c := range b {
		crc ^= uint32(c) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// reader reads bits, most significant first, holding the first error.
type reader struct {
	b   []byte
	at  int
	err error
}

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) skip(n int) {
	if n < 0 || r.at+n > len(r.b)*8 {
		r.fail(errShort)
		n = len(r.b)*8 - r.at
	}
	r.at += n
}

func (r *reader) bits(n int) uint64 {
	if n > 64 || r.at+n > len(r.b)*8 {
		r.fail(errShort)
		r.at = len(r.b) * 8
		return 0
	}
	var v uint64
	for range n {
		v = v<<1 | uint64(r.b[r.at/8]>>(7-r.at%8)&1)
		r.at++
	}
	return v
}

func (r *reader) flag() bool { return r.bits(1) == 1 }

// ue reads an Exp-Golomb code; a signed one reads the same bits.
func (r *reader) ue() uint64 {
	zeros := 0
	for !r.flag() && r.err == nil {
		zeros++
	}
	return 1<<zeros - 1 + r.bits(zeros)
}

func (r *reader) align() {
	for r.at%8 != 0 && r.err == nil {
		if r.flag() {
			r.fail(errors.New("dovi: alignment bit set"))
		}
	}
}

// writer writes bits, most significant first.
type writer struct {
	b []byte
	n int
}

func (w *writer) bits(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		w.b[len(w.b)-1] |= byte(v>>i&1) << (7 - w.n%8)
		w.n++
	}
}

func (w *writer) ue(v uint64) {
	n := 0
	for v+1 >= 1<<(n+1) {
		n++
	}
	w.bits(0, n)
	w.bits(v+1, n+1)
}

func (w *writer) se(v int64) {
	if v > 0 {
		w.ue(uint64(2*v - 1))
	} else {
		w.ue(uint64(-2 * v))
	}
}

// copy writes bits from to to of b.
func (w *writer) copy(b []byte, from, to int) {
	for i := from; i < to; i++ {
		w.bits(uint64(b[i/8]>>(7-i%8)&1), 1)
	}
}

func (w *writer) align() {
	if w.n%8 != 0 {
		w.bits(0, 8-w.n%8)
	}
}
