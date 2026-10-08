package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// CreditRef is someone's part in a title, with their picture's id.
type CreditRef struct {
	PersonID uuid.UUID
	Name     string
	Kind     domain.CreditKind
	Role     string
	Photo    uuid.UUID
	// Blurhashes holds the photo's BlurHash, where it has one.
	Blurhashes Blurhashes
}

type creditRow struct {
	Source   domain.FieldSource
	PersonID uuid.UUID
	Name     string
	Kind     domain.CreditKind
	Role     string
	PhotoID  *uuid.UUID
	Blurhash *string
}

// credits answers a title's cast and crew as the highest-ranked source with any gives them.
func (s *Store) credits(ctx context.Context, item uuid.UUID) ([]CreditRef, error) {
	ranked, err := ranks(ctx, s.pool, item)
	if err != nil {
		return nil, err
	}
	rows, err := queryStructs[creditRow](ctx, s.pool, `
		SELECT c.source, c.person_id, p.name, c.kind, c.role, p.photo_id, p.photo_blurhash AS blurhash FROM credits c
		JOIN people p ON p.id = c.person_id WHERE c.item_id = $1 ORDER BY c.position`, item)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	best := rows[0].Source
	for _, r := range rows {
		if ranked[r.Source] > ranked[best] {
			best = r.Source
		}
	}
	var out []CreditRef
	for _, r := range rows {
		if r.Source != best {
			continue
		}
		ref := CreditRef{PersonID: r.PersonID, Name: r.Name, Kind: r.Kind, Role: r.Role}
		ref.Photo, ref.Blurhashes = photo(r.PhotoID, r.Blurhash)
		out = append(out, ref)
	}
	return out, nil
}

// billed is a season's or an episode's credits under its show's, as Plex and Jellyfin bill one:
// the show's cast first, since a provider credits an episode with its guests alone, then the
// episode's own, less anyone the show's cast already names.
func billed(show, own []CreditRef) []CreditRef {
	cast := map[uuid.UUID]bool{}
	var out []CreditRef
	for _, c := range show {
		if c.Kind == domain.CreditActor {
			cast[c.PersonID] = true
			out = append(out, c)
		}
	}
	for _, c := range own {
		if !c.Kind.Acting() || !cast[c.PersonID] {
			out = append(out, c)
		}
	}
	return out
}

// PersonPage is someone as their page shows them: what is known of them and their work here.
type PersonPage struct {
	ID         uuid.UUID
	Name       string
	Photo      uuid.UUID
	Blurhashes Blurhashes
	Biography  string
	Born       domain.Date
	Died       domain.Date
	Birthplace string
	IDs        map[domain.Provider]string
	// DescribedAt is when a provider last said who they are, zero for never.
	DescribedAt time.Time
	// Language is what they are described in: the one every film and show they are credited on
	// asks in, its own or its library's, where they all ask in one; else "", the server's.
	Language string
}

// PersonCredit is a title someone is credited on, as a card, and what they did on it; credits on
// episodes are the show's.
type PersonCredit struct {
	Card Card
	Kind domain.CreditKind
	Role string
}

// personColumns are model.Person's, for a statement that reads whole people.
const personColumns = `id, name, photo_url, photo_id, photo_blurhash, biography, born, died, birthplace, described_at`

