package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// applyMetadata writes what source says about a title, field by field, wherever no source its
// library ranks higher has spoken, and remembers the source of each field it writes. A source the
// library does not take writes nothing. A list is replaced whole, never merged, so two providers'
// genres never stand side by side.
func applyMetadata(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, m domain.Metadata) error {
	type itemField struct {
		Field  domain.Field
		Source domain.FieldSource
	}
	fields, err := queryStructs[itemField](ctx, tx, `SELECT field, source FROM item_fields WHERE item_id = $1`, item)
	if err != nil {
		return err
	}
	ranked, err := ranks(ctx, tx, item)
	if err != nil {
		return err
	}
	rank, taken := ranked[source]
	if !taken {
		return nil
	}
	current := make(map[domain.Field]int, len(fields))
	for _, r := range fields {
		current[r.Field] = ranked[r.Source]
	}
	var assigns []string
	args := []any{item}
	var written []domain.Field
	// A field is named as the column it is written to.
	set := func(name domain.Field, said bool, value any) {
		if cur, ok := current[name]; (said || slices.Contains(m.Locked, name)) && (!ok || rank >= cur) {
			if said {
				args = append(args, value)
				assigns = append(assigns, fmt.Sprintf("%s = $%d", name, len(args)))
			}
			written = append(written, name)
		}
	}
	set(domain.FieldTitle, m.Title != "", m.Title)
	sort := cmp.Or(m.SortTitle, m.Title)
	set(domain.FieldSortTitle, sort != "", sortTitle(sort))
	set(domain.FieldOriginalTitle, m.OriginalTitle != "", m.OriginalTitle)
	set(domain.FieldOverview, m.Overview != "", m.Overview)
	set(domain.FieldTagline, m.Tagline != "", m.Tagline)
	set(domain.FieldCertificate, m.Certificate != "", m.Certificate)
	set(domain.FieldReleaseDate, !m.ReleaseDate.IsZero(), m.ReleaseDate)
	set(domain.FieldYear, m.Year != 0, m.Year)
	set(domain.FieldGenres, len(m.Genres) > 0, m.Genres)
	set(domain.FieldStudios, len(m.Studios) > 0, m.Studios)
	if len(written) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	if len(assigns) > 0 {
		b.Queue(`UPDATE items SET `+strings.Join(assigns, ", ")+` WHERE id = $1`, args...)
	}
	b.Queue(`
		INSERT INTO item_fields (item_id, field, source) SELECT $1, unnest($2::text[]), $3
		ON CONFLICT (item_id, field) DO UPDATE SET source = excluded.source, updated_at = now()`,
		item, written, source)
	return tx.SendBatch(ctx, b).Close()
}

// ranks orders the sources that may write an item's fields: what files say lowest, a reader's own
// edit highest, and those its library asks for the metadata of its kind between, in the library's
// order. A source not asked is absent, and a value it once wrote ranks below everything.
func ranks(ctx context.Context, tx db, item uuid.UUID) (map[domain.FieldSource]int, error) {
	row := &model.Item{ID: item}
	err := tx.QueryRow(ctx, `SELECT library_id, kind FROM items WHERE id = $1`, item).Scan(&row.LibraryID, &row.Kind)
	if err != nil {
		return nil, found(err)
	}
	taken, err := rankings(ctx, tx, []*model.Item{row}, domain.FetcherMetadata)
	if err != nil {
		return nil, err
	}
	return rankOf(taken[item]), nil
}

// rankOf is how highly a library that asks these sources, most trusted first, ranks each, higher
// first: an edit over them all, a file under them all.
func rankOf(taken []domain.FieldSource) map[domain.FieldSource]int {
	out := map[domain.FieldSource]int{domain.SourceFile: 1, domain.SourceUser: len(taken) + 2}
	for n, src := range taken {
		out[src] = len(taken) + 1 - n
	}
	return out
}

