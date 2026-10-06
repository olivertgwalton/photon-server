package store

import (
	"cmp"
	"context"
	"database/sql/driver"
	"encoding/json"
	"slices"
	"time"

	"gorm.io/gen/field"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// jsonList writes a list column as the JSON its serializer reads back.
type jsonList []string

func (l jsonList) Value() (driver.Value, error) {
	b, err := json.Marshal([]string(l))
	return string(b), err
}

// applyMetadata writes what source says about a title, field by field, wherever no source its
// library ranks higher has spoken, and remembers the source of each field it writes. A source the
// library does not take writes nothing. A list is replaced whole, never merged, so two providers'
// genres never stand side by side.
func applyMetadata(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, m domain.Metadata) error {
	f := tx.ItemField
	rows, err := f.WithContext(ctx).Where(f.ItemID.Eq(item)).Find()
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
	current := make(map[domain.Field]int, len(rows))
	for _, r := range rows {
		current[r.Field] = ranked[r.Source]
	}
	i := tx.Item
	var assigns []field.AssignExpr
	var written []*model.ItemField
	set := func(name domain.Field, said bool, a field.AssignExpr) {
		if cur, ok := current[name]; (said || slices.Contains(m.Locked, name)) && (!ok || rank >= cur) {
			if said {
				assigns = append(assigns, a)
			}
			written = append(written, &model.ItemField{ItemID: item, Field: name, Source: source})
		}
	}
	set(domain.FieldTitle, m.Title != "", i.Title.Value(m.Title))
	sort := cmp.Or(m.SortTitle, m.Title)
	set(domain.FieldSortTitle, sort != "", i.SortTitle.Value(sortTitle(sort)))
	set(domain.FieldOriginalTitle, m.OriginalTitle != "", i.OriginalTitle.Value(m.OriginalTitle))
	set(domain.FieldOverview, m.Overview != "", i.Overview.Value(m.Overview))
	set(domain.FieldTagline, m.Tagline != "", i.Tagline.Value(m.Tagline))
	set(domain.FieldCertificate, m.Certificate != "", i.Certificate.Value(m.Certificate))
	set(domain.FieldReleaseDate, !m.ReleaseDate.IsZero(), i.ReleaseDate.Value(m.ReleaseDate))
	set(domain.FieldYear, m.Year != 0, i.Year.Value(m.Year))
	set(domain.FieldGenres, len(m.Genres) > 0, i.Genres.Value(jsonList(m.Genres)))
	set(domain.FieldStudios, len(m.Studios) > 0, i.Studios.Value(jsonList(m.Studios)))
	if len(written) == 0 {
		return nil
	}
	if len(assigns) > 0 {
		if _, err := i.WithContext(ctx).Where(i.ID.Eq(item)).UpdateSimple(assigns...); err != nil {
			return err
		}
	}
	return f.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "item_id"}, {Name: "field"}},
		DoUpdates: clause.Assignments(map[string]any{"source": source, "updated_at": time.Now()}),
	}).Create(written...)
}

// ranks orders the sources that may write an item's fields: what files say lowest, a reader's own
// edit highest, and its library's sources between, in the library's order. A source the library
// does not take is absent, and a value it once wrote ranks below everything.
func ranks(ctx context.Context, tx *query.Query, item model.UUID) (map[domain.FieldSource]int, error) {
	ls, i := tx.LibrarySource, tx.Item
	taken, err := ls.WithContext(ctx).Join(i, i.LibraryID.EqCol(ls.LibraryID)).Where(i.ID.Eq(item)).Find()
	if err != nil {
		return nil, err
	}
	return rankOf(taken), nil
}

// rankOf is how highly a library that takes these sources ranks each, higher first: an edit over
// them all, a file under them all.
func rankOf(taken []*model.LibrarySource) map[domain.FieldSource]int {
	out := map[domain.FieldSource]int{domain.SourceFile: 1, domain.SourceUser: len(taken) + 2}
	for _, t := range taken {
		out[t.Source] = len(taken) + 1 - t.Position
	}
	return out
}

// describe writes what a title's file and folder names say about it, then its NFO, if it has one.
func describe(ctx context.Context, tx *query.Query, item model.UUID, title string, year int, ids map[domain.Provider]string, nfo *domain.Metadata) error {
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
func saveIDs(ctx context.Context, tx *query.Query, item model.UUID, source domain.IDSource, ids map[domain.Provider]string) error {
	if len(ids) == 0 {
		return nil
	}
	e := tx.ExternalID
	known, err := e.WithContext(ctx).Where(e.ItemID.Eq(item)).Find()
	if err != nil {
		return err
	}
	for provider, value := range ids {
		if slices.ContainsFunc(known, func(k *model.ExternalID) bool {
			return k.Provider == provider && k.Source.Rank() > source.Rank()
		}) {
			continue
		}
		err := e.WithContext(ctx).Save(&model.ExternalID{ItemID: item, Provider: provider, Value: value, Source: source})
		if err != nil {
			return err
		}
	}
	return nil
}
