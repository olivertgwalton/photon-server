//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// An episode wears its show's backdrop, poster and lettering where it has none, on its card and its
// page, as Plex answers an episode with its show's art, and keeps its own still.
func TestAnEpisodeWearsItsShowsPictures(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	part := Copy{ContentKey: []byte("e1"), Parts: []Part{{RelPath: "e1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Pilot", Folder: "Show/Season 1", ByNumber: true, Copies: []Copy{part}}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	i := s.q.Item
	show, err := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemShow))).Take()
	if err != nil {
		t.Fatal(err)
	}
	art := func(kind domain.ArtworkKind, name string) domain.Artwork {
		return domain.Artwork{Kind: kind, URL: "https://image.tmdb.org/t/p/original/" + name + ".jpg"}
	}
	seasons := map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{
		1: {Title: "Pilot", Artwork: []domain.Artwork{art(domain.ArtworkThumb, "still")}},
	}}}
	if err := s.SaveIdentity(ctx, uuid.UUID(show.ID), domain.SourceTMDB, domain.Metadata{Title: "Show", Artwork: []domain.Artwork{
		art(domain.ArtworkPoster, "poster"), art(domain.ArtworkBackdrop, "backdrop"), art(domain.ArtworkLogo, "logo"),
	}}, seasons); err != nil {
		t.Fatal(err)
	}
	ep, err := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemEpisode))).Take()
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.AddProfile(ctx, "Viewer", domain.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}
	showPictures, _, err := s.pictureOrder(ctx, []*model.Item{show})
	if err != nil {
		t.Fatal(err)
	}
	ownPictures, _, err := s.pictureOrder(ctx, []*model.Item{ep})
	if err != nil {
		t.Fatal(err)
	}
	want := func(kind domain.ArtworkKind) uuid.UUID { return first(showPictures[show.ID][kind]) }
	still := first(ownPictures[ep.ID][domain.ArtworkThumb])
	if still == (uuid.UUID{}) {
		t.Fatal("the episode has no still of its own")
	}

	cards, err := s.cards(ctx, viewer.ID, []*model.Item{ep})
	if err != nil {
		t.Fatal(err)
	}
	c := cards[0]
	if c.Poster != want(domain.ArtworkPoster) || c.Backdrop != want(domain.ArtworkBackdrop) || c.Logo != want(domain.ArtworkLogo) {
		t.Errorf("card poster, backdrop, logo = %v %v %v, want the show's", c.Poster, c.Backdrop, c.Logo)
	}
	if c.Thumb != still {
		t.Errorf("card thumb = %v, want the episode's own still %v", c.Thumb, still)
	}
	page, err := s.Title(ctx, viewer.ID, uuid.UUID(ep.ID))
	if err != nil {
		t.Fatal(err)
	}
	if first(page.Artwork[domain.ArtworkBackdrop]) != want(domain.ArtworkBackdrop) || first(page.Artwork[domain.ArtworkLogo]) != want(domain.ArtworkLogo) {
		t.Errorf("page artwork = %v, want the show's backdrop and lettering", page.Artwork)
	}
	if first(page.Artwork[domain.ArtworkThumb]) != still {
		t.Errorf("page thumb = %v, want the episode's own still", page.Artwork[domain.ArtworkThumb])
	}
}
