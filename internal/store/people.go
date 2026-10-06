package store

import (
	"bytes"
	"cmp"
	"context"
	"database/sql/driver"
	"errors"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// credited is what a source credits on one title.
type credited struct {
	item    model.UUID
	credits []domain.Credit
}

// personKey is one provider's id for someone.
type personKey struct {
	provider domain.Provider
	value    string
}

// creditBatch is how many credits go in one statement.
const creditBatch = 1000

// saveCredits replaces what a source credits on titles, keeping one row per person whatever titles
// credit them, in a handful of statements however many credits there are. A person is known by
// any of their ids; a credit with none is passed over. Credits sharing an id, or whose ids name
// people already, are one person, and people found by different ids of one are merged into the
// first added. The ids a person lacks are added, and their name and picture follow the last
// credit; a new picture gets a new id. Someone another match adds at the same moment is theirs.
func saveCredits(ctx context.Context, tx *query.Query, source domain.FieldSource, titles []credited) error {
	type entry struct {
		item     model.UUID
		position int
		credit   domain.Credit
		keys     []personKey
	}
	var entries []entry
	var keys []personKey
	var cleared []driver.Valuer
	for _, t := range titles {
		cleared = append(cleared, t.item)
		for n, cr := range t.credits {
			e := entry{item: t.item, position: n, credit: cr}
			for _, provider := range slices.Sorted(maps.Keys(cr.IDs)) {
				if value := cr.IDs[provider]; value != "" {
					e.keys = append(e.keys, personKey{provider, value})
				}
			}
			if len(e.keys) > 0 {
				entries = append(entries, e)
				keys = append(keys, e.keys...)
			}
		}
	}
	c := tx.Credit
	if _, err := c.WithContext(ctx).Where(c.ItemID.In(cleared...), c.Source.Eq(string(source))).Delete(); err != nil || len(entries) == 0 {
		return err
	}
	owners, err := personOwners(ctx, tx, keys)
	if err != nil {
		return err
	}

	// Credits are one person where they share an id, or name one person by different ids.
	parent := make([]int, len(entries))
	find := func(n int) int {
		for parent[n] != n {
			n = parent[n]
		}
		return n
	}
	first := map[any]int{}
	join := func(at any, n int) {
		if m, ok := first[at]; ok {
			parent[find(m)] = find(n)
		} else {
			first[at] = n
		}
	}
	for n, e := range entries {
		parent[n] = n
		for _, k := range e.keys {
			join(k, n)
			if o, ok := owners[k]; ok {
				join(o.PersonID, n)
			}
		}
	}
	type person struct {
		entries     []int
		keys        []personKey
		name, photo string
		added       *model.Person
	}
	var people []*person
	byRoot := map[int]*person{}
	for n, e := range entries {
		p := byRoot[find(n)]
		if p == nil {
			p = &person{}
			byRoot[find(n)] = p
			people = append(people, p)
		}
		p.entries = append(p.entries, n)
		p.keys = append(p.keys, e.keys...)
		p.name = e.credit.Name
		p.photo = cmp.Or(e.credit.Photo, p.photo)
	}

	// Someone none of whose ids is known is added, as the last credit names them.
	var added []*model.Person
	for _, p := range people {
		if !slices.ContainsFunc(p.keys, func(k personKey) bool { _, ok := owners[k]; return ok }) {
			p.added = &model.Person{Name: p.name}
			setPhoto(p.added, p.photo)
			added = append(added, p.added)
		}
	}
	if len(added) > 0 {
		if err := tx.Person.WithContext(ctx).Create(added...); err != nil {
			return err
		}
	}
	// The ids not yet known go in in one order, so two matches adding the same people wait on
	// each other rather than deadlock; an id another took first is left with them.
	var newIDs [3][]string
	for _, p := range people {
		var to model.UUID
		if p.added != nil {
			to = p.added.ID
		}
		for _, k := range p.keys {
			if o, ok := owners[k]; ok && (to == (model.UUID{}) || before(o.PersonID, to)) {
				to = o.PersonID
			}
		}
		for _, k := range p.keys {
			if _, ok := owners[k]; !ok {
				newIDs[0] = append(newIDs[0], uuid.UUID(to).String())
				newIDs[1] = append(newIDs[1], string(k.provider))
				newIDs[2] = append(newIDs[2], k.value)
			}
		}
	}
	if len(newIDs[0]) > 0 {
		err := tx.PersonExternalID.WithContext(ctx).UnderlyingDB().Exec(`
			INSERT INTO person_ids (person_id, provider, value)
			SELECT * FROM unnest(?::uuid[], ?::text[], ?::text[]) ORDER BY 2, 3
			ON CONFLICT DO NOTHING`, array(newIDs[0]), array(newIDs[1]), array(newIDs[2])).Error
		if err != nil {
			return err
		}
		if owners, err = personOwners(ctx, tx, keys); err != nil {
			return err
		}
	}

	pp := tx.Person
	var rows []*model.Credit
	var orphans []driver.Valuer
	for _, p := range people {
		var found []model.UUID
		for _, k := range p.keys {
			found = append(found, owners[k].PersonID)
		}
		slices.SortFunc(found, func(a, b model.UUID) int { return bytes.Compare(a[:], b[:]) })
		found = slices.Compact(found)
		keep := found[0]
		for _, other := range found[1:] {
			if err := mergePerson(ctx, tx, keep, other); err != nil {
				return err
			}
		}
		if p.added != nil && !slices.Contains(found, p.added.ID) {
			orphans = append(orphans, p.added.ID)
		}
		if p.added == nil || p.added.ID != keep {
			o := owners[p.keys[slices.IndexFunc(p.keys, func(k personKey) bool { return owners[k].PersonID == keep })]]
			if o.Name != p.name || (p.photo != "" && deref(o.PhotoURL) != p.photo) {
				row := &model.Person{Name: p.name, PhotoURL: o.PhotoURL, PhotoID: o.PhotoID}
				setPhoto(row, p.photo)
				if _, err := pp.WithContext(ctx).Where(pp.ID.Eq(keep)).Select(pp.Name, pp.PhotoURL, pp.PhotoID).Updates(row); err != nil {
					return err
				}
			}
		}
		for _, n := range p.entries {
			e := entries[n]
			rows = append(rows, &model.Credit{ItemID: e.item, PersonID: keep, Source: source, Kind: e.credit.Kind, Role: e.credit.Role, Position: e.position})
		}
	}
	if len(orphans) > 0 {
		if _, err := pp.WithContext(ctx).Where(pp.ID.In(orphans...)).Delete(); err != nil {
			return err
		}
	}
	// A source may credit one person twice for one part: an actor billed as two names of one role.
	return c.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, creditBatch)
}

