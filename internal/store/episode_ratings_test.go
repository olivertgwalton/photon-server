//go:build integration

package store

import (
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// An episode keeps the score its show's provider gave it, as Jellyfin and Plex show one.
func TestAnEpisodeKeepsItsOwnScore(t *testing.T) {
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
	show := oneItem(t, s, "kind = 'show'")
	score := domain.Rating{Site: domain.SiteTMDB, Score: 81, Votes: 120}
	seasons := map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: "The Target", Ratings: []domain.Rating{score}}}}}
	if err := s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, domain.Metadata{Title: "Show"}, seasons); err != nil {
		t.Fatal(err)
	}
	ep := oneItem(t, s, "kind = 'episode'")
	viewer, err := s.AddProfile(ctx, "Viewer", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	cards, err := s.cards(ctx, viewer.ID, []*model.Item{ep})
	if err != nil {
		t.Fatal(err)
	}
	if got := cards[0].Ratings; len(got) != 1 || got[0].Site != domain.SiteTMDB || got[0].Score != 81 {
		t.Errorf("episode card ratings = %+v, want TMDB's 81", got)
	}
}
