package store

import (
	"context"
	"path"

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
