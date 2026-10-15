package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// inboxLength is the most notifications an inbox answers, newest first; older ones are kept, to
// tell nothing twice, but not shown.
const inboxLength = 100

// notify tells each profile of the films and episodes added to a library since a scan began, as
// its notify asks, of those it may see and was never told: a profile's marked shows are those it
// listed, favoured or played any of, and an episode before the furthest it has watched of its show
// is old news, as a film it has watched is. A library that had no titles before is being filled,
// not added to, and tells no one. It answers the profiles told.
const notify = `
	WITH added AS (
		SELECT i AS item, i.id AS item_id, i.kind, i.season_number, i.episode_number,
			CASE i.kind WHEN 'episode' THEN season.parent_id ELSE i.id END AS title_id
		FROM items i LEFT JOIN items season ON season.id = i.parent_id AND i.kind = 'episode'
		WHERE i.library_id = @library AND i.kind IN ('movie', 'episode') AND i.added_at >= @since
			AND EXISTS (SELECT 1 FROM items o WHERE o.library_id = @library AND o.kind IN ('movie', 'episode')
				AND o.added_at < @since)
	), shows AS (
		SELECT DISTINCT title_id AS id FROM added WHERE kind = 'episode'
	), family AS (
		SELECT id AS show_id, id AS item_id FROM shows
		UNION ALL
		SELECT s.id, season.id FROM shows s JOIN items season ON season.parent_id = s.id
		UNION ALL
		SELECT s.id, e.id FROM shows s JOIN items season ON season.parent_id = s.id JOIN items e ON e.parent_id = season.id
	), marks AS (
		SELECT f.show_id, f.item_id, m.profile_id, m.watched FROM family f CROSS JOIN LATERAL (
			SELECT profile_id, false AS watched FROM watchlist WHERE item_id = f.item_id
			UNION ALL SELECT profile_id, false FROM favourites WHERE item_id = f.item_id
			UNION ALL SELECT profile_id, watched_at IS NOT NULL FROM watch_state WHERE item_id = f.item_id
		) m
	), marked AS (
		SELECT DISTINCT profile_id, show_id FROM marks
	), furthest AS (
		SELECT DISTINCT ON (m.profile_id, m.show_id) m.profile_id, m.show_id, x.season_number, x.episode_number
		FROM marks m JOIN items x ON x.id = m.item_id AND x.kind = 'episode'
		WHERE m.watched
		ORDER BY m.profile_id, m.show_id, x.season_number DESC, x.episode_number DESC
	), fresh AS (
		SELECT p.id AS profile_id, a.title_id, a.item_id
		FROM added a CROSS JOIN profiles p
		LEFT JOIN profile_preferences pp ON pp.profile_id = p.id
		LEFT JOIN furthest fu ON fu.profile_id = p.id AND fu.show_id = a.title_id
		CROSS JOIN LATERAL viewer(p.id) v
		WHERE CASE coalesce(pp.notify, 'marked')
				WHEN 'all' THEN true
				WHEN 'marked' THEN a.kind = 'episode'
					AND EXISTS (SELECT 1 FROM marked m WHERE m.profile_id = p.id AND m.show_id = a.title_id)
				ELSE false END
			AND (fu.profile_id IS NULL OR (a.season_number, a.episode_number) > (fu.season_number, fu.episode_number))
			AND NOT EXISTS (SELECT 1 FROM watch_state w
				WHERE w.profile_id = p.id AND w.item_id = a.item_id AND w.watched_at IS NOT NULL)
			AND sees(v, a.item)
			AND NOT EXISTS (SELECT 1 FROM quiet_libraries q WHERE q.profile_id = p.id AND q.library_id = @library)
			AND NOT EXISTS (SELECT 1 FROM notification_items n WHERE n.item_id = a.item_id AND n.profile_id = p.id)
	), kept AS (
		INSERT INTO notifications (profile_id, title_id)
		SELECT DISTINCT profile_id, title_id FROM fresh
		ON CONFLICT (profile_id, title_id) WHERE read_at IS NULL DO UPDATE SET added_at = now()
		RETURNING id, profile_id, title_id
	), told AS (
		INSERT INTO notification_items (item_id, profile_id, notification_id)
		SELECT f.item_id, f.profile_id, k.id FROM fresh f JOIN kept k USING (profile_id, title_id)
		ON CONFLICT DO NOTHING
	)
	SELECT profile_id, array_agg(id) AS ids FROM kept GROUP BY profile_id`

