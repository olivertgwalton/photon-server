//go:build integration

package store

import (
	"maps"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestIdentityDescribesAShowsEpisodes(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	episode := func(n int) Episode {
		key := []byte{byte(n)}
		return Episode{
			Season: 1, Episodes: []int{n}, Title: "the wire", Folder: "The Wire/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: key, Parts: []Part{{
				RelPath: "The Wire/Season 1/" + string(rune('0'+n)) + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{},
			}}}},
		}
	}
	show := Show{Title: "the wire", Folder: "The Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire/Season 1", []byte("v1"), show, []Episode{episode(1), episode(2)}, nil); err != nil {
		t.Fatal(err)
	}
	i := s.q.Item
	row, err := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemShow))).Take()
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := s.q.Job.WithContext(ctx).Where(s.q.Job.Kind.Eq(string(domain.JobIdentify)), s.q.Job.Subject.Eq(row.ID)).Count(); n != 1 {
		t.Errorf("%d identify jobs for the new show, want 1", n)
	}

	sub, ok, err := s.IdentifySubject(ctx, uuid.UUID(row.ID))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if sub.IDs[domain.ProviderTVDB] != "79126" || len(sub.Seasons) != 1 || sub.Seasons[0] != 1 {
		t.Errorf("subject = %+v, want the TVDB id and season 1", sub)
	}

	err = s.SaveIdentity(ctx, uuid.UUID(row.ID), domain.Metadata{Title: "The Wire", IDs: map[domain.Provider]string{
		domain.ProviderTMDB: "1438", domain.ProviderTVDB: "1",
	}}, map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: "The Target"}}}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := i.WithContext(ctx).Find()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		key := string(it.Kind)
		if it.EpisodeNumber != nil {
			key += string(rune('0' + *it.EpisodeNumber))
		}
		got[key] = it.Title
	}
	want := map[string]string{"show": "The Wire", "season": "Season 1", "episode1": "The Target", "episode2": "the wire"}
	if !maps.Equal(got, want) {
		t.Errorf("titles = %v, want %v", got, want)
	}
	ids, _ := s.q.ExternalID.WithContext(ctx).Where(s.q.ExternalID.ItemID.Eq(row.ID)).Find()
	for _, id := range ids {
		if id.Provider == domain.ProviderTVDB && id.Value != "79126" {
			t.Errorf("a matched TVDB id replaced the folder's: %s", id.Value)
		}
	}
}
