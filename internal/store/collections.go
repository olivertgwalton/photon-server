package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"strconv"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// ErrNotUserCollection is a collection a provider made, which only that provider changes.
var ErrNotUserCollection = errors.New("only a collection an admin made is changed by hand")

// minShown is how many titles a provider's collection must hold before it is shown, as Plex's
// minimum automatic collection size: a box set of one is no set.
const minShown = 2

// groupingProviders are the providers whose box sets become collections, by the id they file them
// under.
var groupingProviders = map[domain.FieldSource]domain.Provider{domain.SourceTMDB: domain.ProviderTMDB}

// saveGroupings puts a title in the box sets a source names it part of, making each the first
// time, and takes it out of that source's others.
func saveGroupings(ctx context.Context, tx *query.Query, item model.UUID, source domain.FieldSource, groupings []domain.Grouping) error {
	by, ok := groupingProviders[source]
	if !ok {
		return nil
	}
	i, c, cm := tx.Item, tx.Collection, tx.CollectionMember
	title, err := i.WithContext(ctx).Where(i.ID.Eq(item)).Take()
	if err != nil {
		return err
	}
	var keep []model.UUID
	for _, g := range groupings {
		var found []model.UUID
		err := tx.Item.WithContext(ctx).UnderlyingDB().Raw(`
			SELECT c.item_id FROM collections c
			JOIN items i ON i.id = c.item_id AND i.library_id = ?
			JOIN external_ids e ON e.item_id = c.item_id AND e.provider = ? AND e.value = ?
			WHERE c.origin = ?`, title.LibraryID, by, g.ID, source).Scan(&found).Error
		if err != nil {
			return err
		}
		var id model.UUID
		if len(found) > 0 {
			id = found[0]
		} else {
			row := model.Item{LibraryID: title.LibraryID, Kind: domain.ItemCollection, Title: g.Title, ScanTitle: g.Title, SortTitle: sortTitle(g.Title)}
			if err := i.WithContext(ctx).Create(&row); err != nil {
				return err
			}
			id = row.ID
			if err := c.WithContext(ctx).Create(&model.Collection{ItemID: id, Origin: domain.CollectionOrigin(source)}); err != nil {
				return err
			}
			if err := saveIDs(ctx, tx, id, domain.IDFromMatch, map[domain.Provider]string{by: g.ID}); err != nil {
				return err
			}
			if err := keyTitle(ctx, tx, id); err != nil {
				return err
			}
		}
		if err := applyMetadata(ctx, tx, id, source, domain.Metadata{Title: g.Title}); err != nil {
			return err
		}
		if err := saveProviderArtwork(ctx, tx, id, source, g.Artwork); err != nil {
			return err
		}
		if err := cm.WithContext(ctx).Save(&model.CollectionMember{CollectionID: id, ItemID: item}); err != nil {
			return err
		}
		keep = append(keep, id)
	}
	// Out of the source's sets it no longer names; one left empty goes at the next scan.
	gone := `DELETE FROM collection_members m USING collections c
		WHERE m.collection_id = c.item_id AND c.origin = ? AND m.item_id = ?`
	if len(keep) == 0 {
		return tx.Item.WithContext(ctx).UnderlyingDB().Exec(gone, source, item).Error
	}
	return tx.Item.WithContext(ctx).UnderlyingDB().Exec(gone+" AND m.collection_id NOT IN ?", source, item, keep).Error
}

// shownCollections are a library's collections worth showing: an admin's, and a provider's once
// it holds minShown titles.
var shownCollections = `SELECT c.item_id FROM collections c
	WHERE c.origin = 'user' OR (SELECT count(*) FROM collection_members m WHERE m.collection_id = c.item_id) >= ` + strconv.Itoa(minShown)

