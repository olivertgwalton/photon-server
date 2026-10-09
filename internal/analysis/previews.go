package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/library"
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
	// madeLife is how long a part's previews may be kept and not recorded before they are taken
	// for ones abandoned while being made.
	madeLife = time.Hour
)

// stillLimit is as long as one try at a chapter's picture may take, as Jellyfin allows 10 seconds,
// and twice that for a picture tone mapped from HDR. A variable for the test to shorten.
var stillLimit = 10 * time.Second

// objects keeps objects by key, on disk or in a bucket.
type objects interface {
	Open(ctx context.Context, key string) (blob.Object, error)
	Put(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) iter.Seq2[blob.Entry, error]
}

// Previews keeps parts' previews: under each part's id, trickplay/{n}.jpg and chapters/{idx}.jpg.
type Previews struct {
	blobs objects
}

func NewPreviews(blobs objects) *Previews { return &Previews{blobs: blobs} }

func (p *Previews) Sheet(ctx context.Context, part uuid.UUID, n int) (blob.Object, error) {
	return p.blobs.Open(ctx, path.Join(part.String(), "trickplay", strconv.Itoa(n)+".jpg"))
}

func (p *Previews) ChapterImage(ctx context.Context, part uuid.UUID, idx int) (blob.Object, error) {
	return p.blobs.Open(ctx, path.Join(part.String(), "chapters", strconv.Itoa(idx)+".jpg"))
}

// Sweep removes the previews of parts that have none recorded, but for those kept within madeLife,
// which may be being made. It answers how many parts' it removed.
func (p *Previews) Sweep(ctx context.Context, live func(ctx context.Context, parts []uuid.UUID) (map[uuid.UUID]bool, error)) (int, error) {
	newest := map[uuid.UUID]time.Time{}
	for e, err := range p.blobs.List(ctx, "") {
		if err != nil {
			return 0, err
		}
		folder, _, _ := strings.Cut(e.Key, "/")
		if id, err := uuid.Parse(folder); err == nil && e.ModTime.After(newest[id]) {
			newest[id] = e.ModTime
		}
	}
	maps.DeleteFunc(newest, func(_ uuid.UUID, at time.Time) bool { return time.Since(at) < madeLife })
	alive, err := live(ctx, slices.Collect(maps.Keys(newest)))
	if err != nil {
		return 0, err
	}
	removed := 0
	for id := range newest {
		if alive[id] {
			continue
		}
		if err := p.remove(ctx, id, nil); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// remove removes a part's previews but those keep names.
func (p *Previews) remove(ctx context.Context, part uuid.UUID, keep map[string]bool) error {
	var gone []string
	for e, err := range p.blobs.List(ctx, part.String()+"/") {
		if err != nil {
			return err
		}
		if !keep[e.Key] {
			gone = append(gone, e.Key)
		}
	}
	for _, key := range gone {
		if err := p.blobs.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// keep puts what is in the folder made as the part's previews, answering their keys.
func (p *Previews) keep(ctx context.Context, part uuid.UUID, made string) (map[string]bool, error) {
	root, err := os.OpenRoot(made)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	kept := map[string]bool{}
	err = fs.WalkDir(root.FS(), ".", func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		key := path.Join(part.String(), name)
		kept[key] = true
		return p.blobs.Put(ctx, key, f)
	})
	return kept, err
}

// MakePreviews makes a part's previews as its library asks, replacing any it had, or removes them
// where it asks for none.
func MakePreviews(st *store.Store, tools media.Tools, p *Previews, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		src, err := st.PreviewSource(ctx, part)
		if errors.Is(err, store.ErrNotFound) {
			return p.remove(ctx, part, nil)
		}
		if err != nil {
			return err
		}
		if src.Level == domain.PreviewsOff {
			if err := st.ForgetPreviews(ctx, part); err != nil {
				return err
			}
			return p.remove(ctx, part, nil)
		}
		in, err := openPart(ctx, st, part)
		if err != nil {
			return err
		}
		defer in.Close()
		made, err := os.MkdirTemp("", "previews-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(made)
		chapters, err := chapterImages(ctx, tools, in, src.Chapters, src.Length, src.Range, filepath.Join(made, "chapters"), log)
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
			th, err := tools.Trickplay(ctx, in, dir, trickplay, src.Range)
			if err != nil {
				return err
			}
			sheets = &store.Trickplay{
				Width: th.Width, Height: th.Height, IntervalMS: int(trickplay.Interval.Milliseconds()),
				Columns: trickplay.Columns, Rows: trickplay.Rows, Thumbnails: th.Count,
			}
		case domain.PreviewsChapters, domain.PreviewsOff:
		}
		// The new previews are kept before the record says what there is, and the old ones none of
		// them replaced removed only once it does.
		kept, err := p.keep(ctx, part, made)
		if err != nil {
			return err
		}
		if err := st.SavePreviews(ctx, part, chapters, sheets); err != nil {
			return err
		}
		return p.remove(ctx, part, kept)
	}
}

// chapterImages pictures the chapters of a video length long into dir in order, answering the idx
// of those pictured. As Jellyfin does, it stops at the first chapter starting past the end, and at
// the first whose picture cannot be made, keeping those made before it: on a mount that has
// stopped answering, each chapter tried would cost its limit twice over.
func chapterImages(ctx context.Context, tools media.Tools, in media.Input, chapters []store.ChapterSpan, length time.Duration, source domain.Range, dir string, log *slog.Logger) ([]int, error) {
	if err := os.Mkdir(dir, 0o750); err != nil {
		return nil, err
	}
	var made []int
	for _, c := range chapters {
		if c.Start >= length {
			break
		}
		at := c.Start
		if at == 0 {
			at = min(openingChapterAt, c.End/2)
		}
		err := still(ctx, tools, in, at, source, filepath.Join(dir, strconv.Itoa(c.Idx)+".jpg"))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			log.WarnContext(ctx, "chapters not pictured", slog.Int("from", c.Idx), slog.Any("err", err))
			break
		}
		made = append(made, c.Idx)
	}
	return made, nil
}

// still pictures a chapter from keyframes alone or, failing that, from every frame, as Jellyfin's
// keyframe-only extraction does, each try within stillLimit.
func still(ctx context.Context, tools media.Tools, in media.Input, at time.Duration, source domain.Range, path string) error {
	limit := stillLimit
	if source.HDR() {
		limit *= 2
	}
	var err error
	for _, decode := range []media.Decode{media.DecodeKeyframes, media.DecodeEvery} {
		try, cancel := context.WithTimeout(ctx, limit)
		err = tools.Still(try, in, decode, at, chapterWidth, source, path)
		cancel()
		if err == nil || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// openPart opens a place a part's bytes are.
func openPart(ctx context.Context, st *store.Store, part uuid.UUID) (media.Input, error) {
	root, rel, err := st.PartFile(ctx, part)
	if err != nil {
		return media.Input{}, err
	}
	in, err := library.OpenMedia(root, rel)
	if err != nil {
		return media.Input{}, fmt.Errorf("part %s: %w", part, err)
	}
	return in, nil
}
