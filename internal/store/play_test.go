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

func TestTheLongestCopyOnDiskPlaysUnlessOneIsAskedFor(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string, d time.Duration) Part {
		return Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{Duration: d}}
	}
	film := Film{Title: "Lawrence", Folder: "L", Copies: []Copy{
		{ContentKey: []byte("cut"), Label: "theatrical", Parts: []Part{part("L/theatrical.mkv", 3*time.Hour)}},
		{ContentKey: []byte("long"), Label: "restored", Parts: []Part{part("L/r1.mkv", 2*time.Hour), part("L/r2.mkv", 2*time.Hour)}},
	}}
	if _, err := s.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	longest, parts, err := s.Playable(ctx, uuid.UUID(item.ID), uuid.UUID{})
	if err != nil || len(parts) != 2 || parts[1].OffsetMS != (2*time.Hour).Milliseconds() {
		t.Fatalf("Playable = %v, %+v, %v; want the four-hour copy's two parts on one timeline", longest, parts, err)
	}
	v := s.q.Version
	theatrical, err := v.WithContext(ctx).Where(v.Label.Eq("theatrical")).Take()
	if err != nil {
		t.Fatal(err)
	}
	if got, parts, err := s.Playable(ctx, uuid.UUID(item.ID), uuid.UUID(theatrical.ID)); err != nil || got != uuid.UUID(theatrical.ID) || len(parts) != 1 {
		t.Errorf("asking for the theatrical cut: %v, %d parts, %v", got, len(parts), err)
	}
	if err := s.FinishScan(ctx, lib.ID, []string{"L"}, []string{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Playable(ctx, uuid.UUID(item.ID), uuid.UUID{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("with every copy's files gone: %v, want ErrNotFound", err)
	}
}
