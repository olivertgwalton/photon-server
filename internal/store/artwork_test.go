//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestPicturesBesideATitleComeBeforeAProvidersAndItsCardShowsTheBest(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "heat", Folder: "Heat", Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Heat/poster.jpg"}},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
			RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	err = s.SaveIdentity(ctx, item, domain.SourceTMDB, domain.Metadata{Artwork: []domain.Artwork{
		{Kind: domain.ArtworkPoster, URL: "https://image.tmdb.org/t/p/original/heat.jpg"},
		{Kind: domain.ArtworkBackdrop, URL: "https://image.tmdb.org/t/p/original/heat-wide.jpg"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, item)
	if err != nil {
		t.Fatal(err)
	}
	posters := page.Artwork[domain.ArtworkPoster]
	if len(posters) != 2 || len(page.Artwork[domain.ArtworkBackdrop]) != 1 {
		t.Fatalf("artwork = %v, want two posters and a backdrop", page.Artwork)
	}
	local, err := s.Picture(ctx, posters[0])
	if err != nil || local.Path != "Heat/poster.jpg" || local.Root != "/srv/films" {
		t.Errorf("best poster = %+v, %v; want the file beside the film", local, err)
	}
	if provider, _ := s.Picture(ctx, posters[1]); provider.URL != "https://image.tmdb.org/t/p/original/heat.jpg" {
		t.Errorf("second poster = %+v, want TMDB's", provider)
	}
	cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Order: domain.Ascending, Limit: 10})
	if err != nil || len(cards) != 1 || cards[0].Poster != posters[0] || cards[0].Backdrop != page.Artwork[domain.ArtworkBackdrop][0] {
		t.Errorf("card = %+v, %v; want the best poster and backdrop", cards, err)
	}
}

