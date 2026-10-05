package store

import (
	"cmp"
	"context"
	"errors"
	"math"
	"path"
	"slices"
	"uuid"

	"gorm.io/gorm"

	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// saveFolderArtwork replaces a title's pictures that are files in folder with those found there
// now; pictures it has in other folders stand.
func saveFolderArtwork(ctx context.Context, tx *query.Query, item model.UUID, folder string, pictures []domain.Artwork) error {
	a := tx.Artwork
	_, err := a.WithContext(ctx).Where(a.ItemID.Eq(item), a.Source.Eq(string(domain.SourceFile)), a.Folder.Eq(folder)).Delete()
	if err != nil {
		return err
	}
	rows := make([]*model.Artwork, 0, len(pictures))
	for n, p := range pictures {
		if path.Dir(p.Path) != folder {
			continue
		}
		rows = append(rows, &model.Artwork{
			ItemID: item, Source: domain.SourceFile, Kind: p.Kind, Place: p.Path, Position: n, Folder: &folder,
		})
	}
	return createArtwork(ctx, tx, rows)
}

// saveProviderArtwork replaces what a provider has for a title with what it has now.
func saveProviderArtwork(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, pictures []domain.Artwork) error {
	a := tx.Artwork
	if _, err := a.WithContext(ctx).Where(a.ItemID.Eq(item), a.Source.Eq(string(source))).Delete(); err != nil {
		return err
	}
	rows := make([]*model.Artwork, len(pictures))
	for n, p := range pictures {
		rows[n] = &model.Artwork{
			ItemID: item, Source: source, Kind: p.Kind, Place: p.URL, Position: n,
			Language: optional(p.Language), Width: optionalInt(p.Width), Height: optionalInt(p.Height),
		}
	}
	return createArtwork(ctx, tx, rows)
}

func createArtwork(ctx context.Context, tx *query.Query, rows []*model.Artwork) error {
	if len(rows) == 0 {
		return nil
	}
	// A provider may list one picture twice.
	return tx.Artwork.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rows...)
}

// pictureOrder answers each title's pictures by kind, best first: files beside it, then its
// library's providers in the library's order, then each provider's own order.
func (s *Store) pictureOrder(ctx context.Context, items []*model.Item) (map[model.UUID]map[domain.ArtworkKind][]uuid.UUID, error) {
	out := map[model.UUID]map[domain.ArtworkKind][]uuid.UUID{}
	if len(items) == 0 {
		return out, nil
	}
	a, ls := s.q.Artwork, s.q.LibrarySource
	rows, err := a.WithContext(ctx).Where(a.ItemID.In(ids(items)...)).Find()
	if err != nil {
		return nil, err
	}
	taken, err := ls.WithContext(ctx).Find()
	if err != nil {
		return nil, err
	}
	rank := map[model.UUID]map[domain.FieldSource]int{}
	for _, t := range taken {
		if rank[t.LibraryID] == nil {
			rank[t.LibraryID] = map[domain.FieldSource]int{domain.SourceFile: -1}
		}
		rank[t.LibraryID][t.Source] = t.Position
	}
	library := map[model.UUID]model.UUID{}
	for _, it := range items {
		library[it.ID] = it.LibraryID
	}
	// A provider the library has since dropped comes after every one it takes.
	order := func(x *model.Artwork) int {
		if r, ok := rank[library[x.ItemID]][x.Source]; ok {
			return r
		}
		return math.MaxInt
	}
	slices.SortStableFunc(rows, func(x, y *model.Artwork) int {
		return cmp.Or(cmp.Compare(order(x), order(y)), cmp.Compare(x.Position, y.Position))
	})
	for _, r := range rows {
		if out[r.ItemID] == nil {
			out[r.ItemID] = map[domain.ArtworkKind][]uuid.UUID{}
		}
		out[r.ItemID][r.Kind] = append(out[r.ItemID][r.Kind], uuid.UUID(r.ID))
	}
	return out, nil
}

// Picture is where one picture is: a file under Root, or a provider's URL.
type Picture struct {
	Root string
	Path string
	URL  string
}

// Picture answers where a picture is, or ErrNotFound.
func (s *Store) Picture(ctx context.Context, id uuid.UUID) (Picture, error) {
	a, i, l := s.q.Artwork, s.q.Item, s.q.Library
	row, err := a.WithContext(ctx).Where(a.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Picture{}, ErrNotFound
	}
	if err != nil {
		return Picture{}, err
	}
	if row.Source != domain.SourceFile {
		return Picture{URL: row.Place}, nil
	}
	var lib struct{ Root string }
	err = l.WithContext(ctx).Select(l.Root).Join(i, i.LibraryID.EqCol(l.ID)).Where(i.ID.Eq(row.ItemID)).Scan(&lib)
	return Picture{Root: lib.Root, Path: row.Place}, err
}
