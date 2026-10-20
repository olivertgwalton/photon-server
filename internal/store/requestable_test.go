//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A title to ask for is drawn as a title's page: from what its search said until it is described,
// then from its description, its backdrop, lettering and people's photos kept as pictures the
// server answers, and its people the server's own where it knows them by the same ids. What a
// profile may not ask for has no page.
func TestATitleToAskForIsDrawnAsATitlesPage(t *testing.T) {
	s, _, film, _ := askable(t)
	ctx := t.Context()
	user := addProfile(t, s, "Sam", domain.RoleUser, nil)

	page, err := s.RequestablePage(ctx, user.ID, film.ID)
	if err != nil || page.Title != "Inception" || page.Year != 2010 || page.Kind != domain.ItemMovie || page.IDs[domain.ProviderTMDB] != "27205" {
		t.Fatalf("before it is described: %+v, %v", page, err)
	}
	if a, err := s.Askable(ctx, user.ID, film.ID); err != nil || a.Described || a.Value != "27205" {
		t.Errorf("askable %+v, %v; want it not described yet", a, err)
	}

	var known uuid.UUID
	if err := s.pool.QueryRow(ctx, `INSERT INTO people (name) VALUES ('Marion Cotillard') RETURNING id`).Scan(&known); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO person_ids (person_id, provider, value) VALUES ($1, 'tmdb', '8293')`, known); err != nil {
		t.Fatal(err)
	}
	m := domain.Metadata{
		Title: "Inception", Overview: "A thief who steals secrets.", Tagline: "Your mind is the scene of the crime.",
		Certificate: "GB:12A", Year: 2010, ReleaseDate: time.Date(2010, 7, 16, 0, 0, 0, 0, time.UTC), Genres: []string{"Science Fiction"},
		IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt1375666"},
		Artwork: []domain.Artwork{
			{Kind: domain.ArtworkBackdrop, URL: "https://image.tmdb.org/t/p/original/back.jpg"},
			{Kind: domain.ArtworkBackdrop, URL: "https://image.tmdb.org/t/p/original/second.jpg"},
			{Kind: domain.ArtworkLogo, URL: "https://image.tmdb.org/t/p/original/logo.png"},
		},
		Credits: []domain.Credit{
			{Name: "Marion Cotillard", Kind: domain.CreditActor, Role: "Mal", IDs: map[domain.Provider]string{domain.ProviderTMDB: "8293"}},
			{
				Name: "Elliot Page", Kind: domain.CreditActor, Role: "Ariadne", Photo: "https://image.tmdb.org/t/p/original/page.jpg",
				IDs: map[domain.Provider]string{domain.ProviderTMDB: "27578"},
			},
		},
		Videos: []domain.RemoteVideo{{Kind: domain.ExtraTrailer, Site: "YouTube", Key: "YoHD9XEInc0", Name: "Trailer"}},
	}
	if err := s.KeepDescription(ctx, film.ID, m); err != nil {
		t.Fatal(err)
	}
	if a, err := s.Askable(ctx, user.ID, film.ID); err != nil || !a.Described {
		t.Errorf("askable %+v, %v; want it described", a, err)
	}
	page, err = s.RequestablePage(ctx, user.ID, film.ID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Tagline != m.Tagline || page.Overview != m.Overview || page.Certificate != "GB:12A" || page.IDs[domain.ProviderIMDb] != "tt1375666" ||
		page.IDs[domain.ProviderTMDB] != "27205" || len(page.Videos) != 1 || page.Videos[0].Key != "YoHD9XEInc0" {
		t.Errorf("described: %+v", page)
	}
	for kind, want := range map[domain.ArtworkKind]string{
		domain.ArtworkBackdrop: "https://image.tmdb.org/t/p/original/back.jpg",
		domain.ArtworkLogo:     "https://image.tmdb.org/t/p/original/logo.png",
	} {
		ids := page.Artwork[kind]
		if len(ids) != 1 {
			t.Errorf("%s: %v, want the first", kind, ids)
			continue
		}
		if pic, err := s.Picture(ctx, ids[0]); err != nil || pic.URL != want {
			t.Errorf("%s answers %+v, %v; want %s", kind, pic, err, want)
		}
	}
	if len(page.Credits) != 2 || page.Credits[0].PersonID != known || page.Credits[1].PersonID != (uuid.UUID{}) {
		t.Fatalf("credits %+v; want Cotillard as the server knows her, and Page as no one to open", page.Credits)
	}
	if pic, err := s.Picture(ctx, page.Credits[1].Photo); err != nil || pic.URL != "https://image.tmdb.org/t/p/original/page.jpg" {
		t.Errorf("Page's photo answers %+v, %v", pic, err)
	}

	kid := addProfile(t, s, "Kid", domain.RoleUser, nil)
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestablePage(ctx, kid.ID, film.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a profile held to 12: %v, want ErrNotFound", err)
	}
	if _, err := s.RequestablePage(ctx, user.ID, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such find: %v, want ErrNotFound", err)
	}
}
