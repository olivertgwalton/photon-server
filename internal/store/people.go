package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"time"
	"uuid"

	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// saveCredits replaces what a source credits on a title, keeping one row per person whatever
// titles credit them. A person is known by any of their ids; one with none is passed over.
func saveCredits(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, credits []domain.Credit) error {
	c := tx.Credit
	if _, err := c.WithContext(ctx).Where(c.ItemID.Eq(item), c.Source.Eq(string(source))).Delete(); err != nil {
		return err
	}
	for n, cr := range credits {
		person, ok, err := personByIDs(ctx, tx, cr)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		// Each row goes in as its person is found, since finding a later one may merge an earlier.
		// A source may credit one person twice for one part: an actor billed as two names of one role.
		row := &model.Credit{ItemID: item, PersonID: person, Source: source, Kind: cr.Kind, Role: cr.Role, Position: n}
		if err := c.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row); err != nil {
			return err
		}
	}
	return nil
}

// personByIDs answers the person a credit names by any of their ids, adding them the first time,
// and false for a credit with none. People found by different ids of one credit are the same
// person and are merged into the first added. The ids a credit brings that its person lacks are
// added, and their name and picture follow the latest credit; a new picture gets a new id.
func personByIDs(ctx context.Context, tx *query.Query, cr domain.Credit) (model.UUID, bool, error) {
	pi := tx.PersonExternalID
	var ids []*model.PersonExternalID
	var match []field.Expr
	for provider, value := range cr.IDs {
		if value != "" {
			ids = append(ids, &model.PersonExternalID{Provider: provider, Value: value})
			match = append(match, field.And(pi.Provider.Eq(string(provider)), pi.Value.Eq(value)))
		}
	}
	if len(ids) == 0 {
		return model.UUID{}, false, nil
	}
	found, err := pi.WithContext(ctx).Where(field.Or(match...)).Order(pi.PersonID).Find()
	if err != nil {
		return model.UUID{}, false, err
	}
	people := make([]model.UUID, 0, len(found))
	for _, f := range found {
		people = append(people, f.PersonID)
	}
	people = slices.Compact(people)
	p := tx.Person
	var row *model.Person
	if len(people) == 0 {
		row = &model.Person{Name: cr.Name}
		setPhoto(row, cr.Photo)
		if err := p.WithContext(ctx).Create(row); err != nil {
			return model.UUID{}, false, err
		}
	} else {
		for _, other := range people[1:] {
			if err := mergePerson(ctx, tx, people[0], other); err != nil {
				return model.UUID{}, false, err
			}
		}
		if row, err = p.WithContext(ctx).Where(p.ID.Eq(people[0])).Take(); err != nil {
			return model.UUID{}, false, err
		}
		if row.Name != cr.Name || deref(row.PhotoURL) != cr.Photo {
			row.Name = cr.Name
			setPhoto(row, cr.Photo)
			if err := p.WithContext(ctx).Save(row); err != nil {
				return model.UUID{}, false, err
			}
		}
	}
	for _, id := range ids {
		id.PersonID = row.ID
	}
	create := pi.WithContext(ctx)
	if len(people) > 0 {
		// A second id for a provider they already have one for is not taken. For someone new, a
		// conflict is another job adding them at once: this one fails rather than keep them twice.
		create = create.Clauses(clause.OnConflict{DoNothing: true})
	}
	return row.ID, true, create.Create(ids...)
}

// mergePerson folds other into into: their credits and the ids into has no id of the provider for.
func mergePerson(ctx context.Context, tx *query.Query, into, other model.UUID) error {
	db := tx.Person.WithContext(ctx).UnderlyingDB()
	args := map[string]any{"into": into, "other": other}
	for _, stmt := range []string{
		`UPDATE person_ids SET person_id = @into WHERE person_id = @other
			AND provider NOT IN (SELECT provider FROM person_ids WHERE person_id = @into)`,
		`UPDATE credits c SET person_id = @into WHERE person_id = @other AND NOT EXISTS (
			SELECT 1 FROM credits k WHERE k.person_id = @into AND (k.item_id, k.source, k.kind, k.role) = (c.item_id, c.source, c.kind, c.role))`,
		`DELETE FROM people WHERE id = @other`,
	} {
		if err := db.Exec(stmt, args).Error; err != nil {
			return err
		}
	}
	return nil
}

