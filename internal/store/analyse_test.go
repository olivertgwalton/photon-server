//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnalysingATitleReadsItsFilesAgain(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, shows.ID, LibraryChange{
		Previews: domain.PreviewsAll, Markers: domain.MarkersAll, Keyframes: domain.KeyframesIndex,
	}); err != nil {
		t.Fatal(err)
	}
	video := domain.Stream{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1280, Height: 720, Range: domain.RangeSDR}
	part := func(rel string) Part {
		return Part{RelPath: rel, Size: 1_000, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
			Container: "mp4", Duration: time.Hour, Streams: []domain.Stream{video},
		}}
	}
	// The first episode is one copy in two files.
	eps := []Episode{
		{
			Season: 1, Episodes: []int{1}, Title: "S1E1", Folder: "Wire", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte("e1"), Parts: []Part{part("Wire/e1.cd1.mp4"), part("Wire/e1.cd2.mp4")}}},
		},
		{
			Season: 1, Episodes: []int{2}, Title: "S1E2", Folder: "Wire", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte("e2"), Parts: []Part{part("Wire/e2.mp4")}}},
		},
	}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Wire", []byte("v1"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}

	queued := func(kind domain.JobKind) int {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = $1 AND due = 'now'`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := s.AnalyseTitle(ctx, oneItem(t, s, "kind = 'show'").ID); err != nil {
		t.Fatal(err)
	}
	if n := queued(domain.JobProbe); n != 3 {
		t.Errorf("analysing the show queued %d files to read, want all three of its episodes'", n)
	}
	if err := s.AnalyseTitle(ctx, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("analysing no title: %v, want ErrNotFound", err)
	}

	// The first file reads longer now, in another container, with a sound it was not read to have.
	var first, second uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM parts WHERE idx = 0 AND version_id = (
		SELECT version_id FROM parts GROUP BY version_id HAVING count(*) = 2)`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT id FROM parts WHERE idx = 1`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProbe(ctx, first, domain.Facts{
		Container: "matroska,webm", Duration: 2 * time.Hour,
		Streams: []domain.Stream{video, {Index: 1, Kind: domain.StreamAudio, Codec: "aac", Channels: 2}},
	}); err != nil {
		t.Fatal(err)
	}
	var streams int
	var offset, total int64
	var container string
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM streams WHERE part_id = $1), (SELECT offset_ms FROM parts WHERE id = $2),
			v.duration_ms, v.container
		FROM parts p JOIN versions v ON v.id = p.version_id WHERE p.id = $1`, first, second).
		Scan(&streams, &offset, &total, &container); err != nil {
		t.Fatal(err)
	}
	if streams != 2 || offset != (2*time.Hour).Milliseconds() || total != (3*time.Hour).Milliseconds() || container != "matroska,webm" {
		t.Errorf("after reading again: %d streams, second file at %d ms, copy %d ms in %q; want 2, two hours, three hours, matroska",
			streams, offset, total, container)
	}
	for _, kind := range []domain.JobKind{domain.JobKeyframes, domain.JobPreviews, domain.JobMarkers} {
		if n := queued(kind); n != 1 {
			t.Errorf("%d %s jobs due now, want the file's own (or its season's) asked for again", n, kind)
		}
	}
}
