package analysis

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// trickplay is Jellyfin's default: a 320-pixel thumbnail every ten seconds, a hundred to a sheet.
var trickplay = media.Grid{Interval: 10 * time.Second, Width: 320, Columns: 10, Rows: 10}

const (
	// chapterWidth is wide enough for a chapter's card on a television.
	chapterWidth = 640
	// openingChapterAt is where a chapter starting at zero is pictured, as Jellyfin does: a title's
	// first frame is often black.
	openingChapterAt = 15 * time.Second
	// making names the folders previews are made in before they are moved into place.
	making = ".making-"
	// madeLife is how long a folder may be left being made, or made and not recorded, before it is
	// taken for one abandoned.
	madeLife = time.Hour
)

// Previews keeps parts' previews: a folder per part holding trickplay/{n}.jpg and
// chapters/{idx}.jpg.
type Previews struct {
	dir  string
	root *os.Root
}

func OpenPreviews(dir string) (*Previews, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Previews{dir: dir, root: root}, nil
}

func (p *Previews) Close() error { return p.root.Close() }

func (p *Previews) Sheet(part uuid.UUID, n int) (*os.File, error) {
	return p.root.Open(path.Join(part.String(), "trickplay", strconv.Itoa(n)+".jpg"))
}

func (p *Previews) ChapterImage(part uuid.UUID, idx int) (*os.File, error) {
	return p.root.Open(path.Join(part.String(), "chapters", strconv.Itoa(idx)+".jpg"))
}

// Sweep removes the folders of parts that have no previews recorded, and folders abandoned while
// being made. It answers how many it removed.
func (p *Previews) Sweep(ctx context.Context, live func(ctx context.Context, parts []uuid.UUID) (map[uuid.UUID]bool, error)) (int, error) {
	entries, err := fs.ReadDir(p.root.FS(), ".")
	if err != nil {
		return 0, err
	}
	var stale []string
	byID := map[uuid.UUID]string{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < madeLife {
			continue
		}
		if id, err := uuid.Parse(e.Name()); err == nil {
			byID[id] = e.Name()
		} else if strings.HasPrefix(e.Name(), making) {
			stale = append(stale, e.Name())
		}
	}
	ids := make([]uuid.UUID, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	alive, err := live(ctx, ids)
	if err != nil {
		return 0, err
	}
	for id, name := range byID {
		if !alive[id] {
			stale = append(stale, name)
		}
	}
	for n, name := range stale {
		if err := p.root.RemoveAll(name); err != nil {
			return n, err
		}
	}
	return len(stale), nil
}

// MakePreviews makes a part's previews as its library asks, replacing any it had, or removes them
// where it asks for none.
func MakePreviews(st *store.Store, tools media.Tools, p *Previews, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		src, err := st.PreviewSource(ctx, part)
		if errors.Is(err, store.ErrNotFound) {
			return p.root.RemoveAll(part.String())
		}
		if err != nil {
			return err
		}
		if src.Level == domain.PreviewsOff {
			if err := st.ForgetPreviews(ctx, part); err != nil {
				return err
			}
			return p.root.RemoveAll(part.String())
		}
		f, err := openPart(ctx, st, part)
		if err != nil {
			return err
		}
		defer f.Close()
		made, err := os.MkdirTemp(p.dir, making)
		if err != nil {
			return err
		}
		defer os.RemoveAll(made)
		toneMap := src.Range != "" && src.Range != domain.RangeSDR
		chapters, err := chapterImages(ctx, tools, f, src.Chapters, toneMap, filepath.Join(made, "chapters"), log)
		if err != nil {
			return err
		}
		var sheets *store.Trickplay
		switch src.Level {
		case domain.PreviewsAll:
			dir := filepath.Join(made, "trickplay")
			if err := os.Mkdir(dir, 0o750); err != nil {
				return err
			}
			th, err := tools.Trickplay(ctx, f, dir, trickplay, toneMap)
			if err != nil {
				return err
			}
			sheets = &store.Trickplay{
				Width: th.Width, Height: th.Height, IntervalMS: int(trickplay.Interval.Milliseconds()),
				Columns: trickplay.Columns, Rows: trickplay.Rows, Thumbnails: th.Count,
			}
		case domain.PreviewsChapters, domain.PreviewsOff:
		}
		// The new folder takes the old one's place before the record says what is in it.
		if err := p.root.RemoveAll(part.String()); err != nil {
			return err
		}
		if err := p.root.Rename(filepath.Base(made), part.String()); err != nil {
			return err
		}
		return st.SavePreviews(ctx, part, chapters, sheets)
	}
}

// chapterImages pictures each chapter into dir, answering the idx of those pictured. A chapter
// whose picture cannot be made, one starting past the end of the video say, is left without.
func chapterImages(ctx context.Context, tools media.Tools, f *os.File, chapters []store.ChapterSpan, toneMap bool, dir string, log *slog.Logger) ([]int, error) {
	if err := os.Mkdir(dir, 0o750); err != nil {
		return nil, err
	}
	var made []int
	for _, c := range chapters {
		at := c.Start
		if at == 0 {
			at = min(openingChapterAt, c.End/2)
		}
		err := tools.Still(ctx, f, at, chapterWidth, toneMap, filepath.Join(dir, strconv.Itoa(c.Idx)+".jpg"))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			log.WarnContext(ctx, "chapter not pictured", slog.Int("chapter", c.Idx), slog.Any("err", err))
			continue
		}
		made = append(made, c.Idx)
	}
	return made, nil
}

// openPart opens a place a part's bytes are, through its library's root.
func openPart(ctx context.Context, st *store.Store, part uuid.UUID) (*os.File, error) {
	root, rel, err := st.PartFile(ctx, part)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("part %s: %w", part, err)
	}
	return f, nil
}