// Told is a profile told of titles added, and its notifications added or gathering more.
type Told struct {
	ProfileID uuid.UUID
	IDs       []int64
}

// Notify tells each profile that cares of the films and episodes a scan of a library added since
// it began, answering the profiles told.
func (s *Store) Notify(ctx context.Context, library uuid.UUID, since time.Time) ([]Told, error) {
	return queryStructs[Told](ctx, s.pool, notify, pgx.NamedArgs{"library": library, "since": since})
}

// Inbox is a profile's notifications, newest first, and how many are unread.
type Inbox struct {
	Notifications []domain.Notification
	Unread        int
}

// told are a notification's films or episodes that a profile still may see: how many, and the
// newest in its show.
const told = `
	CROSS JOIN LATERAL (
		SELECT count(*)::int AS new,
			(array_agg(i.id ORDER BY i.season_number DESC NULLS LAST, i.episode_number DESC NULLS LAST))[1] AS latest
		FROM notification_items ni JOIN items i ON i.id = ni.item_id
		WHERE ni.notification_id = n.id AND sees(v, i)
	) t`

// Inbox answers a profile's notifications of what it still may see: the newest, or those of ids.
func (s *Store) Inbox(ctx context.Context, profile uuid.UUID, ids []int64) (Inbox, error) {
	type row struct {
		ID      int64
		TitleID uuid.UUID
		Latest  uuid.UUID
		New     int
		AddedAt time.Time
		ReadAt  *time.Time
	}
	rows, err := queryStructs[row](ctx, s.pool, `
		SELECT n.id, n.title_id, t.latest, t.new, n.added_at, n.read_at
		FROM notifications n CROSS JOIN viewer($1) v`+told+`
		WHERE n.profile_id = $1 AND t.new > 0 AND ($3::bigint[] IS NULL OR n.id = ANY($3))
		ORDER BY n.added_at DESC, n.id DESC LIMIT $2`, profile, inboxLength, ids)
	if err != nil {
		return Inbox{}, err
	}
	out := Inbox{Notifications: make([]domain.Notification, len(rows))}
	for i, r := range rows {
		out.Notifications[i] = domain.Notification{ID: r.ID, Title: r.TitleID, Latest: r.Latest, New: r.New, AddedAt: r.AddedAt, ReadAt: r.ReadAt}
	}
	out.Unread, err = s.unread(ctx, profile)
	return out, err
}

// unread counts a profile's unread notifications of what it still may see.
func (s *Store) unread(ctx context.Context, profile uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM notifications n CROSS JOIN viewer($1) v
		WHERE n.profile_id = $1 AND n.read_at IS NULL AND EXISTS (
			SELECT 1 FROM notification_items ni JOIN items i ON i.id = ni.item_id
			WHERE ni.notification_id = n.id AND sees(v, i))`, profile).Scan(&n)
	return n, err
}

// ReadNotification marks one of a profile's notifications read, answering how many are left
// unread. ErrNotFound for none of its own.
func (s *Store) ReadNotification(ctx context.Context, profile uuid.UUID, id int64) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications SET read_at = coalesce(read_at, now()) WHERE id = $1 AND profile_id = $2`, id, profile)
	if err == nil && tag.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return s.unread(ctx, profile)
}

// ReadNotifications marks a profile's notifications added by a time read, so one added after the
// inbox was read stays unread, answering how many are left unread.
func (s *Store) ReadNotifications(ctx context.Context, profile uuid.UUID, through time.Time) (int, error) {
	_, err := s.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now() WHERE profile_id = $1 AND read_at IS NULL AND added_at <= $2`,
		profile, through)
	if err != nil {
		return 0, err
	}
	return s.unread(ctx, profile)
}
