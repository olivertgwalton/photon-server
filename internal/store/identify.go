package store

import (
	"context"
	"errors"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// Subject is what a film or show is matched to a provider by, with the seasons of a show that
// a provider has yet to describe.
type Subject struct {
	Kind    domain.ItemKind
	Title   string
	Year    int
	IDs     map[domain.Provider]string
	Seasons []int
	// Sources are what its library takes metadata from.
	Sources []domain.FieldSource
}

// IdentifySubject answers what is known of a title to match it by, or false for one that has gone.
func (s *Store) IdentifySubject(ctx context.Context, id uuid.UUID) (Subject, bool, error) {
	i, e := s.q.Item, s.q.ExternalID
	item, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Subject{}, false, nil
	}
	if err != nil {
		return Subject{}, false, err
	}
	sub := Subject{Kind: item.Kind, Title: item.Title, IDs: map[domain.Provider]string{}}
	if item.Year != nil {
		sub.Year = *item.Year
	}
	ls := s.q.LibrarySource
	if err := ls.WithContext(ctx).Where(ls.LibraryID.Eq(item.LibraryID)).Order(ls.Position).
		Pluck(ls.Source, &sub.Sources); err != nil {
		return Subject{}, false, err
	}
	ids, err := e.WithContext(ctx).Where(e.ItemID.Eq(item.ID)).Find()
	if err != nil {
		return Subject{}, false, err
	}
	for _, x := range ids {
		sub.IDs[x.Provider] = x.Value
	}
	if item.Kind == domain.ItemShow {
		// Only a season holding something still titled by its file name, as Jellyfin asks a
		// provider only about items it has never refreshed: a show's new episode costs one season.
		err := i.WithContext(ctx).UnderlyingDB().Raw(`
			SELECT DISTINCT s.season_number FROM items s
			JOIN items e ON e.parent_id = s.id OR e.id = s.id
			JOIN item_fields f ON f.item_id = e.id AND f.field = 'title' AND f.source = 'file'
			WHERE s.parent_id = ? AND s.kind = 'season'
			ORDER BY s.season_number`, item.ID).Scan(&sub.Seasons).Error
		if err != nil {
			return Subject{}, false, err
		}
	}
	return sub, true, nil
}

// SaveIdentity writes what a provider says about a title, and about the seasons and episodes of a
// show, under every source that ranks above it.
func (s *Store) SaveIdentity(ctx context.Context, id uuid.UUID, source domain.FieldSource, m domain.Metadata, seasons map[int]domain.SeasonMetadata) error {
	return s.q.Transaction(func(tx *query.Query) error {
		item := model.UUID(id)
		if err := applyMetadata(ctx, tx, item, source, m); err != nil {
			return err
		}
		if err := saveIDs(ctx, tx, item, domain.IDFromMatch, m.IDs); err != nil {
			return err
		}
		i := tx.Item
		for number, season := range seasons {
			row, err := i.WithContext(ctx).Where(
				i.ParentID.Eq(item), i.Kind.Eq(string(domain.ItemSeason)), i.SeasonNumber.Eq(number),
			).Take()
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if err := applyMetadata(ctx, tx, row.ID, source, season.Metadata); err != nil {
				return err
			}
			episodes, err := i.WithContext(ctx).Where(i.ParentID.Eq(row.ID), i.Kind.Eq(string(domain.ItemEpisode))).Find()
			if err != nil {
				return err
			}
			for _, e := range episodes {
				if e.EpisodeNumber == nil {
					continue
				}
				if said, ok := season.Episodes[*e.EpisodeNumber]; ok {
					if err := applyMetadata(ctx, tx, e.ID, source, said); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
