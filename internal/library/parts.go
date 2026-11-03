package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Places finds where a part's bytes are.
type Places interface {
	PartPlace(ctx context.Context, part uuid.UUID) (domain.Place, error)
}

// Streams opens a version a provider streams, as its offers say where it is now.
type Streams interface {
	Open(ctx context.Context, p domain.Place) (media.Input, error)
}

// Parts opens the parts of the versions the catalogue holds, for a tool to read or a player to
// fetch: a file under a folder library's root, or a version a remote library's provider streams.
type Parts struct {
	Places  Places
	Streams Streams
}

func (p Parts) Open(ctx context.Context, part uuid.UUID) (media.Input, error) {
	place, err := p.Places.PartPlace(ctx, part)
	if err != nil {
		return media.Input{}, err
	}
	var in media.Input
	switch place.Media {
	case domain.MediaRemote:
		in, err = p.Streams.Open(ctx, place)
	case domain.MediaFolder:
		in, err = OpenMedia(place.Root, place.Rel)
	}
	if err != nil {
		return media.Input{}, fmt.Errorf("part %s: %w", part, err)
	}
	return in, nil
}

// ErrNoLyrics is a track no lyrics are found for.
var ErrNoLyrics = errors.New("no lyrics")

// Lyrics are a part's lyrics: an LRC file of its name beside it, as Jellyfin and Navidrome read
// one, else what its own tags hold. A version a provider streams has none.
func (p Parts) Lyrics(ctx context.Context, part uuid.UUID) (media.Lyrics, error) {
	place, err := p.Places.PartPlace(ctx, part)
	if err != nil {
		return media.Lyrics{}, err
	}
	if place.Media != domain.MediaFolder {
		return media.Lyrics{}, ErrNoLyrics
	}
	sidecar := strings.TrimSuffix(place.Rel, path.Ext(place.Rel)) + ".lrc"
	if f, err := Open(place.Root, sidecar); err == nil {
		defer f.Close()
		text, err := io.ReadAll(io.LimitReader(f, maxLyrics))
		if err != nil {
			return media.Lyrics{}, err
		}
		return found(media.ParseLyrics(string(text)))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return media.Lyrics{}, err
	}
	if !filepath.IsLocal(filepath.FromSlash(place.Rel)) {
		return media.Lyrics{}, fmt.Errorf("%q: %w", place.Rel, errNotInside)
	}
	text, err := media.ReadLyrics(filepath.Join(place.Root, filepath.FromSlash(place.Rel)))
	if err != nil {
		return media.Lyrics{}, err
	}
	return found(media.ParseLyrics(text))
}

// maxLyrics is the most of an LRC file read: a long song's timed lines are a few kilobytes.
const maxLyrics = 1 << 20

func found(l media.Lyrics) (media.Lyrics, error) {
	if len(l.Lines) == 0 {
		return media.Lyrics{}, ErrNoLyrics
	}
	return l, nil
}