func setPhoto(p *model.Person, url string) {
	if url == "" || deref(p.PhotoURL) == url {
		return
	}
	id := model.UUID(uuid.NewV7())
	p.PhotoURL, p.PhotoID = &url, &id
}

// CreditRef is someone's part in a title, with their picture's id.
type CreditRef struct {
	PersonID uuid.UUID         `json:"person_id"`
	Name     string            `json:"name"`
	Kind     domain.CreditKind `json:"kind"`
	Role     string            `json:"role,omitzero"`
	Photo    uuid.UUID         `json:"photo,omitzero"`
}

// credits answers a title's cast and crew as the highest-ranked source with any gives them.
func (s *Store) credits(ctx context.Context, item model.UUID) ([]CreditRef, error) {
	ranked, err := ranks(ctx, s.q, item)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Source   domain.FieldSource
		PersonID model.UUID
		Name     string
		Kind     domain.CreditKind
		Role     string
		PhotoID  *model.UUID
	}
	err = s.q.Credit.WithContext(ctx).UnderlyingDB().Raw(`
		SELECT c.source, c.person_id, p.name, c.kind, c.role, p.photo_id FROM credits c
		JOIN people p ON p.id = c.person_id WHERE c.item_id = ? ORDER BY c.position`, item).Scan(&rows).Error
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
		ref := CreditRef{PersonID: uuid.UUID(r.PersonID), Name: r.Name, Kind: r.Kind, Role: r.Role}
		if r.PhotoID != nil {
			ref.Photo = uuid.UUID(*r.PhotoID)
		}
		out = append(out, ref)
	}
	return out, nil
}

// PersonPage is someone as their page shows them: what is known of them and their work here.
type PersonPage struct {
	ID         uuid.UUID                  `json:"id"`
	Name       string                     `json:"name"`
	Photo      uuid.UUID                  `json:"photo,omitzero"`
	Biography  string                     `json:"biography,omitzero"`
	Born       domain.Date                `json:"born,omitzero"`
	Died       domain.Date                `json:"died,omitzero"`
	Birthplace string                     `json:"birthplace,omitzero"`
	IDs        map[domain.Provider]string `json:"ids,omitzero"`
	// DescribedAt is when a provider last said who they are, zero for never.
	DescribedAt time.Time `json:"-"`
}

// PersonCredit is a title someone is credited on, as a card, and what they did on it; credits on
// episodes are the show's.
type PersonCredit struct {
	Card Card
	Kind domain.CreditKind
	Role string
}

// Person answers someone's page, or ErrNotFound.
func (s *Store) Person(ctx context.Context, id uuid.UUID) (PersonPage, error) {
	p := s.q.Person
	row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PersonPage{}, ErrNotFound
	}
	if err != nil {
		return PersonPage{}, err
	}
	out := PersonPage{
		ID: id, Name: row.Name, Biography: deref(row.Biography), Born: date(row.Born), Died: date(row.Died),
		Birthplace: deref(row.Birthplace), DescribedAt: deref(row.DescribedAt),
	}
	if row.PhotoID != nil {
		out.Photo = uuid.UUID(*row.PhotoID)
	}
	pi := s.q.PersonExternalID
	ids, err := pi.WithContext(ctx).Where(pi.PersonID.Eq(row.ID)).Find()
	if err != nil {
		return PersonPage{}, err
	}
	for _, i := range ids {
		if out.IDs == nil {
			out.IDs = map[domain.Provider]string{}
		}
		out.IDs[i.Provider] = i.Value
	}
	return out, nil
}

