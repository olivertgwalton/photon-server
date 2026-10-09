package scan

import (
	"bytes"
	"context"
	"errors"
	"path"

	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

func plansOf[T any](titles []T, copies func(T) []copyPlan) [][]copyPlan {
	out := make([][]copyPlan, len(titles))
	for i, t := range titles {
		out[i] = copies(t)
	}
	return out
}

// readCopies reads the content key of every copy of each title of a folder that changed since the
// last scan and probes those the catalogue does not hold, answering each title's copies that could
// be read. Copies are read at once, as the run has room. A copy that cannot be read is left out and
// logged. A byte-identical copy in a second place is a known copy: it becomes another place to read
// the same version.
func (s *Scanner) readCopies(ctx context.Context, r reading, titles [][]copyPlan) ([][]store.Copy, error) {
	copies := make([][]store.Copy, len(titles))
	ok := make([][]bool, len(titles))
	g, gctx := errgroup.WithContext(ctx)
	for i, plans := range titles {
		copies[i], ok[i] = make([]store.Copy, len(plans)), make([]bool, len(plans))
		for j, v := range plans {
			c := describe(r.dir, v)
			if key, unchanged := r.unchanged(c.Parts); unchanged {
				c.ContentKey = key
				copies[i][j], ok[i][j] = c, true
				continue
			}
			g.Go(func() error {
				done, err := r.run.read(gctx)
				if err != nil {
					return err
				}
				defer done()
				ok[i][j], err = s.readCopy(gctx, r, &c)
				copies[i][j] = c
				return err
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([][]store.Copy, len(titles))
	for i := range copies {
		for j, c := range copies[i] {
			if ok[i][j] {
				out[i] = append(out[i], c)
			}
		}
	}
	return out, nil
}

// readCopy reads a new or changed copy's content key and, unless the library holds a copy of those
// bytes, probes its parts, the first through the file its key was read from: on a debrid mount the
// file's start is still at hand, where read later it would be fetched again. A .strm's key is of
// the .strm, and its media is probed where it names. It reports whether the copy could be read.
func (s *Scanner) readCopy(ctx context.Context, r reading, c *store.Copy) (bool, error) {
	root := r.run.lib.Root
	first, err := library.Open(root, c.Parts[0].RelPath)
	if err != nil {
		s.unreadable(ctx, r, c.Parts[0].RelPath, err)
		return false, nil
	}
	defer first.Close()
	if c.ContentKey, err = library.ContentKey(first, len(c.Parts)); err != nil {
		s.unreadable(ctx, r, c.Parts[0].RelPath, err)
		return false, nil
	}
	known, err := s.store.KnownCopies(ctx, r.run.lib.ID, [][]byte{c.ContentKey})
	if err != nil || known[string(c.ContentKey)] {
		return err == nil, err
	}
	for i, p := range c.Parts {
		var in media.Input
		if i == 0 {
			in, err = library.Media(p.RelPath, first)
		} else {
			in, err = library.OpenMedia(root, p.RelPath)
		}
		var facts domain.Facts
		if err == nil {
			facts, err = s.prober.Probe(ctx, in)
			if i > 0 {
				in.Close()
			}
			r.run.count(func(rep *Report) { rep.Probed++ })
		}
		switch {
		case errors.Is(err, media.ErrNotMedia):
			s.skip(ctx, r.run, p.RelPath, err)
			return false, nil
		case err != nil:
			s.unreadable(ctx, r, p.RelPath, err)
			return false, nil
		}
		c.Parts[i].Facts = &facts
	}
	return true, nil
}

// describe is a planned copy's parts and subtitles, its bytes not yet read.
func describe(dir string, v copyPlan) store.Copy {
	c := store.Copy{Edition: v.edition, Label: v.label}
	for _, sub := range v.subtitles {
		c.Subtitles = append(c.Subtitles, store.Subtitle{
			RelPath: path.Join(dir, sub.file.Name), Size: sub.file.Size, ModTime: sub.file.ModTime,
			Codec: sub.codec, Language: sub.tags.Language, Title: sub.tags.Title,
			Forced: sub.tags.Forced, Default: sub.tags.Default, HearingImpaired: sub.tags.HearingImpaired,
		})
	}
	for _, p := range v.parts {
		c.Parts = append(c.Parts, store.Part{RelPath: path.Join(dir, p.Name), Size: p.Size, ModTime: p.ModTime})
	}
	return c
}

// unchanged answers the content key of a copy whose every part is where the last scan found it,
// at the size and modification time it had then, as Plex and Silo skip a file: its bytes are not
// read again.
func (r reading) unchanged(parts []store.Part) ([]byte, bool) {
	var key []byte
	for i, p := range parts {
		f, ok := r.known[p.RelPath]
		if !ok || f.Size != p.Size || !f.ModTime.Equal(p.ModTime) || f.Idx != i || f.Parts != len(parts) ||
			(key != nil && !bytes.Equal(f.ContentKey, key)) {
			return nil, false
		}
		key = f.ContentKey
	}
	return key, true
}
