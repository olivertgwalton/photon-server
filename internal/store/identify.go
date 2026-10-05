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

// Subject is what a film or show is matched to a provider by.
type Subject struct {
	Kind    domain.ItemKind
	Title   string
	Year    int
	IDs     map[domain.Provider]string
	Seasons []int
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
	ids, err := e.WithContext(ctx).Where(e.ItemID.Eq(item.ID)).Find()
	if err != nil {
		return Subject{}, false, err
	}
	for _, x := range ids {
		sub.IDs[x.Provider] = x.Value
	}
	if item.Kind == domain.ItemShow {
		err := i.WithContext(ctx).Where(i.ParentID.Eq(item.ID), i.Kind.Eq(string(domain.ItemSeason))).
			Order(i.SeasonNumber).Pluck(i.SeasonNumber, &sub.Seasons)
		if err != nil {
			return Subject{}, false, err
		}
	}
	return sub, true, nil
}

// SaveIdentity writes what a provider says about a title, and about the seasons and episodes of a
// show, under every source that ranks above it.
func (s *Store) SaveIdentity(ctx context.Context, id uuid.UUID, m domain.Metadata, seasons map[int]domain.SeasonMetadata) error {
	return s.q.Transaction(func(tx *query.Query) error {
		item := model.UUID(id)
		if err := applyMetadata(ctx, tx, item, domain.SourceTMDB, m); err != nil {
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
			if err := applyMetadata(ctx, tx, row.ID, domain.SourceTMDB, season.Metadata); err != nil {
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
					if err := applyMetadata(ctx, tx, e.ID, domain.SourceTMDB, said); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
