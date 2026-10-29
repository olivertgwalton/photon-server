//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAMergedFilmStaysTogetherThroughAScan(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string, d time.Duration) Part {
		return Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: d}}
	}
	theatrical := Film{Title: "Heat", Year: 1995, Folder: "Heat (1995)", Versions: []Version{
		{ContentKey: []byte("theatrical"), Label: "theatrical", Parts: []Part{part("Heat (1995)/Heat.mkv", 3*time.Hour)}},
	}}
	extended := Film{Title: "Heat Extended", Year: 1995, Folder: "Heat Extended (1995)", Versions: []Version{
		{ContentKey: []byte("extended"), Label: "extended", Parts: []Part{part("Heat Extended (1995)/Heat Extended.mkv", 4*time.Hour)}},
	}}
	scan := func() {
		t.Helper()
		for _, f := range []Film{theatrical, extended} {
			if _, err := s.SaveFolder(ctx, lib.ID, f.Folder, []byte("v1"), []Film{f}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	scan()
	films := func() map[uuid.UUID][]string {
		t.Helper()
		rows, err := s.pool.Query(ctx, `
			SELECT i.id, v.label FROM items i JOIN versions v ON v.item_id = i.id WHERE i.kind = 'movie' ORDER BY v.label`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[uuid.UUID][]string{}
		for rows.Next() {
			var id uuid.UUID
			var label string
			if err := rows.Scan(&id, &label); err != nil {
				t.Fatal(err)
			}
			out[id] = append(out[id], label)
		}
		return out
	}
	if got := films(); len(got) != 2 {
		t.Fatalf("before merging: %v, want two films of a version each", got)
	}
	id := oneItem(t, s, "kind = 'movie' AND scan_title = 'Heat'").ID
	other := oneItem(t, s, "kind = 'movie' AND scan_title = 'Heat Extended'").ID

	viewer := addProfile(t, s, "Neil", domain.RoleUser, nil)
	watched := time.Date(2024, 3, 1, 20, 0, 0, 0, time.UTC)
	if err := s.MarkWatched(ctx, viewer.ID, other, &watched); err != nil {
		t.Fatal(err)
	}
	if err := s.Favourite(ctx, viewer.ID, other); err != nil {
		t.Fatal(err)
	}

	if err := s.MergeTitle(ctx, id, other); err != nil {
		t.Fatal(err)
	}
	together := func(when string) {
		t.Helper()
		got := films()
		if len(got) != 1 || !slices.Equal(got[id], []string{"extended", "theatrical"}) {
			t.Fatalf("%s: %v, want one film of both versions", when, got)
		}
		if title := oneItem(t, s, "id = $1", id).ScanTitle; title != "Heat" {
			t.Errorf("%s: the film is named %q, want its own name kept", when, title)
		}
	}
	together("after merging")
	if n := countRows(t, s, `SELECT count(*) FROM watch_state WHERE profile_id = $1 AND item_id = $2 AND watched_at = $3`,
		viewer.ID, id, watched); n != 1 {
		t.Errorf("the merged film's watch is not kept on the film it was merged into")
	}
	if n := countRows(t, s, `SELECT count(*) FROM favourites WHERE profile_id = $1 AND item_id = $2`, viewer.ID, id); n != 1 {
		t.Errorf("the merged film's favourite is not kept on the film it was merged into")
	}

	// The scan that kept them apart would part them again; it leaves them together.
	scan()
	together("after a scan")

	if err := s.MergeTitle(ctx, id, id); !errors.Is(err, ErrNotMergeable) {
		t.Errorf("merging a film into itself: %v, want ErrNotMergeable", err)
	}
	if err := s.MergeTitle(ctx, id, other); !errors.Is(err, ErrNotMergeable) {
		t.Errorf("merging a film merged already: %v, want ErrNotMergeable", err)
	}
	if err := s.MergeTitle(ctx, uuid.NewV7(), id); !errors.Is(err, ErrNotFound) {
		t.Errorf("merging into no film: %v, want ErrNotFound", err)
	}
}
