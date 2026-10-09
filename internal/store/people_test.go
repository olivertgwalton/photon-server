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

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAPersonIsCreditedOnceAcrossTitles(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
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
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, []Episode{episode}, nil); err != nil {
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
	found, total, err := s.SearchPeople(ctx, "gourney", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 || total != 0 {
		t.Errorf("search inside a word: %+v, want no one", found)
	}
	for _, l := range []uuid.UUID{lib.ID, shows.ID} {
		cards, _, err := s.Wall(ctx, []uuid.UUID{l}, WallPage{Sort: domain.SortTitle, Limit: 10, Filter: WallFilter{People: []uuid.UUID{her}}})
		if err != nil || len(cards) != 1 {
			t.Errorf("titles she is in: %+v, %v; want the film and the show", cards, err)
		}
	}
}

func TestAnEpisodeIsBilledWithItsShowsCast(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	copies := []Copy{{ContentKey: []byte("e1"), Parts: []Part{{RelPath: "e1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Lucifer", Folder: "Lucifer/Season 1", ByNumber: true, Copies: copies}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Lucifer/Season 1", []byte("v1"), Show{Title: "Lucifer", Folder: "Lucifer"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, "kind = 'show'")
	person := func(name, id string, kind domain.CreditKind, role string) domain.Credit {
		return domain.Credit{Name: name, IDs: map[domain.Provider]string{domain.ProviderTMDB: id}, Kind: kind, Role: role}
	}
	ellis := person("Tom Ellis", "1", domain.CreditActor, "Lucifer Morningstar")
	german := person("Lauren German", "2", domain.CreditActor, "Chloe Decker")
	// A provider credits an episode with its guests and crew, and sometimes a regular as a guest.
	if err := s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, domain.Metadata{Title: "Lucifer", Credits: []domain.Credit{ellis, german}},
		map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: "Pilot", Credits: []domain.Credit{
			person("Russell Wong", "3", domain.CreditGuestStar, "Vincent Green"),
			person("Tom Ellis", "1", domain.CreditGuestStar, "Lucifer"),
			person("Len Wiseman", "4", domain.CreditDirector, "Director"),
		}}}}}); err != nil {
		t.Fatal(err)
	}

	names := func(credits []CreditRef) []string {
		var out []string
		for _, c := range credits {
			out = append(out, c.Name)
		}
		return out
	}
	page, err := s.Title(ctx, uuid.UUID{}, oneItem(t, s, "kind = 'episode'").ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(page.Credits), []string{"Tom Ellis", "Lauren German", "Russell Wong", "Len Wiseman"}; !slices.Equal(got, want) {
		t.Errorf("episode's credits = %v, want the show's cast, then its guest and crew, no one twice", got)
	}
	if page.Credits[0].Role != "Lucifer Morningstar" {
		t.Errorf("a regular's part = %q, want the show's", page.Credits[0].Role)
	}
	season, err := s.Title(ctx, uuid.UUID{}, oneItem(t, s, "kind = 'season'").ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(season.Credits), []string{"Tom Ellis", "Lauren German"}; !slices.Equal(got, want) {
		t.Errorf("season's credits = %v, want the show's cast", got)
	}
}

func TestTwoSourcesCreditingOneShowNameOneCast(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	// TVDB first, as a library that prefers it has it.
	if err := s.SetLibrary(ctx, shows.ID, LibraryChange{Sources: metadataFrom(domain.LibraryShows, domain.SourceTVDB, domain.SourceTMDB)}); err != nil {
		t.Fatal(err)
	}
	copies := []Copy{{ContentKey: []byte("e1"), Parts: []Part{{RelPath: "e1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Lucifer", Folder: "Lucifer/Season 1", ByNumber: true, Copies: copies}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Lucifer/Season 1", []byte("v1"), Show{Title: "Lucifer", Folder: "Lucifer"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, "kind = 'show'")
	by := func(p domain.Provider) func(name, id string, kind domain.CreditKind) domain.Credit {
		return func(name, id string, kind domain.CreditKind) domain.Credit {
			return domain.Credit{Name: name, IDs: map[domain.Provider]string{p: id}, Kind: kind}
		}
	}
	tmdb, tvdb := by(domain.ProviderTMDB), by(domain.ProviderTVDB)
	if err := s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, domain.Metadata{Title: "Lucifer", Credits: []domain.Credit{
		tmdb("Tom Ellis", "1", domain.CreditActor),
		// Two people of one name on one show: TVDB's is neither, as far as can be told.
		tmdb("John Smith", "10", domain.CreditActor),
		tmdb("John Smith", "11", domain.CreditActor),
	}}, map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Credits: []domain.Credit{
		tmdb("Russell Wong", "3", domain.CreditGuestStar),
	}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, show.ID, domain.SourceTVDB, domain.Metadata{Title: "Lucifer", Credits: []domain.Credit{
		tvdb("tom ellis", "400436", domain.CreditActor),
		tvdb("John Smith", "900", domain.CreditActor),
		tvdb("Lauren German", "371248", domain.CreditActor),
	}}, map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Credits: []domain.Credit{
		tvdb("Russell Wong", "503", domain.CreditGuestStar),
	}}}}}); err != nil {
		t.Fatal(err)
	}

	people := func(name string) map[uuid.UUID][]string {
		rows, err := s.pool.Query(ctx, `
			SELECT p.id, i.provider || ':' || i.value FROM people p JOIN person_ids i ON i.person_id = p.id
			WHERE lower(p.name) = lower($1) ORDER BY 2`, name)
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID][]string{}
		var id uuid.UUID
		var key string
		if _, err := pgx.ForEachRow(rows, []any{&id, &key}, func() error {
			out[id] = append(out[id], key)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for name, want := range map[string][]string{
		"Tom Ellis":    {"tmdb:1", "tvdb:400436"},
		"Russell Wong": {"tmdb:3", "tvdb:503"},
	} {
		got := people(name)
		if len(got) != 1 || !slices.Equal(slices.Collect(maps.Values(got))[0], want) {
			t.Errorf("%s = %v, want one person known by %v", name, got, want)
		}
	}
	if got := people("John Smith"); len(got) != 3 {
		t.Errorf("John Smith = %v, want three people: TVDB's is not taken for either of TMDB's", got)
	}
}

func TestAPersonIsKnownByAnyProvidersID(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlugin(ctx, Plugin{Slug: "films", Protocol: domain.PluginPhoton, URL: "http://films.test", Manifest: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceTMDB, domain.PluginSource("films"))}); err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("Heat"), Parts: []Part{{RelPath: "Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
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
		found, _, err := s.SearchPeople(ctx, c.Name, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 {
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
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
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
	got, err = s.Similar(ctx, uuid.UUID{}, ids["Heat"])
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(got, func(c Card) bool { return c.Title == "Amélie" }) {
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
		found, _, err := s.SearchPeople(ctx, "Actor 7", 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != round+1 {
			t.Errorf("round %d: %d people named Actor 7, want one a round", round, len(found))
		}
	}
}

func TestSomeoneIsDescribedInTheLanguageTheirTitlesShare(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	german := "de-DE"
	film := func(library, title string) uuid.UUID {
		t.Helper()
		lib, err := s.AddLibrary(ctx, library, domain.LibraryMovies, "/srv/"+library)
		if err != nil {
			t.Fatal(err)
		}
		if library == "Filme" {
			if err := s.SetLibrary(ctx, lib.ID, LibraryChange{MetadataLanguage: &german}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{{Title: title, Folder: title}}, nil); err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'movie' AND library_id = $1`, lib.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: title, Credits: []domain.Credit{
			{Name: "Bruno Ganz", IDs: map[domain.Provider]string{domain.ProviderTMDB: "2310"}, Kind: domain.CreditActor},
		}}, nil); err != nil {
			t.Fatal(err)
		}
		return lib.ID
	}
	filme := film("Filme", "Der Himmel über Berlin")
	ganz := oneItem(t, s, "true").ID
	if err := s.pool.QueryRow(ctx, `SELECT person_id FROM credits LIMIT 1`).Scan(&ganz); err != nil {
		t.Fatal(err)
	}
	if err := s.DescribePerson(ctx, ganz, domain.Person{Name: "Bruno Ganz", Biography: "Ein Schauspieler."}); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Person(ctx, ganz); err != nil || p.Language != "de-DE" || p.DescribedAt.IsZero() {
		t.Errorf("credited in a German library alone: %q, %v; want him described in German", p.Language, err)
	}

	// Asking the library in another language has him described again.
	french := "fr-FR"
	if err := s.SetLibrary(ctx, filme, LibraryChange{MetadataLanguage: &french}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Person(ctx, ganz)
	if err != nil {
		t.Fatal(err)
	}
	if !p.DescribedAt.IsZero() || p.Language != "fr-FR" {
		t.Errorf("after the library asks in French: described at %v, in %q; want him to be described again, in French", p.DescribedAt, p.Language)
	}

	film("Films", "Downfall")
	p, err = s.Person(ctx, ganz)
	if err != nil {
		t.Fatal(err)
	}
	if p.Language != "" {
		t.Errorf("credited in a French and a British library: %q, want the server's own", p.Language)
	}
}