// Person answers someone's page, or ErrNotFound.
func (s *Store) Person(ctx context.Context, id uuid.UUID) (PersonPage, error) {
	row, err := readRow[model.Person](ctx, s.pool, `SELECT `+personColumns+` FROM people WHERE id = $1`, id)
	if err != nil {
		return PersonPage{}, err
	}
	out := PersonPage{
		ID: id, Name: row.Name, Biography: deref(row.Biography),
		Born: domain.Date(deref(row.Born)), Died: domain.Date(deref(row.Died)),
		Birthplace: deref(row.Birthplace), DescribedAt: deref(row.DescribedAt),
	}
	out.Photo, out.Blurhashes = photo(row.PhotoID, row.PhotoBlurhash)
	out.IDs, err = queryMap[domain.Provider, string](ctx, s.pool, `SELECT provider, value FROM person_ids WHERE person_id = $1`, id)
	if err != nil {
		return PersonPage{}, err
	}
	asked, err := queryColumn[string](ctx, s.pool, `
		WITH RECURSIVE credited AS (
			SELECT i.id, i.parent_id, i.kind, i.library_id, i.metadata_language
			FROM credits c JOIN items i ON i.id = c.item_id WHERE c.person_id = $1
			UNION
			SELECT p.id, p.parent_id, p.kind, p.library_id, p.metadata_language FROM items p JOIN credited ON p.id = credited.parent_id
		)
		SELECT DISTINCT coalesce(t.metadata_language, l.metadata_language, '') FROM credited t
		JOIN libraries l ON l.id = t.library_id WHERE t.kind IN ('movie', 'show')`, id)
	if len(asked) == 1 {
		out.Language = asked[0]
	}
	return out, err
}

// forgetDescriptions has everyone credited on the titles under which (and their seasons and
// episodes) described again when next their page is opened, as those titles now ask in another
// language.
func forgetDescriptions(ctx context.Context, tx db, which string, args pgx.NamedArgs) error {
	_, err := tx.Exec(ctx, `
		WITH RECURSIVE under AS (`+which+`
			UNION SELECT i.id FROM items i JOIN under u ON i.parent_id = u.id)
		UPDATE people SET described_at = NULL
		WHERE id IN (SELECT c.person_id FROM credits c WHERE c.item_id IN (SELECT id FROM under))`, args)
	return err
}

// DescribePerson records what a provider says of someone.
func (s *Store) DescribePerson(ctx context.Context, id uuid.UUID, d domain.Person) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := readRow[model.Person](ctx, tx, `SELECT `+personColumns+` FROM people WHERE id = $1`, id)
		if err != nil {
			return err
		}
		now := time.Now()
		row.Biography, row.Birthplace, row.DescribedAt = optional(d.Biography), optional(d.Birthplace), &now
		row.Born, row.Died = optionalTime(d.Born), optionalTime(d.Died)
		setPhoto(&row, d.Photo)
		_, err = tx.Exec(ctx, `
			UPDATE people SET photo_url = $2, photo_id = $3, photo_blurhash = $4, biography = $5, born = $6, died = $7,
				birthplace = $8, described_at = $9
			WHERE id = $1`,
			id, row.PhotoURL, row.PhotoID, row.PhotoBlurhash, row.Biography, row.Born, row.Died, row.Birthplace, row.DescribedAt)
		return err
	})
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

type creditLink struct {
	ItemID uuid.UUID
	Kind   domain.CreditKind
	Role   string
}

// PersonCredits answers the films and shows someone is credited on, the newest first.
func (s *Store) PersonCredits(ctx context.Context, profile, person uuid.UUID) ([]PersonCredit, error) {
	// An episode's credit is its show's; a person in many episodes is listed once per part.
	links, err := queryStructs[creditLink](ctx, s.pool, `
		SELECT DISTINCT ON (t.id, c.kind) t.id AS item_id, c.kind, c.role FROM credits c
		JOIN items i ON i.id = c.item_id
		JOIN items t ON t.id = CASE i.kind WHEN 'episode' THEN (SELECT s.parent_id FROM items s WHERE s.id = i.parent_id) ELSE i.id END
		WHERE c.person_id = $1 AND t.kind IN ('movie', 'show')
			AND EXISTS (SELECT 1 FROM viewer($2) v WHERE sees(v, t) AND first_of_title(v, t))
		ORDER BY t.id, c.kind, c.position`, person, profile)
	if err != nil || len(links) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(links))
	for n, l := range links {
		ids[n] = l.ItemID
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id = ANY($1) ORDER BY released_desc DESC, id`, ids)
	if err != nil {
		return nil, err
	}
	cards, err := s.cards(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	var out []PersonCredit
	for _, c := range cards {
		for _, l := range links {
			if l.ItemID == c.ID {
				out = append(out, PersonCredit{Card: c, Kind: l.Kind, Role: l.Role})
			}
		}
	}
	return out, nil
}
