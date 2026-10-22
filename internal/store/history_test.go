//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestEachPlayIsKeptInTheHistory(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "Heat", Versions: []Version{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	c, err := s.Playable(ctx, uuid.UUID{}, item, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	for n, who := range []uuid.UUID{oliver.ID, kid.ID, oliver.ID} {
		p := domain.Playback{ID: uuid.NewV7(), Profile: who, Item: item, Version: c.Version, Method: domain.PlayRemux, Started: start.Add(time.Duration(n) * time.Minute)}
		if err := s.RecordPlay(ctx, p, p.Started.Add(10*time.Minute), time.Duration(n+1)*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	mine, more, err := s.History(ctx, oliver.ID, Newest, 10)
	if err != nil || more || len(mine) != 2 || mine[0].PositionMS != 3*60_000 || mine[0].Card.Title != "Heat" || mine[0].Method != domain.PlayRemux {
		t.Errorf("Oliver's = %+v, more %v, %v; want his two, the latest first, and no more", mine, more, err)
	}
	first, more, err := s.History(ctx, uuid.UUID{}, Newest, 1)
	if err != nil || !more || len(first) != 1 || first[0].Profile != oliver.ID {
		t.Fatalf("everyone's first = %+v, more %v, %v; want Oliver's latest, and more", first, more, err)
	}
	// A play stopped as the history is read is on no later page: the next goes on from the last read.
	p := domain.Playback{ID: uuid.NewV7(), Profile: kid.ID, Item: item, Version: c.Version, Method: domain.PlayDirect, Started: start.Add(time.Hour)}
	if err := s.RecordPlay(ctx, p, p.Started.Add(time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	next := Before{At: first[0].StoppedAt, ID: first[0].ID}
	if second, more, err := s.History(ctx, uuid.UUID{}, next, 1); err != nil || !more || len(second) != 1 || second[0].Profile != kid.ID || second[0].StoppedAt.After(next.At) {
		t.Errorf("everyone's second = %+v, more %v, %v; want the kid's first play, as before the new one", second, more, err)
	}
}
