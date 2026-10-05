//go:build integration

package analysis

import (
	"context"
	"crypto/sha256"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// fakeFFmpeg writes what FFmpeg 9 made of a generated 1050-second video (ffmpeg -f lavfi -i
// color=c=navy:size=64x36:rate=24 -t 1050 -g 48), whatever it is asked: for trickplay, its listing
// of 105 thumbnails at 320x180 and two sheets; for a chapter, a picture at the path it is given.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/bin/sh
for a; do last=$a; done
case "$*" in
*framecrc*)
	while [ $# -gt 0 ]; do [ "$1" = -start_number ] && sheets=$(dirname "$3"); shift; done
	cp '` + testdata + `/sheet.jpg' "$sheets/0.jpg"
	cp '` + testdata + `/sheet.jpg' "$sheets/1.jpg"
	cat '` + testdata + `/thumbnails.framecrc' ;;
*) cp '` + testdata + `/still.jpg' "$last" ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type fixture struct {
	t        *testing.T
	st       *store.Store
	db       *pgx.Conn
	root     string
	lib      domain.Library
	admin    domain.Profile
	previews *Previews
	make     jobs.Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	db, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	root := t.TempDir()
	lib, err := st.AddLibrary(t.Context(), "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := st.AddProfile(t.Context(), "Admin", domain.RoleAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	previews, err := OpenPreviews(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = previews.Close() })
	tools := media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}
	return &fixture{
		t: t, st: st, db: db, root: root, lib: lib, admin: admin, previews: previews,
		make: MakePreviews(st, tools, previews, log),
	}
}

// film saves a film whose file holds seed, with two chapters, as a scan would, and answers its
// title and its part.
func (f *fixture) film(seed string) (title, part uuid.UUID) {
	f.t.Helper()
	rel := "Heat (1995).mkv"
	if err := os.WriteFile(filepath.Join(f.root, rel), []byte(seed), 0o644); err != nil {
		f.t.Fatal(err)
	}
	key := sha256.Sum256([]byte(seed))
	facts := &media.Facts{
		Container: "matroska,webm", Duration: 1050 * time.Second,
		Streams: []media.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 640, Height: 360, Range: domain.RangeSDR}},
		Chapters: []media.Chapter{
			{Start: 0, End: 10 * time.Minute, Title: "Opening"},
			{Start: 10 * time.Minute, End: 1050 * time.Second, Title: "Heist"},
		},
	}
	film := store.Film{Title: "Heat", Year: 1995, Copies: []store.Copy{{
		ContentKey: key[:], Parts: []store.Part{{RelPath: rel, Size: int64(len(seed)), ModTime: time.Now(), Facts: facts}},
	}}}
	if _, err := f.st.SaveFolder(f.t.Context(), f.lib.ID, "", key[:], []store.Film{film}, nil); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.st.FinishScan(f.t.Context(), f.lib.ID, []string{""}, []string{rel}); err != nil {
		f.t.Fatal(err)
	}
	var t, p string
	err := f.db.QueryRow(f.t.Context(), `
		SELECT v.item_id::text, p.id::text FROM part_files pf JOIN parts p ON p.id = pf.part_id
		JOIN versions v ON v.id = p.version_id WHERE pf.rel_path = $1`, rel).Scan(&t, &p)
	if err != nil {
		f.t.Fatal(err)
	}
	return uuid.MustParse(t), uuid.MustParse(p)
}

func (f *fixture) queued(part uuid.UUID) bool {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(f.t.Context(), `SELECT count(*) FROM jobs WHERE kind = 'previews' AND subject = $1`, part.String()).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n == 1
}

func (f *fixture) run(part uuid.UUID) {
	f.t.Helper()
	if err := f.make(f.t.Context(), part); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) setPreviews(level domain.PreviewLevel) {
	f.t.Helper()
	if err := f.st.SetLibrary(f.t.Context(), f.lib.ID, store.LibraryChange{Previews: level}); err != nil {
		f.t.Fatal(err)
	}
}

