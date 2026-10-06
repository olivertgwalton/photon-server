//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// A chapter with a picture is given it at the signed-in address and at one signed for a player
// with no token; one without is given neither.
func TestATitlesChapterPicturesAreSigned(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	facts := &media.Facts{Duration: time.Hour, Chapters: []media.Chapter{
		{Start: 0, End: time.Minute, Title: "One"}, {Start: time.Minute, End: time.Hour, Title: "Two"},
	}}
	film := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: facts}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	part, err := s.q.Part.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePreviews(ctx, uuid.UUID(part.ID), []int{1}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	p := page(t, s, uuid.UUID(item.ID))
	p.SignChapterImages(func(path string) string { return path + "?sig=x" })
	chapters := p.Versions[0].Chapters
	base := "/api/v1/parts/" + uuid.UUID(part.ID).String()
	if len(chapters) != 2 || chapters[0].Image != "" || chapters[0].SignedImage != "" ||
		chapters[1].Image != base+"/chapters/1/image" || chapters[1].SignedImage != base+"/chapter-images/1?sig=x" {
		t.Errorf("chapters: %+v, want the second's picture at both addresses", chapters)
	}
}