// Collections answers a page of a library's collections, by title, and how many there are.
func (s *Store) Collections(ctx context.Context, lib, profile uuid.UUID, offset, limit int) ([]Card, int64, error) {
	l := s.q.Library
	if _, err := l.WithContext(ctx).Where(l.ID.Eq(model.UUID(lib))).Take(); err != nil {
		return nil, 0, found(err)
	}
	q := s.q.Item.WithContext(ctx).UnderlyingDB().
		Where("items.library_id = ? AND items.kind = 'collection' AND items.id IN ("+shownCollections+")", lib.String()).
		Where("EXISTS (SELECT 1 FROM viewer(?) v WHERE sees(v, items))", profile.String())
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.Item
	if err := q.Order("items.sort_title, items.id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, rows)
	return cards, total, err
}

// Members answers a collection's titles: an admin's in the order they were put, a provider's from
// the first released. ErrNotFound for no such collection.
func (s *Store) Members(ctx context.Context, profile, collection uuid.UUID) ([]Card, error) {
	c := s.q.Collection
	row, err := c.WithContext(ctx).Where(c.ItemID.Eq(model.UUID(collection))).Take()
	if err != nil {
		return nil, found(err)
	}
	order := "items.released_asc, items.sort_title, items.id"
	if row.Origin == domain.CollectionUser {
		order = "m.position, items.id"
	}
	var rows []*model.Item
	err = s.q.Item.WithContext(ctx).UnderlyingDB().
		Joins("JOIN collection_members m ON m.item_id = items.id AND m.collection_id = ?", collection.String()).
		Where("EXISTS (SELECT 1 FROM viewer(?) v WHERE sees(v, items))", profile.String()).
		Order(order).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}

// origins answers who made each collection among rows.
func (s *Store) origins(ctx context.Context, rows []*model.Item) (map[model.UUID]domain.CollectionOrigin, error) {
	out := map[model.UUID]domain.CollectionOrigin{}
	var in []driver.Valuer
	for _, r := range rows {
		if r.Kind == domain.ItemCollection {
			in = append(in, r.ID)
		}
	}
	if len(in) == 0 {
		return out, nil
	}
	c := s.q.Collection
	found, err := c.WithContext(ctx).Where(c.ItemID.In(in...)).Find()
	for _, f := range found {
		out[f.ItemID] = f.Origin
	}
	return out, err
}

// collectionsOf answers the shown collections a title is in, by title.
func (s *Store) collectionsOf(ctx context.Context, item model.UUID) ([]CollectionCard, error) {
	var rows []*model.Item
	err := s.q.Item.WithContext(ctx).UnderlyingDB().
		Where("items.id IN (SELECT collection_id FROM collection_members WHERE item_id = ?) AND items.id IN ("+shownCollections+")", item).
		Order("items.sort_title").Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	pictures, err := s.pictureOrder(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]CollectionCard, len(rows))
	for n, r := range rows {
		out[n] = CollectionCard{ID: uuid.UUID(r.ID), Title: r.Title, Poster: first(pictures[r.ID][domain.ArtworkPoster])}
	}
	return out, nil
}

// AddCollection makes an admin's collection in a library.
func (s *Store) AddCollection(ctx context.Context, lib uuid.UUID, title string) (uuid.UUID, error) {
	var id model.UUID
	err := s.q.Transaction(func(tx *query.Query) error {
		l := tx.Library
		if _, err := l.WithContext(ctx).Where(l.ID.Eq(model.UUID(lib))).Take(); err != nil {
			return found(err)
		}
		row := model.Item{LibraryID: model.UUID(lib), Kind: domain.ItemCollection, Title: title, ScanTitle: title, SortTitle: sortTitle(title)}
		if err := tx.Item.WithContext(ctx).Create(&row); err != nil {
			return err
		}
		id = row.ID
		if err := tx.Collection.WithContext(ctx).Create(&model.Collection{ItemID: id, Origin: domain.CollectionUser}); err != nil {
			return err
		}
		return applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title})
	})
	return uuid.UUID(id), err
}

// SetMembers replaces an admin's collection's titles with these, in this order. Each must be a
// film or show of the collection's library.
func (s *Store) SetMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error {
	return s.q.Transaction(func(tx *query.Query) error {
		lib, err := userCollection(ctx, tx, collection)
		if err != nil {
			return err
		}
		i := tx.Item
		ids := make([]model.UUID, len(items))
		in := make([]driver.Valuer, len(items))
		for n, id := range items {
			ids[n], in[n] = model.UUID(id), model.UUID(id)
		}
		if hasRepeats(ids) {
			return ErrNotFound
		}
		titles, err := i.WithContext(ctx).Where(i.LibraryID.Eq(lib), i.Kind.In(string(domain.ItemMovie), string(domain.ItemShow))).
			Where(i.ID.In(in...)).Count()
		if err != nil {
			return err
		}
		if int(titles) != len(ids) {
			return ErrNotFound
		}
		cm := tx.CollectionMember
		if _, err := cm.WithContext(ctx).Where(cm.CollectionID.Eq(model.UUID(collection))).Delete(); err != nil {
			return err
		}
		rows := make([]*model.CollectionMember, len(ids))
		for n, id := range ids {
			rows[n] = &model.CollectionMember{CollectionID: model.UUID(collection), ItemID: id, Position: n}
		}
		if len(rows) == 0 {
			return nil
		}
		return cm.WithContext(ctx).Create(rows...)
	})
}

// RemoveCollection removes an admin's collection; its titles are left as they are.
func (s *Store) RemoveCollection(ctx context.Context, collection uuid.UUID) error {
	return s.q.Transaction(func(tx *query.Query) error {
		if _, err := userCollection(ctx, tx, collection); err != nil {
			return err
		}
		_, err := tx.Item.WithContext(ctx).Where(tx.Item.ID.Eq(model.UUID(collection))).Delete()
		return err
	})
}

// userCollection answers the library of an admin's collection; ErrNotFound for no collection, and
// ErrNotUserCollection for a provider's.
func userCollection(ctx context.Context, tx *query.Query, collection uuid.UUID) (model.UUID, error) {
	c, i := tx.Collection, tx.Item
	row, err := c.WithContext(ctx).Where(c.ItemID.Eq(model.UUID(collection))).Take()
	if err != nil {
		return model.UUID{}, found(err)
	}
	if row.Origin != domain.CollectionUser {
		return model.UUID{}, ErrNotUserCollection
	}
	item, err := i.WithContext(ctx).Where(i.ID.Eq(row.ItemID)).Take()
	return item.LibraryID, err
}

func hasRepeats(ids []model.UUID) bool {
	seen := map[model.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}
