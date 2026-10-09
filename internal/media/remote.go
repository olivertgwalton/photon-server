package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// rangeBlock is how much of a .strm's media one request fetches for a short read: an index is
// read as many small headers close together, each of which would otherwise be a request.
const rangeBlock = 64 << 10

// keptBlocks is how many fetched blocks are kept for the reads after them.
const keptBlocks = 16

// remoteMedia reads a .strm's media at u by byte ranges: a block at a time for short reads, a
// longer read as it is asked.
type remoteMedia struct {
	ctx    context.Context
	u      *url.URL
	blocks map[int64][]byte
}

// openRemote reads the media at u by byte ranges, answering it with its size: the start of it is
// fetched to learn that.
func openRemote(ctx context.Context, u *url.URL) (*io.SectionReader, error) {
	m := &remoteMedia{ctx: ctx, u: u, blocks: map[int64][]byte{}}
	b, size, err := m.fetch(0, rangeBlock)
	if err != nil {
		return nil, err
	}
	m.blocks[0] = b
	return io.NewSectionReader(m, 0, size), nil
}

func (m *remoteMedia) ReadAt(p []byte, off int64) (int, error) {
	if len(p) >= rangeBlock {
		b, _, err := m.fetch(off, int64(len(p)))
		n := copy(p, b)
		if err == nil && n < len(p) {
			err = io.EOF
		}
		return n, err
	}
	n := 0
	for n < len(p) {
		at := (off + int64(n)) / rangeBlock * rangeBlock
		b, ok := m.blocks[at]
		if !ok {
			var err error
			if b, _, err = m.fetch(at, rangeBlock); err != nil {
				return n, err
			}
			if len(m.blocks) >= keptBlocks {
				clear(m.blocks)
			}
			m.blocks[at] = b
		}
		from := off + int64(n) - at
		if from >= int64(len(b)) {
			return n, io.EOF
		}
		n += copy(p[n:], b[from:])
	}
	return n, nil
}

// fetch asks for length bytes from off, answering those the server sends and the media's size.
func (m *remoteMedia) fetch(off, length int64) ([]byte, int64, error) {
	header := http.Header{"Range": {"bytes=" + strconv.FormatInt(off, 10) + "-" + strconv.FormatInt(off+length-1, 10)}}
	resp, err := Fetch(m.ctx, http.MethodGet, m.u, header)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		return nil, 0, fmt.Errorf("%w: its server does not answer byte ranges", ErrNoIndex)
	default:
		return nil, 0, fmt.Errorf("fetching %s: %s", m.u.Redacted(), resp.Status)
	}
	// "bytes 0-65535/1234567"; a size the server does not know is "*".
	_, total, _ := strings.Cut(resp.Header.Get("Content-Range"), "/")
	size, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: its server does not answer byte ranges", ErrNoIndex)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, length))
	return b, size, err
}
