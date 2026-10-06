//go:build integration

package store

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAPersonIsCreditedOnceAcrossTitles(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Mixed", domain.LibraryMovies, "/srv/mixed")
	if err != nil {
		t.Fatal(err)
	}
	part := func(name string) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Alien", []byte("v1"), []Film{{Title: "Alien", Folder: "Alien", Copies: []Copy{part("Alien")}}}, nil); err != nil {
		t.Fatal(err)
	}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Show", Folder: "Show/Season 1", ByNumber: true, Copies: []Copy{part("Show1")}}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	film := oneItem(t, s, "kind = 'movie'")
	show := oneItem(t, s, "kind = 'show'")
	weaver := func(photo string) domain.Credit {
		return domain.Credit{Name: "Sigourney Weaver", IDs: map[domain.Provider]string{domain.ProviderTMDB: "10205"}, Photo: photo, Kind: domain.CreditActor, Role: "Ripley"}
	}
	if err := s.SaveIdentity(ctx, film.ID, domain.SourceTMDB, domain.Metadata{Title: "Alien", Credits: []domain.Credit{
		weaver("https://image.tmdb.org/t/p/original/sw.jpg"),
		{Name: "Ridley Scott", IDs: map[domain.Provider]string{domain.ProviderTMDB: "578"}, Kind: domain.CreditDirector, Role: "Director"},
		{Name: "Nobody", Kind: domain.CreditActor, Role: "Unknown"},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	// She guest stars in the show's first episode.
	guest := weaver("https://image.tmdb.org/t/p/original/sw.jpg")
	guest.Kind, guest.Role = domain.CreditGuestStar, "Herself"
	if err := s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, domain.Metadata{Title: "Show"},
		map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: "Pilot", Credits: []domain.Credit{guest}}}}}); err != nil {
		t.Fatal(err)
	}

	page, err := s.Title(ctx, uuid.UUID{}, film.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Credits) != 2 || page.Credits[0].Name != "Sigourney Weaver" || page.Credits[0].Role != "Ripley" || page.Credits[1].Kind != domain.CreditDirector || page.Credits[0].Photo == (uuid.UUID{}) {
		t.Fatalf("credits = %+v; want her, with a picture, then the director, and no one without an id", page.Credits)
	}
	her := page.Credits[0].PersonID
	if pic, err := s.Picture(ctx, page.Credits[0].Photo); err != nil || pic.URL != "https://image.tmdb.org/t/p/original/sw.jpg" {
		t.Errorf("her picture = %+v, %v", pic, err)
	}
	work, err := s.PersonCredits(ctx, uuid.UUID{}, her)
	if err != nil || len(work) != 2 {
		t.Fatalf("her work = %+v, %v; want the film and the show", work, err)
	}
	for _, w := range work {
		if w.Card.Title == "Show" && (w.Kind != domain.CreditGuestStar || w.Role != "Herself") {
			t.Errorf("her credit on the show = %+v, want the episode's guest part", w)
		}
	}
	if err := s.DescribePerson(ctx, her, domain.Person{Name: "Sigourney Weaver", Biography: "An actor.", Born: time.Date(1949, 10, 8, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Person(ctx, her)
	if err != nil || p.Biography != "An actor." || !time.Time(p.Born).Equal(time.Date(1949, 10, 8, 0, 0, 0, 0, time.UTC)) || p.DescribedAt.IsZero() || p.IDs[domain.ProviderTMDB] != "10205" {
		t.Errorf("her page = %+v, %v", p, err)
	}
	if _, err := s.Person(ctx, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("no one: %v, want ErrNotFound", err)
	}
	for _, q := range []string{"weav", "SIGOURNEY w", "Sigourney Weaver"} {
		if found, _, err := s.SearchPeople(ctx, q, 0, 10); err != nil || len(found) != 1 || found[0].ID != her || found[0].Photo == (uuid.UUID{}) {
			t.Errorf("search %q: %+v, %v; want her", q, found, err)
		}
	}
	if found, total, _ := s.SearchPeople(ctx, "gourney", 0, 10); len(found) != 0 || total != 0 {
		t.Errorf("search inside a word: %+v, want no one", found)
	}
	cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Limit: 10, Filter: WallFilter{People: []uuid.UUID{her}}})
	if err != nil || len(cards) != 2 {
		t.Errorf("titles she is in: %+v, %v; want the film and the show", cards, err)
	}
}

