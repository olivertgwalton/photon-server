package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"time"
	"uuid"

	"gorm.io/gen/field"
	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// PlaylistSummary is a playlist as its list shows it: how many entries it has and how long they run.
type PlaylistSummary struct {
	ID         uuid.UUID
	Name       string
	Entries    int
	DurationMS int64
	UpdatedAt  time.Time
}

// PlaylistEntry is one entry of a playlist: its own id, and the title it plays.
type PlaylistEntry struct {
	ID   uuid.UUID
	Card Card
}

// Playlists answers a profile's playlists, by name.
func (s *Store) Playlists(ctx context.Context, profile uuid.UUID) ([]PlaylistSummary, error) {
	var out []PlaylistSummary
	err := s.q.Playlist.WithContext(ctx).UnderlyingDB().Raw(`
		SELECT p.id, p.name, p.updated_at, count(e.id) AS entries,
			coalesce(sum((SELECT max(v.duration_ms) FROM versions v WHERE v.item_id = e.item_id AND v.missing_since IS NULL)), 0) AS duration_ms
		FROM playlists p LEFT JOIN playlist_entries e ON e.playlist_id = p.id
		WHERE p.profile_id = ? GROUP BY p.id ORDER BY lower(p.name), p.id`, profile.String()).Scan(&out).Error
	return out, err
}

// AddPlaylist makes a profile's playlist of titles (see AddToPlaylist).
func (s *Store) AddPlaylist(ctx context.Context, profile uuid.UUID, name string, items []uuid.UUID) (uuid.UUID, error) {
	var id model.UUID
	err := s.q.Transaction(func(tx *query.Query) error {
		row := model.Playlist{ProfileID: model.UUID(profile), Name: name}
		if err := tx.Playlist.WithContext(ctx).Create(&row); err != nil {
			return err
		}
		id = row.ID
		return appendEntries(ctx, tx, row.ID, items)
	})
	return uuid.UUID(id), err
}

// PlaylistEntries answers a page of a profile's playlist, in its order, and how many entries it
// has. ErrNotFound for no such playlist of the profile's.
func (s *Store) PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]PlaylistEntry, int64, error) {
	if err := ownPlaylist(ctx, s.q, profile, playlist); err != nil {
		return nil, 0, err
	}
	e := s.q.PlaylistEntry
	// An entry the profile may no longer see, as a library taken from it, is passed over.
	q := e.WithContext(ctx).Where(e.PlaylistID.Eq(model.UUID(playlist))).
		Where(field.NewUnsafeFieldRaw("EXISTS (SELECT 1 FROM items i, viewer(?) v WHERE i.id = item_id AND sees(v, i))", profile.String()))
	total, err := q.Count()
	if err != nil {
		return nil, 0, err
	}
	entries, err := q.Order(e.Position, e.ID).Offset(offset).Limit(limit).Find()
	if err != nil {
		return nil, 0, err
	}
	in := make([]driver.Valuer, len(entries))
	for n, x := range entries {
		in[n] = x.ItemID
	}
	i := s.q.Item
	rows, err := i.WithContext(ctx).Where(i.ID.In(in...)).Find()
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, rows)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	out := make([]PlaylistEntry, len(entries))
	for n, x := range entries {
		out[n] = PlaylistEntry{ID: uuid.UUID(x.ID), Card: byID[uuid.UUID(x.ItemID)]}
	}
	return out, total, nil
}

// AddToPlaylist puts titles at the end of a profile's playlist, in order: a film or episode as it
// is, a show or season as its episodes, a collection as its titles.
func (s *Store) AddToPlaylist(ctx context.Context, profile, playlist uuid.UUID, items []uuid.UUID) error {
	return s.q.Transaction(func(tx *query.Query) error {
		if err := ownPlaylist(ctx, tx, profile, playlist); err != nil {
			return err
		}
		return appendEntries(ctx, tx, model.UUID(playlist), items)
	})
}

// RemoveFromPlaylist takes one entry out of a profile's playlist.
func (s *Store) RemoveFromPlaylist(ctx context.Context, profile, playlist, entry uuid.UUID) error {
	return s.q.Transaction(func(tx *query.Query) error {
		if err := ownPlaylist(ctx, tx, profile, playlist); err != nil {
			return err
		}
		e := tx.PlaylistEntry
		res, err := e.WithContext(ctx).Where(e.PlaylistID.Eq(model.UUID(playlist)), e.ID.Eq(model.UUID(entry))).Delete()
		if err == nil && res.RowsAffected == 0 {
			err = ErrNotFound
		}
		if err != nil {
			return err
		}
		return touch(ctx, tx, model.UUID(playlist))
	})
}