// before reports whether a was added before b, as ids are minted in time order.
func before(a, b model.UUID) bool { return bytes.Compare(a[:], b[:]) < 0 }

// owner is the person an id names, as they are now.
type owner struct {
	PersonID model.UUID
	Provider domain.Provider
	Value    string
	Name     string
	PhotoURL *string
	PhotoID  *model.UUID
}

// personOwners answers who each of keys names, where anyone does.
func personOwners(ctx context.Context, tx *query.Query, keys []personKey) (map[personKey]owner, error) {
	providers, values := make([]string, len(keys)), make([]string, len(keys))
	for n, k := range keys {
		providers[n], values[n] = string(k.provider), k.value
	}
	var rows []owner
	err := tx.Person.WithContext(ctx).UnderlyingDB().Raw(`
		SELECT i.person_id, i.provider, i.value, p.name, p.photo_url, p.photo_id
		FROM person_ids i JOIN people p ON p.id = i.person_id
		WHERE (i.provider, i.value) IN (SELECT * FROM unnest(?::text[], ?::text[]))`,
		array(providers), array(values)).Scan(&rows).Error
	out := make(map[personKey]owner, len(rows))
	for _, r := range rows {
		out[personKey{r.Provider, r.Value}] = r
	}
	return out, err
}

// array is a list as one parameter, where GORM would expand it into a parameter per element.
func array(values []string) pgtype.Array[string] {
	return pgtype.Array[string]{Elements: values, Dims: []pgtype.ArrayDimension{{Length: int32(len(values)), LowerBound: 1}}, Valid: true}
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
		WHERE c.person_id = ? AND t.kind IN ('movie', 'show')
			AND EXISTS (SELECT 1 FROM viewer(?) v WHERE sees(v, t))
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
			WHERE i.kind = src.kind AND i.id <> src.id
				AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, i))
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
