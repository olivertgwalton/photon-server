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
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
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
	oliver, _ := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "h")
	kid, _ := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash")
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	for n, who := range []uuid.UUID{oliver.ID, kid.ID, oliver.ID} {
		p := domain.Playback{ID: uuid.NewV7(), Profile: who, Item: item, Version: c.Version, Method: domain.PlayRemux, Started: start.Add(time.Duration(n) * time.Minute)}
		if err := s.RecordPlay(ctx, p, p.Started.Add(10*time.Minute), time.Duration(n+1)*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	mine, total, err := s.History(ctx, oliver.ID, 0, 10)
	if err != nil || total != 2 || len(mine) != 2 || mine[0].PositionMS != 3*60_000 || mine[0].Card.Title != "Heat" || mine[0].Method != domain.PlayRemux {
		t.Errorf("Oliver's = %+v of %d, %v; want his two, the latest first", mine, total, err)
	}
	if all, total, err := s.History(ctx, uuid.UUID{}, 1, 1); err != nil || total != 3 || len(all) != 1 || all[0].Profile != kid.ID {
		t.Errorf("everyone's second = %+v of %d, %v; want the kid's", all, total, err)
	}
}
