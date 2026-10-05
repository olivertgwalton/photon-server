package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"
	"uuid"

	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// TitleState is what a profile has made of a title. A show's and a season's are their episodes':
// watched once every one is, with how many are left.
type TitleState struct {
	PositionMS   int64      `json:"position_ms,omitzero"`
	Plays        int        `json:"plays,omitzero"`
	WatchedAt    *time.Time `json:"watched_at,omitzero"`
	LastPlayedAt *time.Time `json:"last_played_at,omitzero"`
	FavouriteAt  *time.Time `json:"favourite_at,omitzero"`
	Unwatched    int        `json:"unwatched,omitzero"`
}

// SaveProgress records that a profile stopped a film or episode at position, and answers how far
// that got: too near the start to keep, somewhere to resume, or far enough to count as watched.
func (s *Store) SaveProgress(ctx context.Context, profile, item uuid.UUID, position time.Duration) (domain.Reach, error) {
	i := s.q.Item
	row, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(item))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	lengths, err := s.durations(ctx, ids([]*model.Item{row}))
	if err != nil {
		return "", err
	}
	reach := domain.ReachOf(position, time.Duration(lengths[row.ID])*time.Millisecond)
	if reach == domain.ReachEnd {
		return reach, s.watched(ctx, profile, []*model.Item{row})
	}
	keep := int64(0)
	if reach == domain.ReachResumable {
		keep = position.Milliseconds()
	}
	err = s.q.WatchState.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "profile_id"}, {Name: "item_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"position_ms", "last_played_at"}),
	}).Create(&model.WatchState{
		ProfileID: model.UUID(profile), ItemID: row.ID, PositionMS: keep, LastPlayedAt: new(time.Now()),
	})
	return reach, err
}

// MarkWatched marks a film or episode watched, or every episode of a season or show.
func (s *Store) MarkWatched(ctx context.Context, profile, item uuid.UUID) error {
	leaves, err := s.leaves(ctx, item)
	if err != nil {
		return err
	}
	return s.watched(ctx, profile, leaves)
}

func (s *Store) watched(ctx context.Context, profile uuid.UUID, items []*model.Item) error {
	if len(items) == 0 {
		return nil
	}
	// A play counted adds to the plays before it, which GORM's upsert cannot say.
	return s.q.WatchState.WithContext(ctx).UnderlyingDB().Exec(`
		INSERT INTO watch_state (profile_id, item_id, plays, watched_at, last_played_at)
		SELECT ?, id, 1, now(), now() FROM items WHERE id IN ?
		ON CONFLICT (profile_id, item_id) DO UPDATE SET
			position_ms = 0, plays = watch_state.plays + 1, watched_at = now(), last_played_at = now()`,
		model.UUID(profile), ids(items)).Error
}

// MarkUnwatched forgets that a film or episode, or every episode of a season or show, was
// watched, and where it stopped.
func (s *Store) MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error {
	w := s.q.WatchState
	return s.setLeaves(ctx, profile, item, w.WatchedAt.Null(), w.PositionMS.Value(0))
}

// ClearProgress forgets where a film or episode, or each episode of a season or show, stopped,
// taking it out of Continue Watching; whether it was watched, and its plays, stay.
func (s *Store) ClearProgress(ctx context.Context, profile, item uuid.UUID) error {
	return s.setLeaves(ctx, profile, item, s.q.WatchState.PositionMS.Value(0))
}

func (s *Store) setLeaves(ctx context.Context, profile, item uuid.UUID, set ...field.AssignExpr) error {
	leaves, err := s.leaves(ctx, item)
	if err != nil || len(leaves) == 0 {
		return err
	}
	w := s.q.WatchState
	_, err = w.WithContext(ctx).Where(w.ProfileID.Eq(model.UUID(profile)), w.ItemID.In(ids(leaves)...)).UpdateSimple(set...)
	return err
}

func (s *Store) Favourite(ctx context.Context, profile, item uuid.UUID) error {
	if _, err := s.leaves(ctx, item); err != nil {
		return err
	}
	return s.q.Favourite.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.Favourite{ProfileID: model.UUID(profile), ItemID: model.UUID(item)})
}

func (s *Store) Unfavourite(ctx context.Context, profile, item uuid.UUID) error {
	f := s.q.Favourite
	_, err := f.WithContext(ctx).Where(f.ProfileID.Eq(model.UUID(profile)), f.ItemID.Eq(model.UUID(item))).Delete()
	return err
}

// leaves answers the films or episodes a title is watched by: itself, or a season's or show's
// episodes. ErrNotFound for no title.
func (s *Store) leaves(ctx context.Context, id uuid.UUID) ([]*model.Item, error) {
	i := s.q.Item
	row, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	parents := []*model.Item{row}
	switch row.Kind {
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		return parents, nil
	case domain.ItemShow:
		if parents, err = i.WithContext(ctx).Where(i.ParentID.Eq(row.ID), i.Kind.Eq(string(domain.ItemSeason))).Find(); err != nil {
			return nil, err
		}
	case domain.ItemSeason:
	case domain.ItemCollection:
		// A box set marked is each of its titles marked, as Jellyfin's is.
		var leaves []*model.Item
		err := i.WithContext(ctx).UnderlyingDB().Raw(`
			SELECT e.* FROM items e JOIN collection_members m ON m.collection_id = ?
			WHERE (e.id = m.item_id AND e.kind = 'movie')
				OR (e.kind = 'episode' AND e.parent_id IN (SELECT s.id FROM items s WHERE s.parent_id = m.item_id))`,
			row.ID).Scan(&leaves).Error
		return leaves, err
	}
	return i.WithContext(ctx).Where(i.ParentID.In(ids(parents)...), i.Kind.Eq(string(domain.ItemEpisode))).Find()
}

