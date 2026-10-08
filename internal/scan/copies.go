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
// last scan, asks the catalogue once which it holds, and probes only those it does not, answering
// each title's copies that could be read. Files are read at once, as the run has room. A copy that
// cannot be read is left out and logged. A byte-identical copy in a second place is a known copy:
// it becomes another place to read the same version.
func (s *Scanner) readCopies(ctx context.Context, r reading, titles [][]copyPlan) ([][]store.Copy, error) {
	// fresh is a copy whose key was read now, so the catalogue is asked whether it holds it.
	type read struct {
		c         store.Copy
		ok, fresh bool
	}
	reads := make([][]read, len(titles))
	g, gctx := errgroup.WithContext(ctx)
	for i, plans := range titles {
		reads[i] = make([]read, len(plans))
		for j, v := range plans {
			c := describe(r.dir, v)
			if key, ok := r.unchanged(c.Parts); ok {
				c.ContentKey = key
				reads[i][j] = read{c: c, ok: true}
				continue
			}
			g.Go(func() error {
				done, err := r.run.read(gctx)
				if err != nil {
					return err
				}
				defer done()
				key, err := library.ContentKey(r.run.lib.Root, partPaths(c))
				if err != nil {
					s.unreadable(gctx, r, c.Parts[0].RelPath, err)
					return nil
				}
				c.ContentKey = key
				reads[i][j] = read{c: c, ok: true, fresh: true}
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var keys [][]byte
	for i := range reads {
		for j := range reads[i] {
			if reads[i][j].fresh {
				keys = append(keys, reads[i][j].c.ContentKey)
			}
		}
	}
	known := map[string]bool{}
	if len(keys) > 0 {
		var err error
		if known, err = s.store.KnownCopies(ctx, r.run.lib.ID, keys); err != nil {
			return nil, err
		}
	}
	g, gctx = errgroup.WithContext(ctx)
	for i := range reads {
		for j := range reads[i] {
			rd := &reads[i][j]
			if !rd.fresh || known[string(rd.c.ContentKey)] {
				continue
			}
			g.Go(func() error {
				ok, err := s.probeParts(gctx, r, rd.c)
				rd.ok = ok
				return err
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([][]store.Copy, len(titles))
	for i := range reads {
		for _, rd := range reads[i] {
			if rd.ok {
				out[i] = append(out[i], rd.c)
			}
		}
	}
	return out, nil
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

func partPaths(c store.Copy) []string {
	paths := make([]string, len(c.Parts))
	for i, p := range c.Parts {
		paths[i] = p.RelPath
	}
	return paths
}

// probeParts probes each part of a new copy, as the run has room, reporting whether every one
// could be.
func (s *Scanner) probeParts(ctx context.Context, r reading, c store.Copy) (bool, error) {
	for i, p := range c.Parts {
		done, err := r.run.read(ctx)
		if err != nil {
			return false, err
		}
		facts, err := s.probe(ctx, r.run.lib.Root, p.RelPath)
		done()
		r.run.count(func(rep *Report) { rep.Probed++ })
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

func (s *Scanner) probe(ctx context.Context, root, rel string) (domain.Facts, error) {
	f, err := library.Open(root, rel)
	if err != nil {
		return domain.Facts{}, err
	}
	defer f.Close()
	return s.prober.Probe(ctx, f)
}
