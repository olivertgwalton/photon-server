//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestATitleIsDeletedOnlyWhereItsLibraryAllows(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string) Part {
		return Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}
	}
	ep := Episode{
		Season: 1, Episodes: []int{1}, Title: "S1E1", Folder: "Wire/Season 1", ByNumber: true,
		Copies: []Copy{{
			ContentKey: []byte("e1"), Parts: []Part{part("Wire/Season 1/e1.cd1.mkv"), part("Wire/Season 1/e1.cd2.mkv")},
			Subtitles: []Subtitle{{RelPath: "Wire/Season 1/e1.en.srt", Size: 1, ModTime: time.Unix(0, 0), Codec: "subrip"}},
		}},
	}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Wire/Season 1", []byte("v1"), Show{Title: "The Wire", Folder: "Wire"}, []Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, "kind = 'show'").ID

	if _, err := s.TitleFiles(ctx, show); !errors.Is(err, ErrDeletionOff) {
		t.Fatalf("deleting where the library does not allow it: %v, want ErrDeletionOff", err)
	}
	if err := s.SetLibrary(ctx, shows.ID, LibraryChange{Deletion: domain.DeletionFiles}); err != nil {
		t.Fatal(err)
	}
	files, err := s.TitleFiles(ctx, show)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, f := range files {
		if f.Root != "/srv/shows" {
			t.Errorf("a file in %q, want the library's root", f.Root)
		}
		rels = append(rels, f.Rel)
	}
	if want := []string{"Wire/Season 1/e1.cd1.mkv", "Wire/Season 1/e1.cd2.mkv", "Wire/Season 1/e1.en.srt"}; !slices.Equal(rels, want) {
		t.Errorf("the show's files: %v, want its episode's two parts and its subtitles", rels)
	}

	if err := s.ForgetTitle(ctx, show); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&left); err != nil || left != 0 {
		t.Errorf("after forgetting the show: %d titles, %v; want none, its season and episode with it", left, err)
	}
	if _, err := s.TitleFiles(ctx, show); !errors.Is(err, ErrNotFound) {
		t.Errorf("a forgotten show's files: %v, want ErrNotFound", err)
	}
}
