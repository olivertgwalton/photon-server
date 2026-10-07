//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestATitleSaysWhereItsIntroAndCreditsAre(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	facts := &domain.Facts{Duration: 42 * time.Minute, Chapters: []domain.Chapter{
		{Start: 0, End: time.Minute, Title: "Cold Open"},
		{Start: time.Minute, End: 150 * time.Second, Title: "Opening"},
		{Start: 150 * time.Second, End: 40 * time.Minute, Title: "Chapter 3"},
		{Start: 40 * time.Minute, End: 42 * time.Minute, Title: "End Credits"},
	}}
	film := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: facts}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	id := cards[0].ID
	markers := func() []MarkerRef { return page(t, s, id).Versions[0].Markers }
	chapters := []MarkerRef{
		{Kind: domain.MarkerIntro, StartMS: 60_000, EndMS: 150_000, Source: domain.MarkerByChapter},
		{Kind: domain.MarkerCredits, StartMS: 2_400_000, EndMS: 2_520_000, Source: domain.MarkerByChapter},
	}
	if diff := cmp.Diff(chapters, markers()); diff != "" {
		t.Errorf("from its chapters (-want +got):\n%s", diff)
	}

	// The season's fingerprints, had it one, give way to the chapters.
	var part uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM parts`).Scan(&part); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFingerprintMarkers(ctx, []uuid.UUID{part}, map[uuid.UUID][]domain.Marker{
		part: {{Kind: domain.MarkerIntro, StartMS: 61_000, EndMS: 149_000}, {Kind: domain.MarkerRecap, StartMS: 1000, EndMS: 30_000}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := markers(); len(got) != 3 || got[0].Source != domain.MarkerByFingerprint || got[1] != chapters[0] {
		t.Errorf("with fingerprints: %+v, want the recap they found and the chapters' intro", got)
	}

	// An admin's word outranks both, and clearing it brings them back.
	version := page(t, s, id).Versions[0].ID
	if err := s.SetMarkers(ctx, version, []domain.Marker{{Kind: domain.MarkerIntro, StartMS: 62_000, EndMS: 148_500}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := markers(); len(got) != 3 || got[1] != (MarkerRef{Kind: domain.MarkerIntro, StartMS: 62_000, EndMS: 148_500, Source: domain.MarkerByUser}) {
		t.Errorf("set by hand: %+v, want the admin's intro", got)
	}
	if err := s.SetMarkers(ctx, version, []domain.Marker{{Kind: domain.MarkerCredits, StartMS: 2_500_000, EndMS: 2_600_000}}, nil); !errors.Is(err, ErrMarkerOutsidePart) {
		t.Errorf("credits past the end: %v, want ErrMarkerOutsidePart", err)
	}
	if err := s.SetMarkers(ctx, version, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := markers(); len(got) != 3 || got[1] != chapters[0] {
		t.Errorf("cleared: %+v, want the chapters' intro again", got)
	}

	// An admin's word that there is no intro or recap hides the chapters' and fingerprints', through
	// a rescan and the season compared again, until it is cleared.
	none := []domain.MarkerAbsent{{Kind: domain.MarkerIntro}, {Kind: domain.MarkerRecap}}
	if err := s.SetMarkers(ctx, version, nil, none); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(chapters[1:], markers()); diff != "" {
		t.Errorf("none said (-want +got):\n%s", diff)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v2"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFingerprintMarkers(ctx, []uuid.UUID{part}, map[uuid.UUID][]domain.Marker{
		part: {{Kind: domain.MarkerIntro, StartMS: 61_000, EndMS: 149_000}, {Kind: domain.MarkerRecap, StartMS: 1000, EndMS: 30_000}},
	}); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(chapters[1:], markers()); diff != "" {
		t.Errorf("none said, then rescanned and compared again (-want +got):\n%s", diff)
	}
	if err := s.SetMarkers(ctx, version, nil, []domain.MarkerAbsent{{Kind: domain.MarkerIntro, Part: 1}}); !errors.Is(err, ErrMarkerNoPart) {
		t.Errorf("none in a second part of a one-part copy: %v, want ErrMarkerNoPart", err)
	}
	if err := s.SetMarkers(ctx, version, []domain.Marker{{Kind: domain.MarkerIntro, StartMS: 62_000, EndMS: 148_500}}, none); !errors.Is(err, ErrMarkerRepeated) {
		t.Errorf("an intro and none: %v, want ErrMarkerRepeated", err)
	}
	if err := s.SetMarkers(ctx, version, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := markers(); len(got) != 3 || got[0].Source != domain.MarkerByFingerprint || got[1] != chapters[0] {
		t.Errorf("none cleared: %+v, want the fingerprints' recap and the chapters' intro again", got)
	}
	if err := s.SetMarkers(ctx, uuid.NewV7(), nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such copy: %v, want ErrNotFound", err)
	}
}

func page(t *testing.T, s *Store, id uuid.UUID) TitlePage {
	t.Helper()
	p, err := s.Title(t.Context(), uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
