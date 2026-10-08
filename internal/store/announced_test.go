//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnAnnouncedEpisodeIsFoundByTheIDTheCalendarGives(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary(ctx, "Other", domain.LibraryShows, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{other.ID}}); err != nil {
		t.Fatal(err)
	}
	episodes := func(numbers ...int) []Episode {
		var out []Episode
		for _, n := range numbers {
			rel := string(rune('0'+n)) + ".mkv"
			out = append(out, Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: "Severance", ByNumber: true, Copies: []Copy{{
				ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}},
			}}})
		}
		return out
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Severance", []byte("v1"), Show{Title: "Severance", Folder: "Severance"}, episodes(1), nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, "kind = 'show'").ID
	aired := time.Date(2026, time.October, 17, 0, 0, 0, 0, time.UTC)
	if err := s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{}, map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{
		2: {Title: "Half Loop", Overview: "Mark goes back.", ReleaseDate: aired},
	}}}); err != nil {
		t.Fatal(err)
	}
	days, err := s.Calendar(ctx, CalendarQuery{Profile: admin.ID, Start: aired, End: aired, Filter: domain.CalendarAll})
	if err != nil || len(days) != 1 {
		t.Fatal(days, err)
	}
	id := days[0].Entries[0].ID
	got, err := s.AnnouncedEpisode(ctx, admin.ID, id)
	if err != nil || got.Title != "Half Loop" || got.Overview != "Mark goes back." || got.Show == nil || got.Show.ID != show || *got.EpisodeNumber != 2 {
		t.Errorf("announced episode = %+v, %v; want Half Loop, of Severance", got, err)
	}
	if _, err := s.AnnouncedEpisode(ctx, kid.ID, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("for a profile that cannot open its library: %v, want ErrNotFound", err)
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Severance", []byte("v2"), Show{Title: "Severance", Folder: "Severance"}, episodes(1, 2), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AnnouncedEpisode(ctx, admin.ID, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("once its file landed: %v, want ErrNotFound", err)
	}
}
