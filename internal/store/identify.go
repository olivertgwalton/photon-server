package store

import (
	"context"
	"errors"
	"slices"
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
	// Order is the order a show's episode files are numbered in.
	Order domain.EpisodeOrder
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
	sub := Subject{Kind: item.Kind, Title: item.Title, IDs: map[domain.Provider]string{}, Order: item.EpisodeOrder}
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
		if err := saveRemoteVideos(ctx, tx, item, source, m.Videos); err != nil {
			return err
		}
		if err := saveProviderArtwork(ctx, tx, item, source, m.Artwork); err != nil {
			return err
		}
		if err := saveRatings(ctx, tx, item, source, m.Ratings); err != nil {
			return err
		}
		if err := saveGroupings(ctx, tx, item, source, m.Collections); err != nil {
			return err
		}
		if err := saveCredits(ctx, tx, item, source, m.Credits); err != nil {
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
			if err := saveProviderArtwork(ctx, tx, row.ID, source, season.Metadata.Artwork); err != nil {
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
					if err := saveProviderArtwork(ctx, tx, e.ID, source, said.Artwork); err != nil {
						return err
					}
					if err := saveCredits(ctx, tx, e.ID, source, said.Credits); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// saveRemoteVideos replaces what a provider links to for a title with the videos it links to now,
// of the kinds the title's library keeps.
func saveRemoteVideos(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, videos []domain.RemoteVideo) error {
	rv, ex, i := tx.RemoteVideo, tx.LibraryRemoteExtra, tx.Item
	if _, err := rv.WithContext(ctx).Where(rv.ItemID.Eq(item), rv.Source.Eq(string(source))).Delete(); err != nil {
		return err
	}
	var kept []domain.ExtraKind
	err := ex.WithContext(ctx).Select(ex.Kind).Join(i, i.LibraryID.EqCol(ex.LibraryID)).Where(i.ID.Eq(item)).Scan(&kept)
	if err != nil {
		return err
	}
	var rows []*model.RemoteVideo
	for _, v := range videos {
		if !slices.Contains(kept, v.Kind) {
			continue
		}
		row := &model.RemoteVideo{
			ItemID: item, Source: source, Position: len(rows), Kind: v.Kind, Site: v.Site, Key: v.Key, Name: v.Name,
			Language: optional(v.Language),
		}
		if !v.Published.IsZero() {
			row.PublishedAt = &v.Published
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	return rv.WithContext(ctx).Create(rows...)
}
