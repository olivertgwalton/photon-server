package store

import (
	"cmp"
	"context"
	"maps"
	"slices"
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
	entries, keys, cleared := creditEntries(titles)
	_, err := tx.Exec(ctx, `DELETE FROM credits WHERE item_id = ANY($1) AND source = $2`, cleared, source)
	if err != nil || len(entries) == 0 {
		return err
	}
	owners, err := personOwners(ctx, tx, keys)
	if err != nil {
		return err
	}
	adopted, err := adoptNamesakes(ctx, tx, source, entries, owners)
	if err != nil {
		return err
	}
	if adopted {
		if owners, err = personOwners(ctx, tx, keys); err != nil {
			return err
		}
	}
	people := creditedPeople(entries, owners)
	if err := addPeople(ctx, tx, people, owners); err != nil {
		return err
	}
	added, err := addPersonIDs(ctx, tx, people, owners)
	if err != nil {
		return err
	}
	if added {
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
		keep, orphan, err := settlePerson(ctx, tx, p, owners)
		if err != nil {
			return err
		}
		if orphan {
			orphans = append(orphans, p.added.ID)
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

// adoptNamesakes gives each id no one has to the person another source credits on the same title
// by the same name, where that is one person with no id of the provider yet, answering whether it
// gave any. Sources know a person by ids of their own (TVDB's, TMDB's), and two crediting one show
// name one cast: a namesake on one title is far rarer than one person credited by two sources.
func adoptNamesakes(ctx context.Context, tx db, source domain.FieldSource, entries []creditEntry, owners map[personKey]owner) (bool, error) {
	var items []uuid.UUID
	var names, providers, values []string
	for _, e := range entries {
		for _, k := range e.keys {
			if _, ok := owners[k]; !ok {
				items, names = append(items, e.item), append(names, e.credit.Name)
				providers, values = append(providers, string(k.provider)), append(values, k.value)
			}
		}
	}
	if len(items) == 0 {
		return false, nil
	}
	info, err := tx.Exec(ctx, `
		WITH asked AS (
			SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[]) AS a(item, name, provider, value)
		), namesakes AS (
			SELECT a.provider, a.value, (array_agg(DISTINCT c.person_id))[1] AS person_id
			FROM asked a
			JOIN credits c ON c.item_id = a.item AND c.source <> $5
			JOIN people p ON p.id = c.person_id AND lower(p.name) = lower(a.name)
			WHERE NOT EXISTS (SELECT 1 FROM person_ids i WHERE i.person_id = c.person_id AND i.provider = a.provider)
			GROUP BY a.provider, a.value
			HAVING count(DISTINCT c.person_id) = 1
		)
		INSERT INTO person_ids (person_id, provider, value)
		SELECT person_id, provider, value FROM namesakes ORDER BY provider, value
		ON CONFLICT DO NOTHING`, items, names, providers, values, source)
	return err == nil && info.RowsAffected() > 0, err
}

// creditEntry is one credit with an id, at its place in its title's billing.
type creditEntry struct {
	item     uuid.UUID
	position int
	credit   domain.Credit
	keys     []personKey
}

// creditEntries is the titles' credits that have an id, every id among them, and every title.
func creditEntries(titles []credited) (entries []creditEntry, keys []personKey, cleared []uuid.UUID) {
	for _, t := range titles {
		cleared = append(cleared, t.item)
		for n, cr := range t.credits {
			e := creditEntry{item: t.item, position: n, credit: cr}
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
	return entries, keys, cleared
}

// creditedPerson is the entries that are one person, named as the last of them names them.
type creditedPerson struct {
	entries     []int
	keys        []personKey
	name, photo string
	added       *model.Person
}

// creditedPeople groups entries into people: entries are one person where they share an id, or
// name one person by different ids.
func creditedPeople(entries []creditEntry, owners map[personKey]owner) []*creditedPerson {
	parent := make([]int, len(entries))
	find := func(n int) int {
		for parent[n] != n {
			n = parent[n]
		}
		return n
	}
	// An entry is first found by one of its ids or by a person they already name.
	type found struct {
		key    personKey
		person uuid.UUID
	}
	first := map[found]int{}
	join := func(at found, n int) {
		if m, ok := first[at]; ok {
			parent[find(m)] = find(n)
		} else {
			first[at] = n
		}
	}
	for n, e := range entries {
		parent[n] = n
		for _, k := range e.keys {
			join(found{key: k}, n)
			if o, ok := owners[k]; ok {
				join(found{person: o.PersonID}, n)
			}
		}
	}
	var people []*creditedPerson
	byRoot := map[int]*creditedPerson{}
	for n, e := range entries {
		p := byRoot[find(n)]
		if p == nil {
			p = &creditedPerson{}
			byRoot[find(n)] = p
			people = append(people, p)
		}
		p.entries = append(p.entries, n)
		p.keys = append(p.keys, e.keys...)
		p.name = e.credit.Name
		p.photo = cmp.Or(e.credit.Photo, p.photo)
	}
	return people
}

// addPeople adds everyone none of whose ids is known, as the last credit names them.
func addPeople(ctx context.Context, tx db, people []*creditedPerson, owners map[personKey]owner) error {
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
	if add.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, add).Close()
}

// addPersonIDs adds the ids not yet known, answering whether there were any. They go in in one
// order, so two matches adding the same people wait on each other rather than deadlock; an id
// another took first is left with them.
func addPersonIDs(ctx context.Context, tx db, people []*creditedPerson, owners map[personKey]owner) (bool, error) {
	var newPeople []uuid.UUID
	var newIDs [2][]string
	for _, p := range people {
		var to uuid.UUID
		if p.added != nil {
			to = p.added.ID
		}
		// Of the people its keys name, the one added first wins, as ids are minted in time order.
		for _, k := range p.keys {
			if o, ok := owners[k]; ok && (to == (uuid.UUID{}) || o.PersonID.Compare(to) < 0) {
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
	if len(newPeople) == 0 {
		return false, nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO person_ids (person_id, provider, value)
		SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[]) ORDER BY 2, 3
		ON CONFLICT DO NOTHING`, newPeople, newIDs[0], newIDs[1])
	return err == nil, err
}

// settlePerson merges the people p's ids now name into the first added and gives them p's name
// and picture, answering who is kept and whether the person p added lost every id to another.
func settlePerson(ctx context.Context, tx db, p *creditedPerson, owners map[personKey]owner) (uuid.UUID, bool, error) {
	var found []uuid.UUID
	for _, k := range p.keys {
		found = append(found, owners[k].PersonID)
	}
	slices.SortFunc(found, uuid.UUID.Compare)
	found = slices.Compact(found)
	keep := found[0]
	for _, other := range found[1:] {
		if err := mergePerson(ctx, tx, keep, other); err != nil {
			return keep, false, err
		}
	}
	orphan := p.added != nil && !slices.Contains(found, p.added.ID)
	if p.added != nil && p.added.ID == keep {
		return keep, orphan, nil
	}
	o := owners[p.keys[slices.IndexFunc(p.keys, func(k personKey) bool { return owners[k].PersonID == keep })]]
	if o.Name == p.name && (p.photo == "" || deref(o.PhotoURL) == p.photo) {
		return keep, orphan, nil
	}
	row := &model.Person{Name: p.name, PhotoURL: o.PhotoURL, PhotoID: o.PhotoID, PhotoBlurhash: o.PhotoBlurhash}
	setPhoto(row, p.photo)
	_, err := tx.Exec(ctx, `UPDATE people SET name = $2, photo_url = $3, photo_id = $4, photo_blurhash = $5 WHERE id = $1`,
		keep, row.Name, row.PhotoURL, row.PhotoID, row.PhotoBlurhash)
	return keep, orphan, err
}

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
	rows, err := queryStructs[owner](ctx, tx, `
		SELECT i.person_id, i.provider, i.value, p.name, p.photo_url, p.photo_id, p.photo_blurhash
		FROM person_ids i JOIN people p ON p.id = i.person_id
		WHERE (i.provider, i.value) IN (SELECT * FROM unnest($1::text[], $2::text[]))`, providers, values)
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
