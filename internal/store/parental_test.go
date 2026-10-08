//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	part := func(name string) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}
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
		ids[f.title] = oneItem(t, s, "title = $1", f.title).ID
		if f.cert != "" {
			if err := s.SaveIdentity(ctx, ids[f.title], domain.SourceTMDB, domain.Metadata{Certificate: f.cert}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Show", Folder: "Show/Season 1", ByNumber: true, Copies: []Copy{part("Show1")}}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	show, ep := oneItem(t, s, `kind = 'show'`).ID, oneItem(t, s, `kind = 'episode'`).ID
	if err := s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{Certificate: "TV-14"}, nil); err != nil {
		t.Fatal(err)
	}

	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	walls := func() []string {
		t.Helper()
		var out []string
		for _, lib := range []uuid.UUID{films.ID, other.ID, tv.ID} {
			cards, total, err := s.Wall(ctx, []uuid.UUID{lib}, WallPage{Profile: kid.ID, Sort: domain.SortTitle, Limit: 10})
			if err != nil || int(total) != len(cards) {
				t.Fatal(cards, total, err)
			}
			for _, c := range cards {
				out = append(out, c.Title)
			}
		}
		return out
	}
	partOf := func(item uuid.UUID) uuid.UUID {
		t.Helper()
		var part uuid.UUID
		if err := s.pool.QueryRow(ctx, `
			SELECT p.id FROM parts p JOIN versions v ON v.id = p.version_id WHERE v.item_id = $1 LIMIT 1`, item).Scan(&part); err != nil {
			t.Fatal(err)
		}
		return part
	}
	counted := func(want map[uuid.UUID]domain.TitleCounts) {
		t.Helper()
		got, err := s.LibraryCounts(ctx, kid.ID)
		if err != nil || len(got) != len(want) {
			t.Fatalf("counts = %+v, %v; want %+v", got, err, want)
		}
		for lib, n := range want {
			if got[lib] != n {
				t.Errorf("counts of %s = %+v, want %+v", lib, got[lib], n)
			}
		}
	}
	if got := walls(); len(got) != 5 {
		t.Errorf("with no limits: %q, want everything", got)
	}
	counted(map[uuid.UUID]domain.TitleCounts{films.ID: {Movies: 3}, other.ID: {Movies: 1}, tv.ID: {Shows: 1, Seasons: 1, Episodes: 1}})
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID, tv.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := walls(); !slices.Equal(got, []string{"Paddington"}) {
		t.Errorf("12 and under, rated, Films and TV alone: %q, want Paddington", got)
	}
	counted(map[uuid.UUID]domain.TitleCounts{films.ID: {Movies: 1}})
	for name, id := range map[string]uuid.UUID{"a film rated 15": ids["Heat"], "a TV-14 show's episode": ep, "Up, in a library it lacks": ids["Up"]} {
		if _, err := s.Title(ctx, kid.ID, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
		if _, err := s.Playable(ctx, kid.ID, id, uuid.UUID{}); !errors.Is(err, ErrNotFound) {
			t.Errorf("playing %s: %v, want ErrNotFound", name, err)
		}
		if _, _, err := s.VisiblePartFile(ctx, kid.ID, partOf(id)); !errors.Is(err, ErrNotFound) {
			t.Errorf("timing the connection on %s: %v, want ErrNotFound", name, err)
		}
	}
	// The episode is called Show too, and is rated by its show.
	for _, text := range []string{"heat", "show"} {
		found, total, err := s.Search(ctx, SearchQuery{Profile: kid.ID, Text: text, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 0 || total != 0 {
			t.Errorf("searching for %q: %+v, want nothing", text, found)
		}
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
	if root, rel, err := s.VisiblePartFile(ctx, kid.ID, partOf(ids["Paddington"])); err != nil || root != "/srv/films" || rel != "Paddington.mkv" {
		t.Errorf("timing the connection on Paddington: %q %q %v", root, rel, err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedAllow}, nil); err != nil {
		t.Fatal(err)
	}
	if got := walls(); !slices.Equal(got, []string{"Home Movie", "Paddington", "Up"}) {
		t.Errorf("12 and under, unrated allowed, every library: %q", got)
	}
	counted(map[uuid.UUID]domain.TitleCounts{films.ID: {Movies: 2}, other.ID: {Movies: 1}})
	got, err := s.Access(ctx, kid.ID, nil)
	if err != nil || got.MaxAge == nil || *got.MaxAge != 12 || got.Unrated != domain.UnratedAllow || len(got.Libraries) != 0 {
		t.Errorf("access = %+v, %v", got, err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{uuid.NewV7()}}, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a library there is not: %v, want ErrNotFound", err)
	}
}

func TestCertificatesAreReadAsTheirCountriesRateThem(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetServerSettings(ctx, domain.ServerSettings{Locale: domain.Locale{Language: "hi-IN", Country: "in"}}); err != nil {
		t.Fatal(err)
	}
	part := func(name string) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}
	}
	rate := func(id uuid.UUID, cert string) {
		t.Helper()
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Certificate: cert}, nil); err != nil {
			t.Fatal(err)
		}
	}
	films := map[string]string{"Heat": "US:R", "Fanny and Alexander": "SE:15 / SE:15+", "Sholay": "A", "Paddington": "U"}
	ids := map[string]uuid.UUID{}
	for title, cert := range films {
		if _, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{{Title: title, Folder: title, Copies: []Copy{part(title)}}}, nil); err != nil {
			t.Fatal(err)
		}
		ids[title] = oneItem(t, s, "title = $1", title).ID
		rate(ids[title], cert)
	}
	var eps []Episode
	for n := 1; n <= 2; n++ {
		name := "Show" + string(rune('0'+n))
		eps = append(eps, Episode{Season: 1, Episodes: []int{n}, Title: name, Folder: "Show/Season 1", ByNumber: true, Copies: []Copy{part(name)}})
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	ids["the show"] = oneItem(t, s, `kind = 'show'`).ID
	rate(ids["the show"], "TV-14")
	for n, name := range []string{"an episode rated as its show", "an episode rated TV-MA"} {
		ids[name] = oneItem(t, s, `kind = 'episode' AND episode_number = $1`, n+1).ID
	}
	rate(ids["an episode rated TV-MA"], "TV-MA")

	teen, err := s.AddProfile(ctx, "Teen", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	fourteen := 14
	if err := s.SetAccess(ctx, teen.ID, ProfileAccess{MaxAge: &fourteen, Unrated: domain.UnratedAllow}, nil); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"Heat": false, "Fanny and Alexander": false, "Sholay": false, "Paddington": true,
		"the show": true, "an episode rated as its show": true, "an episode rated TV-MA": false,
	} {
		_, err := s.Title(ctx, teen.ID, ids[name])
		if seen := err == nil; seen != want || (err != nil && !errors.Is(err, ErrNotFound)) {
			t.Errorf("%s (%s) at 14: seen %v, %v; want seen %v", name, films[name], seen, err, want)
		}
	}
	// A library's counts are what the profile sees, its own episode's certificate too.
	counts, err := s.LibraryCounts(ctx, teen.ID)
	if want := (domain.TitleCounts{Movies: 1, Shows: 1, Seasons: 1, Episodes: 1}); err != nil || counts[lib.ID] != want {
		t.Errorf("counts at 14: %+v, %v; want %+v", counts[lib.ID], err, want)
	}
	counts, err = s.LibraryCounts(ctx, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	if want := (domain.TitleCounts{Movies: 4, Shows: 1, Seasons: 1, Episodes: 2}); counts[lib.ID] != want {
		t.Errorf("the server's counts: %+v, want %+v", counts[lib.ID], want)
	}
}
