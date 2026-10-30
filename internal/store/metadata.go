package store

import (
	"context"
	"fmt"
	"maps"
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
	f, err := fieldsOf(ctx, tx, item)
	if err != nil {
		return err
	}
	return f.apply(ctx, tx, source, m)
}

// fields is what decides which of a title's fields and ids a source may write: the source of each,
// and how its library ranks the sources of its metadata.
type fields struct {
	item    uuid.UUID
	sources map[domain.Field]domain.FieldSource
	ranked  map[domain.FieldSource]int
	ids     map[domain.Provider]domain.IDSource
}

// fieldsOf reads a title's fields once, however many sources then write them.
func fieldsOf(ctx context.Context, tx db, item uuid.UUID) (*fields, error) {
	it := &model.Item{ID: item}
	var named []domain.Field
	var by []domain.FieldSource
	var providers []domain.Provider
	var from []domain.IDSource
	err := tx.QueryRow(ctx, `
		SELECT i.kind, i.library_id,
			ARRAY(SELECT field FROM item_fields WHERE item_id = i.id ORDER BY field),
			ARRAY(SELECT source FROM item_fields WHERE item_id = i.id ORDER BY field),
			ARRAY(SELECT provider FROM external_ids WHERE item_id = i.id ORDER BY provider),
			ARRAY(SELECT source FROM external_ids WHERE item_id = i.id ORDER BY provider)
		FROM items i WHERE i.id = $1`, item).
		Scan(&it.Kind, &it.LibraryID, &named, &by, &providers, &from)
	if err != nil {
		return nil, found(err)
	}
	taken, err := rankings(ctx, tx, []*model.Item{it}, domain.FetcherMetadata)
	if err != nil {
		return nil, err
	}
	f := &fields{item: item, sources: map[domain.Field]domain.FieldSource{}, ids: map[domain.Provider]domain.IDSource{}}
	for n, field := range named {
		f.sources[field] = by[n]
	}
	for n, provider := range providers {
		f.ids[provider] = from[n]
	}
	f.ranked = rankOf(taken[item])
	return f, nil
}

