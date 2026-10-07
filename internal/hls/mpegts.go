package hls

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	tsPacket = 188
	// tsWrap is where MPEG-TS's 33-bit timestamps, in 90 kHz units, start again from nought.
	tsWrap = 1 << 33
	patPID = 0
)

var errNotTS = errors.New("hls: not MPEG-TS")

// tsStream reads the MPEG-TS ffmpeg writes as fragments, each from the first packet of a video
// keyframe to the next's. Each is headed by the latest PAT and PMT, so a segment beginning with
// one is decoded by itself. Those tables' continuity counters are numbered afresh as they are
// written, so the copies are not taken for packets lost.
type tsStream struct {
	r     *bufio.Reader
	pkt   [tsPacket]byte
	pmt   int // the PMT's PID, -1 until the PAT is read
	video int // the video's PID, -1 until the PMT is read
	pat   []byte
	table []byte // the latest PMT
	cc    map[int]byte
	// last is the latest keyframe's timestamp counted on past every wrap.
	last int64
	// ahead is the fragment write came to the start of.
	ahead *fragment
}

// readTS reads MPEG-TS whose first keyframe is shown about from on the file's clock.
func readTS(r io.Reader, from time.Duration) *tsStream {
	return &tsStream{
		r: bufio.NewReaderSize(r, 64<<10), pmt: -1, video: -1, cc: map[int]byte{},
		last: int64(from+clockOffset) * 9 / 100_000,
	}
}

// next reads up to the next fragment, dropping whatever precedes the first keyframe. io.EOF after
// the last.
func (s *tsStream) next() (fragment, error) {
	if f := s.ahead; f != nil {
		s.ahead = nil
		return *f, nil
	}
	for {
		f, ok, err := s.read()
		if err != nil || ok {
			return f, err
		}
	}
}

// write writes fragment f whole to w, reading its packets as they are written.
func (s *tsStream) write(w io.Writer, f fragment) error {
	b := bufio.NewWriterSize(w, 64<<10)
	if _, err := b.Write(f.head); err != nil {
		return err
	}
	for {
		next, ok, err := s.read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if ok {
			s.ahead = &next
			break
		}
		if _, err := b.Write(s.pkt[:]); err != nil {
			return err
		}
	}
	return b.Flush()
}

// read reads one packet into s.pkt, answering the fragment it begins where it is a keyframe's
// first.
func (s *tsStream) read() (fragment, bool, error) {
	if _, err := io.ReadFull(s.r, s.pkt[:]); err != nil {
		return fragment{}, false, err
	}
	p := s.pkt[:]
	if p[0] != 0x47 {
		return fragment{}, false, fmt.Errorf("%w: a packet out of sync", errNotTS)
	}
	pid := int(p[1]&0x1f)<<8 | int(p[2])
	start := p[1]&0x40 != 0
	payload, random := tsPayload(p)
	switch {
	case pid == patPID || pid == s.pmt:
		if start {
			if err := s.readTable(pid, payload); err != nil {
				return fragment{}, false, err
			}
		}
		s.count(p)
	case pid == s.video && start && random:
		shown, err := s.shown(payload)
		if err != nil {
			return fragment{}, false, err
		}
		if s.pat == nil || s.table == nil {
			return fragment{}, false, fmt.Errorf("%w: a keyframe before the PAT and PMT", errNotTS)
		}
		head := make([]byte, 0, 3*tsPacket)
		for _, t := range [][]byte{s.pat, s.table} {
			head = append(head, t...)
			s.count(head[len(head)-tsPacket:])
		}
		return fragment{head: append(head, p...), shown: shown}, true, nil
	}
	return fragment{}, false, nil
}

// count numbers a PAT or PMT packet as the next of its PID.
func (s *tsStream) count(p []byte) {
	pid := int(p[1]&0x1f)<<8 | int(p[2])
	p[3] = p[3]&0xf0 | s.cc[pid]&0x0f
	s.cc[pid]++
}

// tsPayload answers a packet's payload and whether its adaptation field marks a random access
// point.
func tsPayload(p []byte) ([]byte, bool) {
	control := (p[3] >> 4) & 3
	at, random := 4, false
	if control&2 != 0 {
		n := int(p[4])
		if n > 0 && 5+n <= tsPacket {
			random = p[5]&0x40 != 0
		}
		at = 5 + n
	}
	if control&1 == 0 || at >= tsPacket {
		return nil, random
	}
	return p[at:], random
}

// readTable reads the PAT, learning the PMT's PID, or the PMT, learning the video's, keeping a
// copy of each. ffmpeg writes either in one packet.
func (s *tsStream) readTable(pid int, payload []byte) error {
	if len(payload) == 0 || 1+int(payload[0])+3 > len(payload) {
		return fmt.Errorf("%w: a table too short", errNotTS)
	}
	sec := payload[1+int(payload[0]):]
	end := 3 + (int(sec[1]&0x0f)<<8 | int(sec[2]))
	if end > len(sec) || end < 12+4 {
		return fmt.Errorf("%w: a table across packets", errNotTS)
	}
	body := sec[:end-4] // less its CRC
	keep := append([]byte(nil), s.pkt[:]...)
	if pid == patPID {
		for at := 8; at+4 <= len(body); at += 4 {
			if program := int(body[at])<<8 | int(body[at+1]); program != 0 {
				s.pmt, s.pat = int(body[at+2]&0x1f)<<8|int(body[at+3]), keep
				return nil
			}
		}
		return fmt.Errorf("%w: a PAT with no program", errNotTS)
	}
	at := 12 + (int(body[10]&0x0f)<<8 | int(body[11]))
	for ; at+5 <= len(body); at += 5 + (int(body[at+3]&0x0f)<<8 | int(body[at+4])) {
		// H.264 and HEVC, all MPEG-TS segments carry.
		if typ := body[at]; typ == 0x1b || typ == 0x24 {
			s.video, s.table = int(body[at+1]&0x1f)<<8|int(body[at+2]), keep
			return nil
		}
	}
	return fmt.Errorf("%w: a PMT with no video", errNotTS)
}

// shown reads when a keyframe is shown from its PES header's timestamp, less the clock offset.
// The timestamp is taken as the one nearest the last keyframe's, of all it might be past a wrap.
func (s *tsStream) shown(pes []byte) (time.Duration, error) {
	if len(pes) < 14 || pes[0] != 0 || pes[1] != 0 || pes[2] != 1 || pes[7]&0x80 == 0 {
		return 0, fmt.Errorf("%w: a keyframe with no timestamp", errNotTS)
	}
	t := pes[9:14]
	pts := int64(t[0]>>1&7)<<30 | int64(t[1])<<22 | int64(t[2]>>1)<<15 | int64(t[3])<<7 | int64(t[4]>>1)
	pts += s.last - s.last%tsWrap
	switch {
	case pts-s.last > tsWrap/2:
		pts -= tsWrap
	case s.last-pts > tsWrap/2:
		pts += tsWrap
	}
	s.last = pts
	return time.Duration(pts*100_000/9) - clockOffset, nil
}