// chapterImages answers the image address of each chapter of the title's copy on disk.
func (f *fixture) chapterImages(title uuid.UUID) []string {
	f.t.Helper()
	page, err := f.st.Title(f.t.Context(), f.admin.ID, title)
	if err != nil {
		f.t.Fatal(err)
	}
	var images []string
	for _, v := range page.Versions {
		if v.MissingSince == nil {
			for _, c := range v.Chapters {
				images = append(images, c.Image)
			}
		}
	}
	return images
}

func TestAPartGetsSheetsAndChapterImages(t *testing.T) {
	f := newFixture(t)
	title, part := f.film("heat")
	if !f.queued(part) {
		t.Fatal("a new part was not queued for previews")
	}
	f.run(part)

	got, err := f.st.Trickplay(t.Context(), f.admin.ID, part)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Trickplay{Width: 320, Height: 180, IntervalMS: 10_000, Columns: 10, Rows: 10, Thumbnails: 105, Sheets: 2}
	if got != want {
		t.Errorf("trickplay = %+v, want %+v", got, want)
	}
	sheet, err := f.previews.Sheet(part, 1)
	if err != nil {
		t.Fatalf("the second sheet: %v", err)
	}
	_ = sheet.Close()

	images := f.chapterImages(title)
	if len(images) != 2 || images[0] != "/api/v1/parts/"+part.String()+"/chapters/0/image" {
		t.Errorf("chapter images = %q, want an address for each of the two chapters", images)
	}
	if err := f.st.HasChapterImage(t.Context(), f.admin.ID, part, 1); err != nil {
		t.Error(err)
	}
	still, err := f.previews.ChapterImage(part, 1)
	if err != nil {
		t.Fatalf("the second chapter's image: %v", err)
	}
	_ = still.Close()
}

func TestALibraryWithPreviewsOffGetsNone(t *testing.T) {
	f := newFixture(t)
	f.setPreviews(domain.PreviewsOff)
	title, part := f.film("heat")
	if f.queued(part) {
		t.Error("a part of a library making no previews was queued for them")
	}
	if n, err := f.st.QueuePreviews(t.Context()); err != nil || n != 0 {
		t.Errorf("the backfill queued %d parts (%v), want none", n, err)
	}
	f.run(part)
	if _, err := f.st.Trickplay(t.Context(), f.admin.ID, part); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("trickplay: %v, want none", err)
	}
	if images := f.chapterImages(title); images[0] != "" || images[1] != "" {
		t.Errorf("chapter images = %q, want none", images)
	}
}

func TestPreviewsFollowTheirLibrary(t *testing.T) {
	f := newFixture(t)
	title, part := f.film("heat")
	f.run(part)

	f.setPreviews(domain.PreviewsChapters)
	if n, err := f.st.QueuePreviews(t.Context()); err != nil || n != 1 {
		t.Fatalf("the backfill queued %d parts (%v), want the one with sheets", n, err)
	}
	f.run(part)
	if _, err := f.st.Trickplay(t.Context(), f.admin.ID, part); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("trickplay after the library asked for chapters alone: %v, want none", err)
	}
	if images := f.chapterImages(title); images[0] == "" {
		t.Error("the chapter images went with the sheets")
	}

	f.setPreviews(domain.PreviewsOff)
	if n, err := f.st.QueuePreviews(t.Context()); err != nil || n != 1 {
		t.Fatalf("the backfill queued %d parts (%v), want the one with chapter images", n, err)
	}
	f.run(part)
	if images := f.chapterImages(title); images[0] != "" {
		t.Error("chapter images outlived their library turning previews off")
	}
	if _, err := f.previews.ChapterImage(part, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the chapter image's file: %v, want it gone", err)
	}
}