// DescribePerson records what a provider says of someone.
func (s *Store) DescribePerson(ctx context.Context, id uuid.UUID, d domain.Person) error {
	return s.q.Transaction(func(tx *query.Query) error {
		p := tx.Person
		row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
		if err != nil {
			return err
		}
		now := time.Now()
		row.Biography, row.Birthplace, row.DescribedAt = optional(d.Biography), optional(d.Birthplace), &now
		row.Born, row.Died = optionalTime(d.Born), optionalTime(d.Died)
		setPhoto(row, d.Photo)
		return p.WithContext(ctx).Save(row)
	})
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// PersonCredits answers the films and shows someone is credited on, the newest first.
func (s *Store) PersonCredits(ctx context.Context, profile, person uuid.UUID) ([]PersonCredit, error) {
	var links []struct {
		ItemID model.UUID
		Kind   domain.CreditKind
		Role   string
	}
	// An episode's credit is its show's; a person in many episodes is listed once per part.
	err := s.q.Credit.WithContext(ctx).UnderlyingDB().Raw(`
		SELECT DISTINCT ON (t.id, c.kind) t.id AS item_id, c.kind, c.role FROM credits c
		JOIN items i ON i.id = c.item_id
		JOIN items t ON t.id = CASE i.kind WHEN 'episode' THEN (SELECT s.parent_id FROM items s WHERE s.id = i.parent_id) ELSE i.id END
		WHERE c.person_id = ? AND t.kind IN ('movie', 'show') AND visible(t.id, ?)
		ORDER BY t.id, c.kind, c.position`, person.String(), profile.String()).Scan(&links).Error
	if err != nil || len(links) == 0 {
		return nil, err
	}
	ids := make([]driver.Valuer, len(links))
	for n, l := range links {
		ids[n] = l.ItemID
	}
	i := s.q.Item
	rows, err := i.WithContext(ctx).Where(i.ID.In(ids...)).Order(i.ReleasedDesc.Desc(), i.ID).Find()
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
			if uuid.UUID(l.ItemID) == c.ID {
				out = append(out, PersonCredit{Card: c, Kind: l.Kind, Role: l.Role})
			}
		}
	}
	return out, nil
}

// similarShown is how many similar titles a title's page offers.
const similarShown = 20

// Similar answers the films or shows most like a title, as Plex ranks them: by how many of its
// first three genres, its first director, its first writer and its five top-billed actors they
// share, all counting alike, the newer first on a tie. A title sharing none is left out.
func (s *Store) Similar(ctx context.Context, profile, id uuid.UUID) ([]Card, error) {
	i := s.q.Item
	item, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if item.Kind != domain.ItemMovie && item.Kind != domain.ItemShow {
		return []Card{}, nil
	}
	var rows []*model.Item
	err = i.WithContext(ctx).UnderlyingDB().Raw(`
		WITH src AS (
			SELECT id, kind, ARRAY(SELECT jsonb_array_elements_text(coalesce(genres, '[]'))) AS genres FROM items WHERE id = @id
		), src_genres AS (
			SELECT src.genres[1:3] AS genres FROM src
		), picked AS (
			(SELECT person_id, kind FROM credits WHERE item_id = @id AND kind = 'director' ORDER BY position LIMIT 1)
			UNION ALL
			(SELECT person_id, kind FROM credits WHERE item_id = @id AND kind = 'writer' ORDER BY position LIMIT 1)
			UNION ALL
			(SELECT DISTINCT ON (position, person_id) person_id, kind FROM credits
				WHERE item_id = @id AND kind = 'actor' ORDER BY position, person_id LIMIT 5)
		), shared_people AS (
			SELECT other.item_id, count(DISTINCT (other.person_id, other.kind)) AS shared
			FROM picked JOIN credits other ON other.person_id = picked.person_id AND other.kind = picked.kind
			WHERE other.item_id <> @id
			GROUP BY other.item_id
		), candidates AS (
			SELECT i.*, ARRAY(SELECT jsonb_array_elements_text(coalesce(i.genres, '[]'))) AS genre_list FROM items i, src
			WHERE i.kind = src.kind AND i.id <> src.id AND visible(i.id, @profile)
		)
		SELECT c.* FROM candidates c CROSS JOIN src_genres g
		LEFT JOIN shared_people p ON p.item_id = c.id
		WHERE c.genre_list && g.genres OR p.item_id IS NOT NULL
		ORDER BY cardinality(ARRAY(SELECT unnest(c.genre_list) INTERSECT SELECT unnest(g.genres))) + coalesce(p.shared, 0) DESC,
			c.released_desc DESC NULLS LAST, c.added_at, c.id
		LIMIT @limit`,
		map[string]any{"id": item.ID, "limit": similarShown, "profile": profile.String()}).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}
