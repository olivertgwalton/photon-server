package store

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// credited is what a source credits on one title.
type credited struct {
	item    uuid.UUID
	credits []domain.Credit
}

// personKey is one provider's id for someone.
type personKey struct {
	provider domain.Provider
	value    string
}

// saveCredits replaces what a source credits on titles, keeping one row per person whatever titles
// credit them, in a handful of statements however many credits there are. A person is known by
// any of their ids; a credit with none is passed over. Credits sharing an id, or whose ids name
// people already, are one person, and people found by different ids of one are merged into the
// first added. The ids a person lacks are added, and their name and picture follow the last
// credit; a new picture gets a new id. Someone another match adds at the same moment is theirs.
func saveCredits(ctx context.Context, tx db, source domain.FieldSource, titles []credited) error {
	type entry struct {
		item     uuid.UUID
		position int
		credit   domain.Credit
		keys     []personKey
	}
	var entries []entry
	var keys []personKey
	var cleared []uuid.UUID
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
	_, err := tx.Exec(ctx, `DELETE FROM credits WHERE item_id = ANY($1) AND source = $2`, cleared, source)
	if err != nil || len(entries) == 0 {
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
	add := &pgx.Batch{}
	for _, p := range people {
		if !slices.ContainsFunc(p.keys, func(k personKey) bool { _, ok := owners[k]; return ok }) {
			added := &model.Person{Name: p.name}
			setPhoto(added, p.photo)
			p.added = added
			add.Queue(`INSERT INTO people (name, photo_url, photo_id, photo_blurhash) VALUES ($1, $2, $3, $4) RETURNING id`,
				added.Name, added.PhotoURL, added.PhotoID, added.PhotoBlurhash).QueryRow(func(r pgx.Row) error {
				return r.Scan(&added.ID)
			})
		}
	}
	if add.Len() > 0 {
		if err := tx.SendBatch(ctx, add).Close(); err != nil {
			return err
		}
	}
	// The ids not yet known go in in one order, so two matches adding the same people wait on
	// each other rather than deadlock; an id another took first is left with them.
	var newPeople []uuid.UUID
	var newIDs [2][]string
	for _, p := range people {
		var to uuid.UUID
		if p.added != nil {
			to = p.added.ID
		}
		for _, k := range p.keys {
			if o, ok := owners[k]; ok && (to == (uuid.UUID{}) || before(o.PersonID, to)) {
				to = o.PersonID
			}
		}
		for _, k := range p.keys {
			if _, ok := owners[k]; !ok {
				newPeople = append(newPeople, to)
				newIDs[0] = append(newIDs[0], string(k.provider))
				newIDs[1] = append(newIDs[1], k.value)
			}
		}
	}
	if len(newPeople) > 0 {
		_, err := tx.Exec(ctx, `
			INSERT INTO person_ids (person_id, provider, value)
			SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[]) ORDER BY 2, 3
			ON CONFLICT DO NOTHING`, newPeople, newIDs[0], newIDs[1])
		if err != nil {
			return err
		}
		if owners, err = personOwners(ctx, tx, keys); err != nil {
			return err
		}
	}

	var credits struct {
		items, people []uuid.UUID
		kinds         []domain.CreditKind
		roles         []string
		positions     []int
	}
	var orphans []uuid.UUID
	for _, p := range people {
		var found []uuid.UUID
		for _, k := range p.keys {
			found = append(found, owners[k].PersonID)
		}
		slices.SortFunc(found, uuid.UUID.Compare)
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
				row := &model.Person{Name: p.name, PhotoURL: o.PhotoURL, PhotoID: o.PhotoID, PhotoBlurhash: o.PhotoBlurhash}
				setPhoto(row, p.photo)
				_, err := tx.Exec(ctx, `UPDATE people SET name = $2, photo_url = $3, photo_id = $4, photo_blurhash = $5 WHERE id = $1`,
					keep, row.Name, row.PhotoURL, row.PhotoID, row.PhotoBlurhash)
				if err != nil {
					return err
				}
			}
		}
		for _, n := range p.entries {
			e := entries[n]
			credits.items = append(credits.items, e.item)
			credits.people = append(credits.people, keep)
			credits.kinds = append(credits.kinds, e.credit.Kind)
			credits.roles = append(credits.roles, e.credit.Role)
			credits.positions = append(credits.positions, e.position)
		}
	}
	if len(orphans) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM people WHERE id = ANY($1)`, orphans); err != nil {
			return err
		}
	}
	// A source may credit one person twice for one part: an actor billed as two names of one role.
	_, err = tx.Exec(ctx, `
		INSERT INTO credits (item_id, person_id, source, kind, role, position)
		SELECT i, p, $3, k, r, n FROM unnest($1::uuid[], $2::uuid[], $4::text[], $5::text[], $6::int[]) AS c(i, p, k, r, n)
		ON CONFLICT DO NOTHING`,
		credits.items, credits.people, source, credits.kinds, credits.roles, credits.positions)
	return err
}

// before reports whether a was added before b, as ids are minted in time order.
func before(a, b uuid.UUID) bool { return a.Compare(b) < 0 }

// owner is the person an id names, as they are now.
type owner struct {
	PersonID      uuid.UUID
	Provider      domain.Provider
	Value         string
	Name          string
	PhotoURL      *string
	PhotoID       *uuid.UUID
	PhotoBlurhash *string
}

// personOwners answers who each of keys names, where anyone does.
func personOwners(ctx context.Context, tx db, keys []personKey) (map[personKey]owner, error) {
	providers, values := make([]string, len(keys)), make([]string, len(keys))
	for n, k := range keys {
		providers[n], values[n] = string(k.provider), k.value
	}
	found, err := tx.Query(ctx, `
		SELECT i.person_id, i.provider, i.value, p.name, p.photo_url, p.photo_id, p.photo_blurhash
		FROM person_ids i JOIN people p ON p.id = i.person_id
		WHERE (i.provider, i.value) IN (SELECT * FROM unnest($1::text[], $2::text[]))`, providers, values)
	if err != nil {
		return nil, err
	}
	rows, err := pgx.CollectRows(found, pgx.RowToStructByName[owner])
	out := make(map[personKey]owner, len(rows))
	for _, r := range rows {
		out[personKey{r.Provider, r.Value}] = r
	}
	return out, err
}

// mergePerson folds other into into: their credits and the ids into has no id of the provider for.
func mergePerson(ctx context.Context, tx db, into, other uuid.UUID) error {
	args := pgx.NamedArgs{"into": into, "other": other}
	for _, stmt := range []string{
		`UPDATE person_ids SET person_id = @into WHERE person_id = @other
			AND provider NOT IN (SELECT provider FROM person_ids WHERE person_id = @into)`,
		`UPDATE credits c SET person_id = @into WHERE person_id = @other AND NOT EXISTS (
			SELECT 1 FROM credits k WHERE k.person_id = @into AND (k.item_id, k.source, k.kind, k.role) = (c.item_id, c.source, c.kind, c.role))`,
		`DELETE FROM people WHERE id = @other`,
	} {
		if _, err := tx.Exec(ctx, stmt, args); err != nil {
			return err
		}
	}
	return nil
}

func setPhoto(p *model.Person, url string) {
	if url == "" || deref(p.PhotoURL) == url {
		return
	}
	id := uuid.NewV7()
	p.PhotoURL, p.PhotoID, p.PhotoBlurhash = &url, &id, nil
}

// CreditRef is someone's part in a title, with their picture's id.
type CreditRef struct {
	PersonID uuid.UUID         `json:"person_id"`
	Name     string            `json:"name"`
	Kind     domain.CreditKind `json:"kind"`
	Role     string            `json:"role,omitzero"`
	Photo    uuid.UUID         `json:"photo,omitzero"`
	// Blurhashes holds the photo's BlurHash, where it has one.
	Blurhashes Blurhashes `json:"blurhashes,omitzero"`
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
	found, err := s.pool.Query(ctx, `
		SELECT c.source, c.person_id, p.name, c.kind, c.role, p.photo_id, p.photo_blurhash AS blurhash FROM credits c
		JOIN people p ON p.id = c.person_id WHERE c.item_id = $1 ORDER BY c.position`, item)
	if err != nil {
		return nil, err
	}
	rows, err := pgx.CollectRows(found, pgx.RowToStructByName[creditRow])
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

// PersonPage is someone as their page shows them: what is known of them and their work here.
type PersonPage struct {
	ID         uuid.UUID                  `json:"id"`
	Name       string                     `json:"name"`
	Photo      uuid.UUID                  `json:"photo,omitzero"`
	Blurhashes Blurhashes                 `json:"blurhashes,omitzero"`
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

// personColumns are model.Person's, for a statement that reads whole people.
const personColumns = `id, name, photo_url, photo_id, photo_blurhash, biography, born, died, birthplace, described_at`

// Person answers someone's page, or ErrNotFound.
func (s *Store) Person(ctx context.Context, id uuid.UUID) (PersonPage, error) {
	row, err := readRow[model.Person](ctx, s.pool, `SELECT `+personColumns+` FROM people WHERE id = $1`, id)
	if err != nil {
		return PersonPage{}, err
	}
	out := PersonPage{
		ID: id, Name: row.Name, Biography: deref(row.Biography), Born: domain.Date(deref(row.Born)), Died: domain.Date(deref(row.Died)),
		Birthplace: deref(row.Birthplace), DescribedAt: deref(row.DescribedAt),
	}
	out.Photo, out.Blurhashes = photo(row.PhotoID, row.PhotoBlurhash)
	var provider domain.Provider
	var value string
	ids, err := s.pool.Query(ctx, `SELECT provider, value FROM person_ids WHERE person_id = $1`, id)
	if err != nil {
		return PersonPage{}, err
	}
	_, err = pgx.ForEachRow(ids, []any{&provider, &value}, func() error {
		if out.IDs == nil {
			out.IDs = map[domain.Provider]string{}
		}
		out.IDs[provider] = value
		return nil
	})
	if err != nil {
		return PersonPage{}, err
	}
	return out, nil
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
	found, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (t.id, c.kind) t.id AS item_id, c.kind, c.role FROM credits c
		JOIN items i ON i.id = c.item_id
		JOIN items t ON t.id = CASE i.kind WHEN 'episode' THEN (SELECT s.parent_id FROM items s WHERE s.id = i.parent_id) ELSE i.id END
		WHERE c.person_id = $1 AND t.kind IN ('movie', 'show')
			AND EXISTS (SELECT 1 FROM viewer($2) v WHERE sees(v, t) AND first_of_title(v, t))
		ORDER BY t.id, c.kind, c.position`, person, profile)
	if err != nil {
		return nil, err
	}
	links, err := pgx.CollectRows(found, pgx.RowToStructByName[creditLink])
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

