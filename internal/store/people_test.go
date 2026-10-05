//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestAPersonIsCreditedOnceAcrossTitles(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Mixed", domain.LibraryMovies, "/srv/mixed")
	if err != nil {
		t.Fatal(err)
	}
	part := func(name string) Copy {
		return Copy{ContentKey: []byte(name), Parts: []Part{{RelPath: name + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{}}}}
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Alien", []byte("v1"), []Film{{Title: "Alien", Folder: "Alien", Copies: []Copy{part("Alien")}}}, nil); err != nil {
		t.Fatal(err)
	}
	episode := Episode{Season: 1, Episodes: []int{1}, Title: "Show", Folder: "Show/Season 1", ByNumber: true, Copies: []Copy{part("Show1")}}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Show/Season 1", []byte("v1"), Show{Title: "Show", Folder: "Show"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	i := s.q.Item
	film, _ := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemMovie))).Take()
	show, _ := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemShow))).Take()
	weaver := func(photo string) domain.Credit {
		return domain.Credit{Name: "Sigourney Weaver", IDs: map[domain.Provider]string{domain.ProviderTMDB: "10205"}, Photo: photo, Kind: domain.CreditActor, Role: "Ripley"}
	}
	if err := s.SaveIdentity(ctx, uuid.UUID(film.ID), domain.SourceTMDB, domain.Metadata{Title: "Alien", Credits: []domain.Credit{
		weaver("https://image.tmdb.org/t/p/original/sw.jpg"),
		{Name: "Ridley Scott", IDs: map[domain.Provider]string{domain.ProviderTMDB: "578"}, Kind: domain.CreditDirector, Role: "Director"},
		{Name: "Nobody", Kind: domain.CreditActor, Role: "Unknown"},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	// She guest stars in the show's first episode.
	guest := weaver("https://image.tmdb.org/t/p/original/sw.jpg")
	guest.Kind, guest.Role = domain.CreditGuestStar, "Herself"
	if err := s.SaveIdentity(ctx, uuid.UUID(show.ID), domain.SourceTMDB, domain.Metadata{Title: "Show"},
		map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: "Pilot", Credits: []domain.Credit{guest}}}}}); err != nil {
		t.Fatal(err)
	}

	page, err := s.Title(ctx, uuid.UUID{}, uuid.UUID(film.ID))
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
	cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Limit: 10, Filter: WallFilter{People: []uuid.UUID{her}}})
	if err != nil || len(cards) != 2 {
		t.Errorf("titles she is in: %+v, %v; want the film and the show", cards, err)
	}
}