// MovePlaylistEntry moves one entry of a profile's playlist to a position, counted from zero; one
// past the end goes last.
func (s *Store) MovePlaylistEntry(ctx context.Context, profile, playlist, entry uuid.UUID, position int) error {
	return s.q.Transaction(func(tx *query.Query) error {
		if err := ownPlaylist(ctx, tx, profile, playlist); err != nil {
			return err
		}
		e := tx.PlaylistEntry
		entries, err := e.WithContext(ctx).Where(e.PlaylistID.Eq(model.UUID(playlist))).Order(e.Position, e.ID).Find()
		if err != nil {
			return err
		}
		from := slices.IndexFunc(entries, func(x *model.PlaylistEntry) bool { return x.ID == model.UUID(entry) })
		if from < 0 {
			return ErrNotFound
		}
		moved := entries[from]
		entries = slices.Delete(entries, from, from+1)
		entries = slices.Insert(entries, min(max(position, 0), len(entries)), moved)
		for n, x := range entries {
			if x.Position == n {
				continue
			}
			if _, err := e.WithContext(ctx).Where(e.ID.Eq(x.ID)).Update(e.Position, n); err != nil {
				return err
			}
		}
		return touch(ctx, tx, model.UUID(playlist))
	})
}

// RenamePlaylist renames a profile's playlist.
func (s *Store) RenamePlaylist(ctx context.Context, profile, playlist uuid.UUID, name string) error {
	p := s.q.Playlist
	res, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(playlist)), p.ProfileID.Eq(model.UUID(profile))).
		UpdateSimple(p.Name.Value(name))
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

// RemovePlaylist removes a profile's playlist.
func (s *Store) RemovePlaylist(ctx context.Context, profile, playlist uuid.UUID) error {
	p := s.q.Playlist
	res, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(playlist)), p.ProfileID.Eq(model.UUID(profile))).Delete()
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

func ownPlaylist(ctx context.Context, q *query.Query, profile, playlist uuid.UUID) error {
	p := q.Playlist
	_, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(playlist)), p.ProfileID.Eq(model.UUID(profile))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

func touch(ctx context.Context, tx *query.Query, playlist model.UUID) error {
	p := tx.Playlist
	_, err := p.WithContext(ctx).Where(p.ID.Eq(playlist)).Update(p.UpdatedAt, time.Now())
	return err
}

// appendEntries puts the films and episodes titles stand for after a playlist's last entry.
func appendEntries(ctx context.Context, tx *query.Query, playlist model.UUID, items []uuid.UUID) error {
	var playable []model.UUID
	for _, id := range items {
		more, err := playableOf(ctx, tx, model.UUID(id))
		if err != nil {
			return err
		}
		playable = append(playable, more...)
	}
	if len(playable) == 0 {
		return touch(ctx, tx, playlist)
	}
	var last struct{ Position *int }
	e := tx.PlaylistEntry
	if err := e.WithContext(ctx).Select(e.Position.Max().As("position")).Where(e.PlaylistID.Eq(playlist)).Scan(&last); err != nil {
		return err
	}
	next := 0
	if last.Position != nil {
		next = *last.Position + 1
	}
	rows := make([]*model.PlaylistEntry, len(playable))
	for n, id := range playable {
		rows[n] = &model.PlaylistEntry{PlaylistID: playlist, ItemID: id, Position: next + n}
	}
	if err := e.WithContext(ctx).Create(rows...); err != nil {
		return err
	}
	return touch(ctx, tx, playlist)
}

// playableOf answers the films and episodes a title stands for, in the order they play; ErrNotFound
// for no title, or one that plays nothing, such as an extra.
func playableOf(ctx context.Context, tx *query.Query, id model.UUID) ([]model.UUID, error) {
	i := tx.Item
	row, err := i.WithContext(ctx).Where(i.ID.Eq(id)).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var out []model.UUID
	db := i.WithContext(ctx).UnderlyingDB()
	switch row.Kind {
	case domain.ItemMovie, domain.ItemEpisode:
		return []model.UUID{row.ID}, nil
	case domain.ItemSeason:
		err = db.Raw(`SELECT id FROM items WHERE parent_id = ? AND kind = 'episode' ORDER BY episode_number, id`, row.ID).Scan(&out).Error
	case domain.ItemShow:
		err = db.Raw(`SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id
			WHERE s.parent_id = ? AND e.kind = 'episode'
			ORDER BY s.season_number = 0, s.season_number, e.episode_number, e.id`, row.ID).Scan(&out).Error
	case domain.ItemCollection:
		var members []model.UUID
		if err := db.Raw(`SELECT m.item_id FROM collection_members m JOIN collections c ON c.item_id = m.collection_id
			JOIN items i ON i.id = m.item_id WHERE m.collection_id = ?
			ORDER BY CASE WHEN c.origin = 'user' THEN m.position END, i.released_asc, i.id`, row.ID).Scan(&members).Error; err != nil {
			return nil, err
		}
		for _, m := range members {
			more, err := playableOf(ctx, tx, m)
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		}
	case domain.ItemExtra:
		return nil, ErrNotFound
	}
	return out, err
}