// rankings answers, for each of items, the sources its library asks for f of its kind, most
// trusted first.
func rankings(ctx context.Context, q db, items []*model.Item, f domain.Fetcher) (map[uuid.UUID][]domain.FieldSource, error) {
	libraries := make([]uuid.UUID, len(items))
	for n, it := range items {
		libraries[n] = it.LibraryID
	}
	type ranking struct {
		library uuid.UUID
		kind    domain.ItemKind
	}
	kinds := map[uuid.UUID]domain.LibraryKind{}
	by := map[ranking][]domain.FieldSource{}
	var r ranking
	var kind domain.LibraryKind
	var source domain.FieldSource
	rows, err := q.Query(ctx, `
		SELECT ls.library_id, l.kind, ls.item_kind, ls.source FROM library_sources ls JOIN libraries l ON l.id = ls.library_id
		WHERE ls.library_id = ANY($1) AND ls.fetcher = $2 AND ls.enabled ORDER BY ls.position`, libraries, f)
	if err != nil {
		return nil, err
	}
	if _, err := pgx.ForEachRow(rows, []any{&r.library, &kind, &r.kind, &source}, func() error {
		kinds[r.library] = kind
		by[r] = append(by[r], source)
		return nil
	}); err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]domain.FieldSource{}
	for _, it := range items {
		if kind, ok := kinds[it.LibraryID]; ok {
			out[it.ID] = by[ranking{it.LibraryID, kind.RankedAs(it.Kind)}]
		}
	}
	return out, nil
}

// asked is what a title's library asks a source for: the metadata and the pictures of which kinds
// of item.
type asked struct {
	title   domain.ItemKind
	library domain.LibraryKind
	taken   []*model.LibrarySource
}

func askedOf(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource) (asked, error) {
	var a asked
	var lib uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT i.kind, l.kind, l.id FROM items i JOIN libraries l ON l.id = i.library_id WHERE i.id = $1`,
		item).Scan(&a.title, &a.library, &lib)
	if err != nil {
		return asked{}, found(err)
	}
	a.taken, err = queryRows[model.LibrarySource](ctx, tx, `
		SELECT `+librarySourceColumns+` FROM library_sources
		WHERE library_id = $1 AND source = $2 AND enabled`, lib, source)
	return a, err
}

// of is what of m the library asks for of an item of a kind. The rest is said as nothing, so what
// the source said of it before is cleared; its ids stand, for the next source to find it by.
func (a asked) of(kind domain.ItemKind, m domain.Metadata) domain.Metadata {
	kind = a.library.RankedAs(kind)
	asks := func(f domain.Fetcher) bool {
		return slices.ContainsFunc(a.taken, func(t *model.LibrarySource) bool { return t.ItemKind == kind && t.Fetcher == f })
	}
	if !asks(domain.FetcherMetadata) {
		m = domain.Metadata{IDs: m.IDs, Artwork: m.Artwork}
	}
	if !asks(domain.FetcherImages) {
		m.Artwork = nil
	}
	return m
}

// describe writes what a title's file and folder names say about it, then its NFO, if it has one.
func describe(ctx context.Context, tx db, item uuid.UUID, title string, year int, ids map[domain.Provider]string, nfo *domain.Metadata) error {
	if err := applyMetadata(ctx, tx, item, domain.SourceFile, domain.Metadata{Title: title, Year: year}); err != nil {
		return err
	}
	if err := saveIDs(ctx, tx, item, domain.IDFromPath, ids); err != nil {
		return err
	}
	if nfo == nil {
		return nil
	}
	if err := applyMetadata(ctx, tx, item, domain.SourceNFO, *nfo); err != nil {
		return err
	}
	return saveIDs(ctx, tx, item, domain.IDFromNFO, nfo.IDs)
}

// saveIDs records a title's provider ids, each unless a higher-ranking source already gave one.
func saveIDs(ctx context.Context, tx db, item uuid.UUID, source domain.IDSource, ids map[domain.Provider]string) error {
	if len(ids) == 0 {
		return nil
	}
	known := map[domain.Provider]domain.IDSource{}
	var provider domain.Provider
	var from domain.IDSource
	rows, err := tx.Query(ctx, `SELECT provider, source FROM external_ids WHERE item_id = $1`, item)
	if err != nil {
		return err
	}
	if _, err := pgx.ForEachRow(rows, []any{&provider, &from}, func() error {
		known[provider] = from
		return nil
	}); err != nil {
		return err
	}
	for provider, value := range ids {
		if from, ok := known[provider]; ok && from.Rank() > source.Rank() {
			continue
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO external_ids (item_id, provider, value, source) VALUES ($1, $2, $3, $4)
			ON CONFLICT (item_id, provider) DO UPDATE SET value = excluded.value, source = excluded.source`,
			item, provider, value, source)
		if err != nil {
			return err
		}
	}
	return nil
}
