//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// An episode no provider rates wears its show's certificate, on its card and its page, as Plex
// rates an episode by its show.
func TestAnEpisodeWearsItsShowsCertificate(t *testing.T) {
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
	ep, err := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemEpisode))).Take()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, uuid.UUID(show.ID), domain.SourceTMDB, domain.Metadata{
		Certificate: "TV-14",
		Artwork: []domain.Artwork{
			{Kind: domain.ArtworkPoster, URL: "https://images.test/show-poster.jpg"},
			{Kind: domain.ArtworkBackdrop, URL: "https://images.test/show-backdrop.jpg"},
			{Kind: domain.ArtworkLogo, URL: "https://images.test/show-logo.png"},
		},
	}, nil); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.AddProfile(ctx, "Viewer", domain.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}

	cards, err := s.cards(ctx, viewer.ID, []*model.Item{ep})
	if err != nil {
		t.Fatal(err)
	}
	if cards[0].Certificate != "TV-14" {
		t.Errorf("card certificate = %q, want the show's TV-14", cards[0].Certificate)
	}
	if cards[0].Poster == uuid.Nil() || cards[0].Backdrop == uuid.Nil() || cards[0].Logo == uuid.Nil() {
		t.Errorf("episode card = %+v, want the show's poster, backdrop and logo", cards[0])
	}
	page, err := s.Title(ctx, viewer.ID, uuid.UUID(ep.ID))
	if err != nil {
		t.Fatal(err)
	}
	if page.Certificate != "TV-14" {
		t.Errorf("page certificate = %q, want the show's TV-14", page.Certificate)
	}
}
