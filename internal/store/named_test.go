//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// An id is told for what it names, a title and its kind or an episode announced, only to a profile
// that may open it.
func TestAnIDIsToldForWhatItNames(t *testing.T) {
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
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{other.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	pilot := Episode{Season: 1, Episodes: []int{1}, Title: "Pilot", Folder: "Severance", ByNumber: true, Copies: []Copy{{
		ContentKey: []byte("1"), Parts: []Part{{RelPath: "1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}},
	}}}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Severance", []byte("v1"), Show{Title: "Severance", Folder: "Severance"}, []Episode{pilot}, nil); err != nil {
		t.Fatal(err)
	}
	show, season := oneItem(t, s, "kind = 'show'").ID, oneItem(t, s, "kind = 'season'").ID
	aired := time.Date(2026, time.October, 17, 0, 0, 0, 0, time.UTC)
	if err := s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{}, map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{
		2: {Title: "Half Loop", ReleaseDate: aired},
	}}}); err != nil {
		t.Fatal(err)
	}
	days, err := s.Calendar(ctx, CalendarQuery{Profile: admin.ID, Start: aired, End: aired, Filter: domain.CalendarAll})
	if err != nil || len(days) != 1 {
		t.Fatal(days, err)
	}
	announced := days[0].Entries[0].ID

	for _, c := range []struct {
		id   uuid.UUID
		want Named
	}{
		{show, Named{Kind: NamedTitle, Title: domain.ItemShow}},
		{season, Named{Kind: NamedTitle, Title: domain.ItemSeason}},
		{announced, Named{Kind: NamedAnnounced}},
	} {
		if got, err := s.Named(ctx, admin.ID, c.id); err != nil || got != c.want {
			t.Errorf("%v names %+v, %v; want %+v", c.id, got, err, c.want)
		}
		if _, err := s.Named(ctx, kid.ID, c.id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%v, to a profile that cannot open its library: %v, want ErrNotFound", c.id, err)
		}
	}
	if _, err := s.Named(ctx, admin.ID, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("an id of nothing: %v, want ErrNotFound", err)
	}
}
