package store

import (
	"cmp"
	"context"
	"encoding/json"
	"iter"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Askable is a title a library of files' search found that a profile may ask for, and what
// describing it takes: the provider's id it was found by, and whether it is described yet.
type Askable struct {
	ID        uuid.UUID
	Library   uuid.UUID
	Kind      domain.ItemKind
	Value     string
	Described bool
}

// mayAsk is the finds a profile may ask for: of a library of files it may see that searches, and
// none for a profile held to an age, which searches nothing.
const mayAsk = `FROM discoveries d JOIN libraries l ON l.id = d.library_id, viewer(@profile) v
	WHERE d.id = @id AND l.media = 'folder' AND l.discover_source IS NOT NULL AND v.max_age IS NULL
		AND (v.libraries IS NULL OR l.id = ANY (v.libraries))`

// Askable answers a find a profile may ask for, or ErrNotFound.
func (s *Store) Askable(ctx context.Context, profile, id uuid.UUID) (Askable, error) {
	var a Askable
	err := s.pool.QueryRow(ctx, `SELECT d.id, d.library_id, d.kind, d.value, d.description IS NOT NULL `+mayAsk,
		pgx.NamedArgs{"profile": profile, "id": id}).Scan(&a.ID, &a.Library, &a.Kind, &a.Value, &a.Described)
	return a, found(err)
}

// KeepDescription keeps what a provider says of a find, and the pictures it names under ids of
// their addresses, for as long as the find is kept. ErrNotFound for a find gone meanwhile.
func (s *Store) KeepDescription(ctx context.Context, id uuid.UUID, m domain.Metadata) error {
	described, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `UPDATE discoveries SET description = $2 WHERE id = $1`, id, described)); err != nil {
			return err
		}
		for _, url := range pictured(m) {
			_, err := tx.Exec(ctx, `
				INSERT INTO discovery_pictures (id, discovery_id, url) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
				picturedAt(id, url), id, url)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// pageArtwork are the kinds of picture a find's page draws, the first of each its provider gives.
var pageArtwork = []domain.ArtworkKind{domain.ArtworkBackdrop, domain.ArtworkLogo}

// pageArt yields the address of each picture of pageArtwork a description gives, by its kind.
func pageArt(m domain.Metadata) iter.Seq2[domain.ArtworkKind, string] {
	return func(yield func(domain.ArtworkKind, string) bool) {
		for _, kind := range pageArtwork {
			i := slices.IndexFunc(m.Artwork, func(a domain.Artwork) bool { return a.Kind == kind && a.URL != "" })
			if i >= 0 && !yield(kind, m.Artwork[i].URL) {
				return
			}
		}
	}
}

// pictured are the addresses of the pictures a find's page draws of a description: its backdrop
// and lettering, and its people's photos.
func pictured(m domain.Metadata) []string {
	var urls []string
	for _, url := range pageArt(m) {
		urls = append(urls, url)
	}
	for _, c := range m.Credits {
		if c.Photo != "" {
			urls = append(urls, c.Photo)
		}
	}
	return urls
}

// picturedAt is the id a find's picture at an address is kept under.
func picturedAt(discovery uuid.UUID, url string) uuid.UUID {
	return derived("picture", discovery, "", url)
}

// RequestablePage answers a find a profile may ask for as a title's page is drawn: from its
// description where it is described, else from what its search said of it. Its people link to
// those the server knows by the same ids. ErrNotFound for no find the profile may ask for.
func (s *Store) RequestablePage(ctx context.Context, profile, id uuid.UUID) (TitlePage, error) {
	var (
		lib                    uuid.UUID
		kind                   domain.ItemKind
		provider               domain.Provider
		value, title           string
		year                   *int
		overview               *string
		poster                 uuid.UUID
		posterURL, description *string
		foundAt                time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT d.library_id, d.kind, d.provider, d.value, d.title, d.year, d.overview, d.poster_id, d.poster_url,
			d.description, d.found_at `+mayAsk, pgx.NamedArgs{"profile": profile, "id": id}).
		Scan(&lib, &kind, &provider, &value, &title, &year, &overview, &poster, &posterURL, &description, &foundAt)
	if err != nil {
		return TitlePage{}, found(err)
	}
	var m domain.Metadata
	if description != nil {
		if err := json.Unmarshal([]byte(*description), &m); err != nil {
			return TitlePage{}, err
		}
	}
	page := TitlePage{
		ID: id, Library: &lib, Kind: kind, Title: cmp.Or(m.Title, title), OriginalTitle: m.OriginalTitle,
		Overview: cmp.Or(m.Overview, deref(overview)), Tagline: m.Tagline, Certificate: m.Certificate,
		Year: cmp.Or(m.Year, deref(year)), ReleaseDate: domain.Date(m.ReleaseDate), Genres: m.Genres, Studios: m.Studios,
		IDs: map[domain.Provider]string{provider: value}, Ratings: m.Ratings, AddedAt: foundAt,
		Artwork: map[domain.ArtworkKind][]uuid.UUID{},
	}
	maps.Copy(page.IDs, m.IDs)
	if posterURL != nil {
		page.Artwork[domain.ArtworkPoster] = []uuid.UUID{poster}
	}
	for kind, url := range pageArt(m) {
		page.Artwork[kind] = []uuid.UUID{picturedAt(id, url)}
	}
	for _, v := range m.Videos {
		page.Videos = append(page.Videos, VideoLink{
			Kind: v.Kind, Site: v.Site, Key: v.Key, Name: v.Name, Language: v.Language, Published: optional(v.Published),
		})
	}
	if page.Credits, err = s.foundCredits(ctx, id, m.Credits); err != nil {
		return TitlePage{}, err
	}
	return page, nil
}

// foundCredits are a description's people, each the server's own where it knows someone by the
// same id, and with no person to open where it does not.
func (s *Store) foundCredits(ctx context.Context, discovery uuid.UUID, credits []domain.Credit) ([]CreditRef, error) {
	if len(credits) == 0 {
		return nil, nil
	}
	var asked []string
	for _, c := range credits {
		if id := c.IDs[domain.ProviderTMDB]; id != "" {
			asked = append(asked, id)
		}
	}
	known, err := queryMap[string, uuid.UUID](ctx, s.pool, `
		SELECT value, person_id FROM person_ids WHERE provider = 'tmdb' AND value = ANY($1)`, asked)
	if err != nil {
		return nil, err
	}
	out := make([]CreditRef, len(credits))
	for i, c := range credits {
		out[i] = CreditRef{PersonID: known[c.IDs[domain.ProviderTMDB]], Name: c.Name, Kind: c.Kind, Role: c.Role}
		if c.Photo != "" {
			out[i].Photo = picturedAt(discovery, c.Photo)
		}
	}
	return out, nil
}
