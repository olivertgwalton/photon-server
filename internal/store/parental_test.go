//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestAProfileSeesOnlyWhatItMay(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary(ctx, "Other", domain.LibraryMovies, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	part := func(name string) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{}}}}
	}
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		lib         uuid.UUID
		title, cert string
	}{
		{films.ID, "Paddington", "PG"}, {films.ID, "Heat", "15"}, {films.ID, "Home Movie", ""}, {other.ID, "Up", "U"},
	} {
		if _, err := s.SaveFolder(ctx, f.lib, f.title, []byte("v1"), []Film{{Title: f.title, Folder: f.title, Copies: []Copy{part(f.title)}}}, nil); err != nil {
			t.Fatal(err)
		}
		i := s.q.Item
		row, err := i.WithContext(ctx).Where(i.Title.Eq(f.title)).Take()
		if err != nil {
			t.Fatal(err)
		}
		ids[f.title] = uuid.UUID(row.ID)
		if f.cert != "" {
			if err := s.SaveIdentity(ctx, ids[f.title], domain.SourceTMDB, domain.Metadata{Certificate: f.cert}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Show", Folder: "Show/Season 1", ByNumber: true, Copies: []Copy{part("Show1")}}
	if _, err := s.SaveShowFolder(ctx, films.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	i := s.q.Item
	show, _ := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemShow))).Take()
	ep, _ := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemEpisode))).Take()
	if err := s.SaveIdentity(ctx, uuid.UUID(show.ID), domain.SourceTMDB, domain.Metadata{Certificate: "TV-14"}, nil); err != nil {
		t.Fatal(err)
	}

	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "")
	if err != nil {
		t.Fatal(err)
	}
	walls := func() []string {
		t.Helper()
		var out []string
		for _, lib := range []uuid.UUID{films.ID, other.ID} {
			cards, total, err := s.Wall(ctx, lib, WallPage{Profile: kid.ID, Sort: domain.SortTitle, Limit: 10})
			if err != nil || int(total) != len(cards) {
				t.Fatal(cards, total, err)
			}
			for _, c := range cards {
				out = append(out, c.Title)
			}
		}
		return out
	}
	if got := walls(); len(got) != 5 {
		t.Errorf("with no limits: %q, want everything", got)
	}
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := walls(); !slices.Equal(got, []string{"Paddington"}) {
		t.Errorf("12 and under, rated, Films alone: %q, want Paddington", got)
	}
	for name, id := range map[string]uuid.UUID{"a film rated 15": ids["Heat"], "a TV-14 show's episode": uuid.UUID(ep.ID), "Up, in a library it lacks": ids["Up"]} {
		if _, err := s.Title(ctx, kid.ID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
		if _, err := s.Playable(ctx, kid.ID, id, uuid.UUID{}); !errors.Is(err, ErrNotFound) {
			t.Errorf("playing %s: %v, want ErrNotFound", name, err)
		}
	}
	if found, _ := s.Search(ctx, SearchQuery{Profile: kid.ID, Text: "heat", Limit: 10}); len(found) != 0 {
		t.Errorf("searching for Heat: %+v, want nothing", found)
	}
	rows, err := s.Home(ctx, kid.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, c := range r.Cards {
			if c.Title != "Paddington" {
				t.Errorf("home row %s holds %q", r.Kind, c.Title)
			}
		}
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedAllow}); err != nil {
		t.Fatal(err)
	}
	if got := walls(); !slices.Equal(got, []string{"Home Movie", "Paddington", "Up"}) {
		t.Errorf("12 and under, unrated allowed, every library: %q", got)
	}
	got, err := s.Access(ctx, kid.ID)
	if err != nil || got.MaxAge == nil || *got.MaxAge != 12 || got.Unrated != domain.UnratedAllow || len(got.Libraries) != 0 {
		t.Errorf("access = %+v, %v", got, err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{uuid.NewV7()}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a library there is not: %v, want ErrNotFound", err)
	}
}
