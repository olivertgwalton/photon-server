//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAFetchedSubtitleOutlivesAScanAndIsServedFromPostgres(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "H", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Copies: []Copy{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "H/heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "H", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	search, err := s.SubtitleSearchOf(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil || search.Query.Kind != domain.ItemMovie || search.Query.IDs[domain.ProviderIMDb] != "tt0113277" ||
		search.Root != "/srv/films" || search.RelPath != "H/heat.mkv" || search.Parts != 1 {
		t.Fatalf("SubtitleSearchOf = %+v, %v; want the film's IMDb id and its one file", search, err)
	}
	id, err := s.SaveFetchedSubtitle(ctx, search.Version, FetchedSubtitleFile{Language: language.French, Body: []byte("1\n")})
	if err != nil {
		t.Fatal(err)
	}
	// The scan finds no such file in the library, and keeps it all the same.
	if _, err := s.FinishScan(ctx, lib.ID, []string{"."}, []string{"H"}, []string{"H/heat.mkv"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil || len(c.Subtitles) != 1 || c.Subtitles[0].ID != id || c.Subtitles[0].Language != language.French {
		t.Fatalf("after a scan, the copy's subtitles: %+v, %v; want the fetched French one", c.Subtitles, err)
	}
	// It is in no library: its text is in Postgres.
	if root, _, err := s.SubtitleFile(ctx, id); err != nil || root != "" {
		t.Errorf("SubtitleFile = %q, %v; want no library root", root, err)
	}
	if body, err := s.SubtitleBody(ctx, id); err != nil || string(body) != "1\n" {
		t.Errorf("SubtitleBody = %q, %v", body, err)
	}
	if err := s.RemoveFetchedSubtitle(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFetchedSubtitle(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing it again: %v, want ErrNotFound", err)
	}
}
