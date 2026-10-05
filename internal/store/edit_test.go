//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestAnEditStandsUntilItIsReset(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat/heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{}}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	row, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.UUID(row.ID)
	matched := domain.Metadata{Title: "Heat", Overview: "A heist.", Tagline: "A Los Angeles crime saga", IDs: map[domain.Provider]string{domain.ProviderTMDB: "999", domain.ProviderIMDb: "tt0000001"}}
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, matched, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.EditMetadata(ctx, id, domain.Metadata{Title: "Heat (Director's Cut)", Locked: []domain.Field{domain.FieldTagline}}); err != nil {
		t.Fatal(err)
	}
	// Matched again: the edit and the lock stand, the rest is the provider's.
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, matched, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, id)
	if err != nil || page.Title != "Heat (Director's Cut)" || page.Overview != "A heist." {
		t.Fatalf("after matching again: %q %q, %v", page.Title, page.Overview, err)
	}
	if err := s.ResetEdits(ctx, id, []domain.Field{domain.FieldTitle}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, matched, nil); err != nil {
		t.Fatal(err)
	}
	locked := matched
	locked.Tagline = "Another tagline"
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, locked, nil); err != nil {
		t.Fatal(err)
	}
	if page, _ = s.Title(ctx, uuid.UUID{}, id); page.Title != "Heat" || page.Tagline != "A Los Angeles crime saga" {
		t.Errorf("after resetting the title alone: %q, tagline %q; want TMDB's title and the tagline kept as it was locked", page.Title, page.Tagline)
	}
	j := s.q.Job
	if n, _ := j.WithContext(ctx).Where(j.Kind.Eq(string(domain.JobScanLibrary))).Count(); n != 1 {
		t.Errorf("%d library scans queued after a reset, want 1", n)
	}
	if n, _ := s.q.Folder.WithContext(ctx).Count(); n != 0 {
		t.Errorf("%d folders remembered after a reset, want its folder to be read again", n)
	}

	// The wrong film was matched: the admin pins the right one.
	if err := s.PinMatch(ctx, id, domain.ProviderTMDB, "949"); err != nil {
		t.Fatal(err)
	}
	sub, _, err := s.IdentifySubject(ctx, id)
	if err != nil || sub.IDs[domain.ProviderTMDB] != "949" || sub.IDs[domain.ProviderIMDb] != "" {
		t.Errorf("after pinning: ids %v, %v; want TMDB's pinned and the IMDb id the wrong match gave gone", sub.IDs, err)
	}
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "999"}}, nil); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ = s.IdentifySubject(ctx, id); sub.IDs[domain.ProviderTMDB] != "949" {
		t.Errorf("a match gave %q, want the pinned id to stand", sub.IDs[domain.ProviderTMDB])
	}
	if err := s.EditMetadata(ctx, uuid.NewV7(), domain.Metadata{Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("editing no title: %v, want ErrNotFound", err)
	}
}
