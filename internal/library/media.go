package library

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// shortcutRead is as much of a .strm as is read for the address it names.
const shortcutRead = 64 << 10

// OpenMedia opens a file of the library at root, as Open does, for a tool to read.
func OpenMedia(root, rel string) (media.Input, error) {
	f, err := Open(root, rel)
	if err != nil {
		return media.Input{}, err
	}
	in, err := Media(rel, f)
	if err != nil {
		f.Close()
	}
	return in, err
}

// Media is what a tool reads of f, a file of a library opened from rel: the file, or the address
// a .strm names, which closing the input closes f all the same.
func Media(rel string, f *os.File) (media.Input, error) {
	if !naming.IsShortcut(rel) {
		return media.Input{File: f}, nil
	}
	u, err := shortcutURL(f)
	if err != nil {
		return media.Input{}, fmt.Errorf("%s: %w", rel, err)
	}
	return media.Input{File: f, URL: u}, nil
}

// shortcutURL reads the address a .strm names: its first line neither blank nor a # comment, as
// Jellyfin reads one. Only an http or https address is media: another scheme, or a path, would
// have the server read what the library does not hold, so it is not media, as ffprobe finds none
// in a file it cannot read, and is not read again until the .strm changes.
func shortcutURL(f *os.File) (*url.URL, error) {
	lines := bufio.NewScanner(io.NewSectionReader(f, 0, shortcutRead))
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("%w: a .strm names %q, not an http or https address", media.ErrNotMedia, line)
		}
		return u, nil
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: a .strm names no address", media.ErrNotMedia)
}