// similarShown is how many similar titles a title's page offers.
const similarShown = 20

// Similar answers the films or shows most like a title, as Plex ranks them: by how many of its
// first three genres, its first director, its first writer and its five top-billed actors they
// share, all counting alike, the newer first on a tie. A title sharing none is left out.
func (s *Store) Similar(ctx context.Context, profile, id uuid.UUID) ([]Card, error) {
	var kind domain.ItemKind
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, id).Scan(&kind); err != nil {
		return nil, found(err)
	}
	if kind != domain.ItemMovie && kind != domain.ItemShow {
		return []Card{}, nil
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `
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
			SELECT i.id, i.released_desc, i.added_at, ARRAY(SELECT jsonb_array_elements_text(coalesce(i.genres, '[]'))) AS genre_list
			FROM items i, src
			WHERE i.kind = src.kind AND i.id NOT IN (SELECT same_title(@id))
				AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, i))
		), ranked AS (
			SELECT c.id, c.released_desc, c.added_at,
				cardinality(ARRAY(SELECT unnest(c.genre_list) INTERSECT SELECT unnest(g.genres))) + coalesce(p.shared, 0) AS score
			FROM candidates c CROSS JOIN src_genres g
			LEFT JOIN shared_people p ON p.item_id = c.id
			WHERE c.genre_list && g.genres OR p.item_id IS NOT NULL
			ORDER BY score DESC, c.released_desc DESC NULLS LAST, c.added_at, c.id
			-- Kept whole, so only the titles down the ranking until enough are shown are asked
			-- whether they are the one of their title shown, not every candidate.
			OFFSET 0
		), shown AS (
			SELECT id AS shown_id, score FROM ranked
			WHERE (SELECT first_of_title(v, i) FROM items i, viewer(@profile) v WHERE i.id = ranked.id)
			LIMIT @limit
		)
		SELECT `+itemColumns+` FROM shown JOIN items ON items.id = shown.shown_id
		ORDER BY shown.score DESC, items.released_desc DESC NULLS LAST, items.added_at, items.id`,
		pgx.NamedArgs{"id": id, "limit": similarShown, "profile": profile})
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}
