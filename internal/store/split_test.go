//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestASplitFilmStaysApartThroughAScan(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string, d time.Duration) Part {
		return Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: d}}
	}
	film := Film{Title: "Lawrence", Folder: "L", Copies: []Copy{
		{ContentKey: []byte("cut"), Label: "theatrical", Parts: []Part{part("L/theatrical.mkv", 3*time.Hour)}},
		{ContentKey: []byte("long"), Label: "restored", Parts: []Part{part("L/restored.mkv", 4*time.Hour)}},
	}}
	scan := func() {
		t.Helper()
		if _, err := s.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
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
	if got := films(); len(got) != 1 {
		t.Fatalf("before splitting: %v, want one film of two copies", got)
	}
	id := oneItem(t, s, "kind = 'movie'").ID

	if err := s.SplitTitle(ctx, id); err != nil {
		t.Fatal(err)
	}
	after := films()
	if len(after) != 2 || len(after[id]) != 1 || after[id][0] != "restored" {
		t.Fatalf("after splitting: %v, want the restored copy, which plays first, kept and the theatrical one apart", after)
	}
	// The scan that grouped them would group them again; it leaves them apart.
	scan()
	if again := films(); len(again) != 2 || len(again[id]) != 1 {
		t.Errorf("after a scan: %v, want the two films still apart", again)
	}

	if err := s.SplitTitle(ctx, id); !errors.Is(err, ErrOneCopy) {
		t.Errorf("splitting a film of one copy: %v, want ErrOneCopy", err)
	}
	if err := s.SplitTitle(ctx, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("splitting no film: %v, want ErrNotFound", err)
	}
}