func TestAPersonIsKnownByAnyProvidersID(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlugin(ctx, Plugin{Slug: "films", URL: "http://films.test", Manifest: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceTMDB, domain.PluginSource("films"))}); err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("Heat"), Parts: []Part{{RelPath: "Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, _ := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Limit: 1})
	heat := cards[0].ID
	plugin := domain.Provider(domain.PluginSource("films"))
	credit := func(name string, ids map[domain.Provider]string) domain.Credit {
		return domain.Credit{Name: name, IDs: ids, Kind: domain.CreditActor, Role: name}
	}
	// A plugin credits someone TMDB does not know, and someone it knows by their IMDb id.
	if err := s.SaveIdentity(ctx, heat, domain.PluginSource("films"), domain.Metadata{Title: "Heat", Credits: []domain.Credit{
		credit("Extra", map[domain.Provider]string{plugin: "extra"}),
		credit("Al Pacino", map[domain.Provider]string{domain.ProviderIMDb: "nm0000199", plugin: "pacino"}),
		credit("Val Kilmer", map[domain.Provider]string{plugin: "kilmer"}),
	}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, heat)
	if err != nil || len(page.Credits) != 3 || page.Credits[0].Name != "Extra" {
		t.Fatalf("credits = %+v, %v; want the plugin's three", page.Credits, err)
	}
	extra := page.Credits[0].PersonID
	if p, err := s.Person(ctx, extra); err != nil || p.Name != "Extra" || p.IDs[plugin] != "extra" || len(p.IDs) != 1 {
		t.Errorf("his page = %+v, %v; want him by the plugin's id", p, err)
	}
	if work, err := s.PersonCredits(ctx, uuid.UUID{}, extra); err != nil || len(work) != 1 || work[0].Card.Title != "Heat" {
		t.Errorf("his work = %+v, %v; want Heat", work, err)
	}
	// TMDB credits Pacino by the IMDb id the plugin gave, and Kilmer by its own id alone; then the
	// plugin learns Kilmer's TMDB id, and the two Kilmers are one.
	if err := s.SaveIdentity(ctx, heat, domain.SourceTMDB, domain.Metadata{Title: "Heat", Credits: []domain.Credit{
		credit("Al Pacino", map[domain.Provider]string{domain.ProviderTMDB: "1158", domain.ProviderIMDb: "nm0000199"}),
		credit("Val Kilmer", map[domain.Provider]string{domain.ProviderTMDB: "5576"}),
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, heat, domain.PluginSource("films"), domain.Metadata{Title: "Heat", Credits: []domain.Credit{
		credit("Al Pacino", map[domain.Provider]string{domain.ProviderIMDb: "nm0000199", plugin: "pacino"}),
		credit("Val Kilmer", map[domain.Provider]string{domain.ProviderTMDB: "5576", plugin: "kilmer"}),
	}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err = s.Title(ctx, uuid.UUID{}, heat)
	if err != nil || len(page.Credits) != 2 {
		t.Fatalf("credits = %+v, %v", page.Credits, err)
	}
	want := map[string]map[domain.Provider]string{
		"Al Pacino":  {domain.ProviderTMDB: "1158", domain.ProviderIMDb: "nm0000199", plugin: "pacino"},
		"Val Kilmer": {domain.ProviderTMDB: "5576", plugin: "kilmer"},
	}
	for _, c := range page.Credits {
		p, err := s.Person(ctx, c.PersonID)
		if err != nil || !maps.Equal(p.IDs, want[c.Name]) {
			t.Errorf("%s = %+v, %v; want one person with ids %v", c.Name, p, err, want[c.Name])
		}
		if found, _, _ := s.SearchPeople(ctx, c.Name, 0, 10); len(found) != 1 {
			t.Errorf("search %q: %+v, want one person", c.Name, found)
		}
	}
}

func TestSimilarTitlesShareSomething(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	mann := domain.Credit{Name: "Michael Mann", IDs: map[domain.Provider]string{domain.ProviderTMDB: "638"}, Kind: domain.CreditDirector, Role: "Director"}
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		title   string
		genres  []string
		credits []domain.Credit
	}{
		{"Heat", []string{"Crime", "Thriller"}, []domain.Credit{mann}},
		{"Thief", []string{"Crime"}, []domain.Credit{mann}},
		{"Ronin", []string{"Thriller"}, nil},
		{"Amélie", []string{"Comedy"}, nil},
	} {
		film := Film{Title: f.title, Folder: f.title, Copies: []Copy{{ContentKey: []byte(f.title), Parts: []Part{{RelPath: f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
		if _, err := s.SaveFolder(ctx, lib.ID, f.title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, _ := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		ids[f.title] = cards[0].ID
		if err := s.SaveIdentity(ctx, cards[0].ID, domain.SourceTMDB, domain.Metadata{Title: f.title, Genres: f.genres, Credits: f.credits}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Similar(ctx, uuid.UUID{}, ids["Heat"])
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range got {
		titles = append(titles, c.Title)
	}
	if want := []string{"Thief", "Ronin"}; !slices.Equal(titles, want) {
		t.Errorf("like Heat: %q, want %q: its director and a genre, then a genre, and nothing that shares none", titles, want)
	}
	// Only the first three genres count: a fourth shared is no likeness.
	if err := s.SaveIdentity(ctx, ids["Amélie"], domain.SourceTMDB, domain.Metadata{Title: "Amélie", Genres: []string{"Comedy"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, ids["Heat"], domain.SourceTMDB, domain.Metadata{Title: "Heat", Genres: []string{"Crime", "Thriller", "Drama", "Comedy"}, Credits: []domain.Credit{mann}}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Similar(ctx, uuid.UUID{}, ids["Heat"]); slices.ContainsFunc(got, func(c Card) bool { return c.Title == "Amélie" }) {
		t.Errorf("a fourth genre counted: %+v", got)
	}
}

func TestTwoMatchesCreditingSomeoneNewAtOnceShareThem(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	var shows []uuid.UUID
	for _, name := range []string{"Andor", "Ahsoka"} {
		episode := Episode{
			Season: 1, Episodes: []int{1}, Title: name, Folder: name + "/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
		}
		if _, err := s.SaveShowFolder(ctx, lib.ID, name+"/Season 1", []byte("v1"), Show{Title: name, Folder: name}, []Episode{episode}, nil); err != nil {
			t.Fatal(err)
		}
		shows = append(shows, oneItem(t, s, "kind = 'show' AND title = $1", name).ID)
	}
	for round := range 3 {
		// The same thirty people, new to the server, cast in both shows and guests in their episodes.
		var cast []domain.Credit
		for n := range 30 {
			cast = append(cast, domain.Credit{
				Name: fmt.Sprint("Actor ", n), Kind: domain.CreditActor, Role: "Rebel",
				IDs: map[domain.Provider]string{domain.ProviderTMDB: fmt.Sprint(round, "-", n)},
			})
		}
		start := make(chan struct{})
		errs := make(chan error, len(shows))
		for _, show := range shows {
			go func() {
				<-start
				errs <- s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{Credits: cast},
					map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Credits: cast[:5]}}}})
			}()
		}
		close(start)
		for range shows {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		var credited [][]uuid.UUID
		for _, show := range shows {
			page, err := s.Title(ctx, uuid.UUID{}, show)
			if err != nil {
				t.Fatal(err)
			}
			var people []uuid.UUID
			for _, c := range page.Credits {
				people = append(people, c.PersonID)
			}
			credited = append(credited, people)
		}
		if len(credited[0]) != len(cast) || !slices.Equal(credited[0], credited[1]) {
			t.Errorf("round %d: credited %v and %v, want the same %d people on both", round, credited[0], credited[1], len(cast))
		}
		if found, _, _ := s.SearchPeople(ctx, "Actor 7", 0, 10); len(found) != round+1 {
			t.Errorf("round %d: %d people named Actor 7, want one a round", round, len(found))
		}
	}
}