func TestAChangedFileDropsOldPreviews(t *testing.T) {
	f := newFixture(t)
	title, old := f.film("heat")
	f.run(old)

	_, replaced := f.film("heat, recut")
	if replaced == old {
		t.Fatal("a file with new bytes is still the old part")
	}
	if images := f.chapterImages(title); images[0] != "" || images[1] != "" {
		t.Errorf("the new file's chapters show %q, the old file's images", images)
	}
	if _, err := f.st.Trickplay(t.Context(), f.admin.ID, replaced); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the new file's trickplay: %v, want none until it is made", err)
	}
	if !f.queued(replaced) {
		t.Error("the new file was not queued for previews")
	}
	if _, err := f.st.Trickplay(t.Context(), f.admin.ID, old); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the replaced file's trickplay: %v, want it forgotten", err)
	}
	f.age(old)
	if _, err := f.previews.Sweep(t.Context(), f.st.LivePreviews); err != nil {
		t.Fatal(err)
	}
	if _, err := f.previews.Sheet(old, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the replaced file's sheet: %v, want it gone", err)
	}
}

// age dates a part's previews folder past the sweep's wait for one being made.
func (f *fixture) age(part uuid.UUID) {
	f.t.Helper()
	long := time.Now().Add(-2 * madeLife)
	if err := os.Chtimes(filepath.Join(f.previews.dir, part.String()), long, long); err != nil {
		f.t.Fatal(err)
	}
}

// missingFor scans the library with the film's file gone, then dates its going d ago.
func (f *fixture) missingFor(d time.Duration) {
	f.t.Helper()
	if _, err := f.st.FinishScan(f.t.Context(), f.lib.ID, []string{""}, nil); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.Exec(f.t.Context(), `UPDATE versions SET missing_since = now() - $1::interval`, d.String()); err != nil {
		f.t.Fatal(err)
	}
}

func TestPreviewsOfAMissingFileLastTheGrace(t *testing.T) {
	const grace = 30 * 24 * time.Hour
	for _, c := range []struct {
		name    string
		missing time.Duration
		back    bool
		kept    bool
	}{
		{"missing a day", 24 * time.Hour, false, true},
		{"missing past the grace", grace + time.Hour, false, false},
		{"back after the grace", grace + time.Hour, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			_, part := f.film("heat")
			f.run(part)
			f.missingFor(c.missing)
			if c.back {
				f.film("heat")
			}
			if _, err := f.st.ForgetMissingPreviews(t.Context(), time.Now().Add(-grace)); err != nil {
				t.Fatal(err)
			}
			f.age(part)
			if _, err := f.previews.Sweep(t.Context(), f.st.LivePreviews); err != nil {
				t.Fatal(err)
			}
			_, err := f.previews.Sheet(part, 0)
			if kept := err == nil; kept != c.kept {
				t.Errorf("sheet kept = %v (%v), want %v", kept, err, c.kept)
			}
			_, err = f.st.Trickplay(t.Context(), f.admin.ID, part)
			if kept := err == nil; kept != c.kept {
				t.Errorf("trickplay kept = %v (%v), want %v", kept, err, c.kept)
			}
		})
	}
}

func TestTheSweepClearsPreviewsNoPartHas(t *testing.T) {
	f := newFixture(t)
	_, part := f.film("heat")
	f.run(part)
	stray := uuid.NewV7().String()
	if err := os.MkdirAll(filepath.Join(f.previews.dir, stray, "trickplay"), 0o750); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-2 * madeLife)
	for _, name := range []string{stray, part.String()} {
		if err := os.Chtimes(filepath.Join(f.previews.dir, name), long, long); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := f.previews.Sweep(t.Context(), f.st.LivePreviews); err != nil || n != 1 {
		t.Errorf("swept %d folders (%v), want the stray one", n, err)
	}
	if _, err := f.previews.Sheet(part, 0); err != nil {
		t.Errorf("the part's own sheet: %v", err)
	}
}
