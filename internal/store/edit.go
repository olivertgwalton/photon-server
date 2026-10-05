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

// EditMetadata writes what an admin says of a title. A reader's edit outranks every source, so it
// stands until it is reset; Locked fields are claimed even where nothing is written, so no source
// fills them, as Jellyfin's locked fields are.
func (s *Store) EditMetadata(ctx context.Context, id uuid.UUID, m domain.Metadata) error {
	return s.q.Transaction(func(tx *query.Query) error {
		if _, err := editable(ctx, tx, id); err != nil {
			return err
		}
		return applyMetadata(ctx, tx, model.UUID(id), domain.SourceUser, m)
	})
}

// ResetEdits gives fields an admin edited or locked back to the sources: its folder is read again
// at the next scan and it is matched again, so each field takes the best value a source has. No
// fields resets every one.
func (s *Store) ResetEdits(ctx context.Context, id uuid.UUID, fields []domain.Field) error {
	return s.q.Transaction(func(tx *query.Query) error {
		item, err := editable(ctx, tx, id)
		if err != nil {
			return err
		}
		f := tx.ItemField
		q := f.WithContext(ctx).Where(f.ItemID.Eq(item.ID), f.Source.Eq(string(domain.SourceUser)))
		if len(fields) > 0 {
			names := make([]string, len(fields))
			for n, x := range fields {
				names[n] = string(x)
			}
			q = q.Where(f.Field.In(names...))
		}
		if _, err := q.Delete(); err != nil {
			return err
		}
		return rematch(ctx, tx, item)
	})
}

// PinMatch fixes which title a provider matches a film or show to: the id is the admin's, above
// any an NFO or path gives, and the ids an earlier match led to are dropped so the match finds
// them again.
func (s *Store) PinMatch(ctx context.Context, id uuid.UUID, provider domain.Provider, value string) error {
	return s.q.Transaction(func(tx *query.Query) error {
		item, err := editable(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Kind != domain.ItemMovie && item.Kind != domain.ItemShow {
			return ErrNotFound
		}
		e := tx.ExternalID
		if _, err := e.WithContext(ctx).Where(e.ItemID.Eq(item.ID), e.Source.Eq(string(domain.IDFromMatch))).Delete(); err != nil {
			return err
		}
		if err := e.WithContext(ctx).Save(&model.ExternalID{ItemID: item.ID, Provider: provider, Value: value, Source: domain.IDFromUser}); err != nil {
			return err
		}
		return enqueue(ctx, tx, domain.JobIdentify, item.ID)
	})
}

// editable answers a title an admin edits, or ErrNotFound.
func editable(ctx context.Context, tx *query.Query, id uuid.UUID) (*model.Item, error) {
	i := tx.Item
	item, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return item, err
}

// rematch has a title's folder read again at the next scan, and its film or show matched again.
func rematch(ctx context.Context, tx *query.Query, item *model.Item) error {
	fo := tx.Folder
	if _, err := fo.WithContext(ctx).Where(fo.LibraryID.Eq(item.LibraryID), fo.Path.Eq(item.Folder)).Delete(); err != nil {
		return err
	}
	title := item
	for title.Kind == domain.ItemSeason || title.Kind == domain.ItemEpisode {
		if title.ParentID == nil {
			return nil
		}
		i := tx.Item
		parent, err := i.WithContext(ctx).Where(i.ID.Eq(*title.ParentID)).Take()
		if err != nil {
			return err
		}
		title = parent
	}
	if title.Kind == domain.ItemMovie || title.Kind == domain.ItemShow {
		if err := enqueue(ctx, tx, domain.JobIdentify, title.ID); err != nil {
			return err
		}
	}
	return enqueueAfter(ctx, tx, domain.JobScanLibrary, item.LibraryID, 0)
}
