//go:build integration

package analysis

import (
	"cmp"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Two of media's fixtures: a Matroska file whose Cues list its keyframes, and an MPEG-TS file,
// which lists none.
const (
	indexedFile   = "cues-end.mkv"
	unindexedFile = "transport.ts"
)

var indexedKeyframes = []int64{0, 1335, 2002, 4713, 5130, 7257}

func media9(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "media", "testdata", "keyframes", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) setKeyframes(mode domain.KeyframeMode) {
	f.t.Helper()
	if err := f.st.SetLibrary(f.t.Context(), f.lib.ID, store.LibraryChange{Keyframes: mode}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) keyframesQueued(part uuid.UUID) bool {
	f.t.Helper()
	var n int
	err := f.db.QueryRow(f.t.Context(), `SELECT count(*) FROM jobs WHERE kind = 'keyframes' AND subject = $1 AND state = 'queued'`, part.String()).Scan(&n)
	if err != nil {
		f.t.Fatal(err)
	}
	return n == 1
}

// stored answers the keyframes saved for a part, or false where there is no row.
func (f *fixture) stored(part uuid.UUID) ([]int64, bool) {
	f.t.Helper()
	var pts []int64
	err := f.db.QueryRow(f.t.Context(), `SELECT pts_ms FROM keyframes WHERE part_id = $1`, part.String()).Scan(&pts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return pts, true
}

// index runs the part's keyframes job and, as the worker would, finishes it.
func (f *fixture) index(part uuid.UUID) {
	f.t.Helper()
	if err := Keyframes(f.st)(f.t.Context(), part); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.Exec(f.t.Context(), `DELETE FROM jobs WHERE kind = 'keyframes' AND subject = $1`, part.String()); err != nil {
		f.t.Fatal(err)
	}
}

func TestAnIndexLibraryReadsOnlyTheContainersIndex(t *testing.T) {
	f := newFixture(t)
	_, mkv := f.film(media9(t, indexedFile))
	if !f.keyframesQueued(mkv) {
		t.Fatal("a new part was not queued for keyframes")
	}
	// No ffprobe: the index is read here.
	f.index(mkv)
	if got, _ := f.stored(mkv); !gocmp.Equal(got, indexedKeyframes) {
		t.Errorf("keyframes %v, want %v", got, indexedKeyframes)
	}

	f = newFixture(t)
	_, ts := f.film(media9(t, unindexedFile))
	f.index(ts)
	if got, ok := f.stored(ts); !ok || len(got) != 0 {
		t.Errorf("a file with no index was saved with %v, %t; want none known", got, ok)
	}
	known, err := f.st.Keyframes(t.Context(), ts)
	if err != nil || known.Mode != domain.KeyframesIndex || known.PtsMS == nil {
		t.Errorf("Keyframes = %+v, %v; want none known, read", known, err)
	}
}

func TestAFullLibraryWalksAFileWithNoIndex(t *testing.T) {
	path, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFPROBE"), "ffprobe"))
	if err != nil {
		t.Skipf("needs ffprobe: %v", err)
	}
	tools := media.Tools{FFprobe: media.Tool{Path: path}}
	f := newFixture(t)
	_, ts := f.film(media9(t, unindexedFile))
	f.index(ts)
	if got, _ := f.stored(ts); len(got) != 0 {
		t.Fatalf("an index library walked the file: %v", got)
	}

	// Switching to full queues the part with none known.
	f.setKeyframes(domain.KeyframesFull)
	if !f.keyframesQueued(ts) {
		t.Fatal("a part with no keyframes known was not queued when its library switched to full")
	}
	if _, ok := f.stored(ts); ok {
		t.Error("a part queued again kept its empty keyframes")
	}
	// Its index read finds none, and leaves the walk to the window's work.
	f.index(ts)
	if _, ok := f.stored(ts); ok {
		t.Error("a file with no index was walked as its index was read")
	}
	var walks int
	if err := f.db.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE kind = 'keyframe_walk' AND subject = $1`, ts.String()).Scan(&walks); err != nil || walks != 1 {
		t.Fatalf("%d walks queued (%v), want one", walks, err)
	}
	if err := WalkKeyframes(f.st, tools)(t.Context(), ts); err != nil {
		t.Fatal(err)
	}
	want := []int64{1483, 2818, 3485, 6196, 6614, 8741}
	if got, _ := f.stored(ts); !gocmp.Equal(got, want) {
		t.Errorf("walked keyframes %v, want %v", got, want)
	}

	// A part whose keyframes are known keeps them.
	f = newFixture(t)
	_, mkv := f.film(media9(t, indexedFile))
	f.index(mkv)
	f.setKeyframes(domain.KeyframesFull)
	if f.keyframesQueued(mkv) {
		t.Error("a part whose keyframes are known was queued again")
	}
	if got, _ := f.stored(mkv); !gocmp.Equal(got, indexedKeyframes) {
		t.Errorf("a part's known keyframes became %v", got)
	}
}

func TestAnOffLibraryFindsNoKeyframes(t *testing.T) {
	f := newFixture(t)
	f.setKeyframes(domain.KeyframesOff)
	_, part := f.film(media9(t, indexedFile))
	if f.keyframesQueued(part) {
		t.Error("a part of a library finding no keyframes was queued for them")
	}
	// A job queued before the switch reads nothing.
	f.index(part)
	if _, ok := f.stored(part); ok {
		t.Error("a part of a library finding no keyframes had them saved")
	}

	f.setKeyframes(domain.KeyframesIndex)
	if !f.keyframesQueued(part) {
		t.Error("switching keyframes back on did not queue the library's parts")
	}
}

// An import's keyframes jobs wait behind every scan and identify, and a part played before its turn
// goes ahead of them all.
func TestKeyframesJobsWaitBehindWhatAReaderSees(t *testing.T) {
	f := newFixture(t)
	_, part := f.film(media9(t, indexedFile))
	if err := f.st.ScanLibrary(t.Context(), f.lib.ID, 0); err != nil {
		t.Fatal(err)
	}
	claim := func() domain.JobKind {
		t.Helper()
		jobs, err := f.st.ClaimJobs(t.Context(), []domain.JobKind{domain.JobKeyframes, domain.JobScanLibrary}, nil, uuid.NewV7(), time.Minute, 1)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("claimed %v, %v", jobs, err)
		}
		return jobs[0].Kind
	}
	if kind := claim(); kind != domain.JobScanLibrary {
		t.Errorf("claimed %s before the later scan", kind)
	}
	other, err := f.st.AddLibrary(t.Context(), "Other", domain.LibraryMovies, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.ScanLibrary(t.Context(), other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AskKeyframes(t.Context(), part); err != nil {
		t.Fatal(err)
	}
	if kind := claim(); kind != domain.JobKeyframes {
		t.Errorf("claimed %s before keyframes a play asked for", kind)
	}
}