// apply is applyMetadata over fields already read, which it keeps up to date.
func (f *fields) apply(ctx context.Context, tx db, source domain.FieldSource, m domain.Metadata) error {
	rank, taken := f.ranked[source]
	if !taken {
		return nil
	}
	var assigns []string
	args := []any{f.item}
	var written []domain.Field
	assign := func(name domain.Field, value any) {
		args = append(args, value)
		assigns = append(assigns, fmt.Sprintf("%s = $%d", name, len(args)))
	}
	outranks := func(name domain.Field) bool {
		from, ok := f.sources[name]
		return !ok || rank >= f.ranked[from]
	}
	// A field is named as the column it is written to. set answers whether it wrote a value.
	set := func(name domain.Field, said bool, value any) bool {
		if !said && !slices.Contains(m.Locked, name) || !outranks(name) {
			return false
		}
		if said {
			assign(name, value)
		}
		written = append(written, name)
		return said
	}
	wroteTitle := set(domain.FieldTitle, m.Title != "", m.Title)
	if m.SortTitle != "" || slices.Contains(m.Locked, domain.FieldSortTitle) {
		set(domain.FieldSortTitle, m.SortTitle != "", sortTitle(m.SortTitle))
	} else if wroteTitle && outranks(domain.FieldSortTitle) {
		// A sort title read off the title goes with it. An admin who edits the title locks the title
		// alone, so the sort title stays its source's, and goes back with the title once reset.
		assign(domain.FieldSortTitle, sortTitle(m.Title))
		if source != domain.SourceUser {
			written = append(written, domain.FieldSortTitle)
		}
	}
	set(domain.FieldOriginalTitle, m.OriginalTitle != "", m.OriginalTitle)
	set(domain.FieldOverview, m.Overview != "", m.Overview)
	set(domain.FieldTagline, m.Tagline != "", m.Tagline)
	set(domain.FieldCertificate, m.Certificate != "", m.Certificate)
	set(domain.FieldReleaseDate, !m.ReleaseDate.IsZero(), m.ReleaseDate)
	set(domain.FieldYear, m.Year != 0, m.Year)
	set(domain.FieldGenres, len(m.Genres) > 0, m.Genres)
	set(domain.FieldStudios, len(m.Studios) > 0, m.Studios)
	set(domain.FieldAirsBeforeSeason, m.AirsBeforeSeason != 0, m.AirsBeforeSeason)
	set(domain.FieldAirsBeforeEpisode, m.AirsBeforeEpisode != 0, m.AirsBeforeEpisode)
	set(domain.FieldAirsAfterSeason, m.AirsAfterSeason != 0, m.AirsAfterSeason)
	if len(written) == 0 {
		return nil
	}
	for _, name := range written {
		f.sources[name] = source
	}
	// A source titling an item answers describeAgain's asking about it.
	if source != domain.SourceFile && slices.Contains(written, domain.FieldTitle) {
		assigns = append(assigns, "asked_again_at = NULL")
	}
	b := &pgx.Batch{}
	if len(assigns) > 0 {
		b.Queue(`UPDATE items SET `+strings.Join(assigns, ", ")+` WHERE id = $1`, args...)
	}
	b.Queue(`
		INSERT INTO item_fields (item_id, field, source) SELECT $1, unnest($2::text[]), $3
		ON CONFLICT (item_id, field) DO UPDATE SET source = excluded.source, updated_at = now()`,
		f.item, written, source)
	return tx.SendBatch(ctx, b).Close()
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
// trusted first; for a collection, in no library, the providers whose box sets become collections.
func rankings(ctx context.Context, q db, items []*model.Item, f domain.Fetcher) (map[uuid.UUID][]domain.FieldSource, error) {
	var libraries []uuid.UUID
	for _, it := range items {
		if it.LibraryID != nil {
			libraries = append(libraries, *it.LibraryID)
		}
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
		if it.LibraryID == nil {
			out[it.ID] = slices.Sorted(maps.Keys(groupingProviders))
		} else if kind, ok := kinds[*it.LibraryID]; ok {
			out[it.ID] = by[ranking{*it.LibraryID, kind.RankedAs(it.Kind)}]
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
	f, err := fieldsOf(ctx, tx, item)
	if err != nil {
		return err
	}
	if err := f.apply(ctx, tx, domain.SourceFile, domain.Metadata{Title: title, Year: year}); err != nil {
		return err
	}
	if err := f.saveIDs(ctx, tx, domain.IDFromPath, ids); err != nil {
		return err
	}
	if nfo == nil {
		return nil
	}
	if err := f.apply(ctx, tx, domain.SourceNFO, *nfo); err != nil {
		return err
	}
	return f.saveIDs(ctx, tx, domain.IDFromNFO, nfo.IDs)
}

// saveIDs records a title's provider ids, each unless a higher-ranking source already gave one.
func saveIDs(ctx context.Context, tx db, item uuid.UUID, source domain.IDSource, ids map[domain.Provider]string) error {
	if len(ids) == 0 {
		return nil
	}
	f, err := fieldsOf(ctx, tx, item)
	if err != nil {
		return err
	}
	return f.saveIDs(ctx, tx, source, ids)
}

// saveIDs is saveIDs over fields already read, which it keeps up to date.
func (f *fields) saveIDs(ctx context.Context, tx db, source domain.IDSource, ids map[domain.Provider]string) error {
	for provider, value := range ids {
		if from, ok := f.ids[provider]; ok && from.Rank() > source.Rank() {
			continue
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO external_ids (item_id, provider, value, source) VALUES ($1, $2, $3, $4)
			ON CONFLICT (item_id, provider) DO UPDATE SET value = excluded.value, source = excluded.source`,
			f.item, provider, value, source)
		if err != nil {
			return err
		}
		f.ids[provider] = source
	}
	return nil
}