// states answers what a profile has made of each title.
func (s *Store) states(ctx context.Context, profile uuid.UUID, items []*model.Item) (map[model.UUID]TitleState, error) {
	out := map[model.UUID]TitleState{}
	if len(items) == 0 {
		return out, nil
	}
	w, f := s.q.WatchState, s.q.Favourite
	p := model.UUID(profile)
	own, err := w.WithContext(ctx).Where(w.ProfileID.Eq(p), w.ItemID.In(ids(items)...)).Find()
	if err != nil {
		return nil, err
	}
	for _, r := range own {
		st := TitleState{PositionMS: r.PositionMS, Plays: r.Plays, WatchedAt: r.WatchedAt, LastPlayedAt: r.LastPlayedAt}
		out[r.ItemID] = st
	}
	var groups []*model.Item
	for _, it := range items {
		if it.Kind == domain.ItemShow || it.Kind == domain.ItemSeason {
			groups = append(groups, it)
		}
	}
	if len(groups) > 0 {
		// gen cannot count down two levels of episodes, so this one query is SQL.
		var counts []struct {
			ID         model.UUID
			Episodes   int
			Watched    int
			LastPlayed *time.Time
			WatchedAt  *time.Time
		}
		err := w.WithContext(ctx).UnderlyingDB().Raw(`
			SELECT g.id, count(e.id) AS episodes, count(ws.watched_at) AS watched,
				max(ws.last_played_at) AS last_played, max(ws.watched_at) AS watched_at
			FROM items g
			JOIN items e ON e.kind = 'episode'
				AND (e.parent_id = g.id OR e.parent_id IN (SELECT id FROM items s WHERE s.parent_id = g.id))
			LEFT JOIN watch_state ws ON ws.item_id = e.id AND ws.profile_id = ?
			WHERE g.id IN ?
			GROUP BY g.id`, p, ids(groups)).Scan(&counts).Error
		if err != nil {
			return nil, err
		}
		for _, c := range counts {
			st := TitleState{Unwatched: c.Episodes - c.Watched, LastPlayedAt: c.LastPlayed}
			if st.Unwatched == 0 {
				st.WatchedAt = c.WatchedAt
			}
			out[c.ID] = st
		}
	}
	favs, err := f.WithContext(ctx).Where(f.ProfileID.Eq(p), f.ItemID.In(ids(items)...)).Find()
	if err != nil {
		return nil, err
	}
	for _, r := range favs {
		st := out[r.ItemID]
		st.FavouriteAt = &r.AddedAt
		out[r.ItemID] = st
	}
	return out, nil
}

// RecordPlay keeps a playback in the history as it stops.
func (s *Store) RecordPlay(ctx context.Context, p domain.Playback, stopped time.Time, position time.Duration) error {
	row := model.Play{
		ProfileID: model.UUID(p.Profile), ItemID: model.UUID(p.Item), Method: p.Method,
		StartedAt: p.Started, StoppedAt: stopped, PositionMS: position.Milliseconds(),
	}
	if p.Version != (uuid.UUID{}) {
		row.VersionID = new(model.UUID(p.Version))
	}
	err := s.q.Play.WithContext(ctx).Create(&row)
	// A title removed while it played leaves nothing to keep.
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return nil
	}
	return err
}

// Play is one playback in the history: who, of what, how, when and how far.
type Play struct {
	ID         uuid.UUID
	Profile    uuid.UUID
	Card       Card
	Method     domain.PlayMethod
	StartedAt  time.Time
	StoppedAt  time.Time
	PositionMS int64
}

// History answers a page of plays, the latest first, and how many there are: a profile's, or
// everyone's for none.
func (s *Store) History(ctx context.Context, profile uuid.UUID, offset, limit int) ([]Play, int64, error) {
	p := s.q.Play
	q := p.WithContext(ctx)
	if profile != (uuid.UUID{}) {
		q = q.Where(p.ProfileID.Eq(model.UUID(profile)))
	}
	total, err := q.Count()
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.Order(p.StoppedAt.Desc(), p.ID.Desc()).Offset(offset).Limit(limit).Find()
	if err != nil || len(rows) == 0 {
		return []Play{}, total, err
	}
	in := make([]driver.Valuer, len(rows))
	for n, r := range rows {
		in[n] = r.ItemID
	}
	i := s.q.Item
	items, err := i.WithContext(ctx).Where(i.ID.In(in...)).Find()
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, items)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	out := make([]Play, len(rows))
	for n, r := range rows {
		out[n] = Play{
			ID: uuid.UUID(r.ID), Profile: uuid.UUID(r.ProfileID), Card: byID[uuid.UUID(r.ItemID)], Method: r.Method,
			StartedAt: r.StartedAt, StoppedAt: r.StoppedAt, PositionMS: r.PositionMS,
		}
	}
	return out, total, nil
}