func TestOnlyPicturesStillInUseAreLive(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{{Title: "Heat", Folder: "Heat"}}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, item, domain.SourceTMDB, domain.Metadata{
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://image.tmdb.org/t/p/original/p.jpg"}},
		Credits: []domain.Credit{{Name: "Al Pacino", IDs: map[domain.Provider]string{domain.ProviderTMDB: "1158"}, Photo: "https://image.tmdb.org/t/p/original/a.jpg", Kind: domain.CreditActor}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	var poster, photo uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT id FROM artwork), (SELECT photo_id FROM people)`).Scan(&poster, &photo); err != nil {
		t.Fatal(err)
	}
	gone := uuid.NewV7()
	live, err := s.LivePictures(ctx, []uuid.UUID{poster, photo, gone})
	if err != nil || !live[poster] || !live[photo] || live[gone] {
		t.Errorf("live = %v, %v; want the poster and the photo, not the replaced one", live, err)
	}
}

func TestAPictureAnAdminChoseOutranksEverySourceThroughARefresh(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "heat", Folder: "Heat", Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Heat/poster.jpg"}},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	id := item
	match := func(posters ...string) {
		t.Helper()
		var m domain.Metadata
		for _, p := range posters {
			m.Artwork = append(m.Artwork, domain.Artwork{Kind: domain.ArtworkPoster, URL: p, Language: "en", Width: 2000, Height: 3000})
		}
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, m, nil); err != nil {
			t.Fatal(err)
		}
	}
	best := func() (string, int) {
		t.Helper()
		page, err := s.Title(ctx, uuid.UUID{}, id)
		if err != nil {
			t.Fatal(err)
		}
		posters := page.Artwork[domain.ArtworkPoster]
		pic, err := s.Picture(ctx, posters[0])
		if err != nil {
			t.Fatal(err)
		}
		return pic.URL + pic.Path, len(posters)
	}
	match("a.jpg", "b.jpg")
	offered, err := s.ArtworkCandidates(ctx, id, domain.ArtworkPoster)
	if err != nil || len(offered) != 2 || offered[0].Source != domain.SourceTMDB || offered[0].Width != 2000 || offered[0].Chosen {
		t.Fatalf("candidates = %+v, %v; want TMDB's two, the file beside the film left out", offered, err)
	}
	if err := s.ChooseArtwork(ctx, id, domain.ArtworkPoster, offered[1].ID); err != nil {
		t.Fatal(err)
	}
	if got, n := best(); got != "b.jpg" || n != 3 {
		t.Errorf("chosen: best poster %q of %d; want b.jpg over the file, listed once", got, n)
	}
	if offered, _ := s.ArtworkCandidates(ctx, id, domain.ArtworkPoster); !offered[1].Chosen || offered[0].Chosen {
		t.Errorf("candidates = %+v; want b.jpg marked chosen", offered)
	}

	match("c.jpg")
	if got, _ := best(); got != "b.jpg" {
		t.Errorf("after a match that no longer offers it, best poster %q; want the choice to stand", got)
	}
	var file uuid.UUID
	_ = s.pool.QueryRow(ctx, `SELECT id FROM artwork WHERE source = 'file'`).Scan(&file)
	offered, _ = s.ArtworkCandidates(ctx, id, domain.ArtworkPoster)
	for _, pick := range []uuid.UUID{file, uuid.NewV7()} {
		if err := s.ChooseArtwork(ctx, id, domain.ArtworkPoster, pick); !errors.Is(err, ErrNotACandidate) {
			t.Errorf("choosing %v: %v; want ErrNotACandidate", pick, err)
		}
	}
	if err := s.ChooseArtwork(ctx, id, domain.ArtworkBackdrop, offered[0].ID); !errors.Is(err, ErrNotACandidate) {
		t.Errorf("choosing a poster as the backdrop: %v; want ErrNotACandidate", err)
	}
	if err := s.ForgetArtworkChoice(ctx, id, domain.ArtworkPoster); err != nil {
		t.Fatal(err)
	}
	if got, _ := best(); got != "Heat/poster.jpg" {
		t.Errorf("choice forgotten: best poster %q; want the file beside the film again", got)
	}
	if _, err := s.ArtworkCandidates(ctx, uuid.NewV7(), domain.ArtworkPoster); !errors.Is(err, ErrNotFound) {
		t.Errorf("candidates of no title: %v, want ErrNotFound", err)
	}
}

// A title's best picture is the one from the provider its library ranks first for pictures of its
// kind; one it has turned off comes after every one it asks.
func TestALibrarysPictureRankingChoosesItsTitlesBest(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	rank := func(images ...domain.RankedSource) {
		t.Helper()
		change := LibraryChange{Name: "Films", Sources: []domain.KindSources{{Kind: domain.ItemMovie, Metadata: []domain.RankedSource{{Source: domain.SourceTMDB, Enabled: true}}, Images: images}}}
		if err := s.SetLibrary(ctx, lib.ID, change); err != nil {
			t.Fatal(err)
		}
	}
	tmdb, omdb := domain.RankedSource{Source: domain.SourceTMDB, Enabled: true}, domain.RankedSource{Source: domain.SourceOMDb, Enabled: true}
	rank(tmdb, omdb)
	film := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
		RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
	}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item := oneItem(t, s, `kind = 'movie'`).ID
	for source, url := range map[domain.FieldSource]string{domain.SourceTMDB: "https://tmdb.example/heat.jpg", domain.SourceOMDb: "https://omdb.example/heat.jpg"} {
		if err := s.SaveIdentity(ctx, item, source, domain.Metadata{Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: url}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	best := func() string {
		t.Helper()
		cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Order: domain.Ascending, Limit: 1})
		if err != nil || len(cards) != 1 {
			t.Fatalf("wall = %+v, %v", cards, err)
		}
		p, err := s.Picture(ctx, cards[0].Poster)
		if err != nil {
			t.Fatal(err)
		}
		return p.URL
	}

	if got := best(); got != "https://tmdb.example/heat.jpg" {
		t.Errorf("ranking TMDB first, the poster is %q, want TMDB's", got)
	}
	rank(omdb, tmdb)
	if got := best(); got != "https://omdb.example/heat.jpg" {
		t.Errorf("ranking OMDb first, the poster is %q, want OMDb's", got)
	}
	omdb.Enabled = false
	rank(omdb, tmdb)
	if got := best(); got != "https://tmdb.example/heat.jpg" {
		t.Errorf("with OMDb turned off, the poster is %q, want TMDB's", got)
	}
}
