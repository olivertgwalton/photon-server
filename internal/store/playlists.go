package store

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// PlaylistSummary is a playlist as its list shows it: how many entries it has and how long they run.
type PlaylistSummary struct {
	ID   uuid.UUID
	Name string
	// Library and Rule are a smart playlist's: the library whose wall its rule reads, and the rule
	// its entries are what of. Both are zero for a playlist made by hand, which holds its entries.
	Library    uuid.UUID
	Rule       *SmartRule
	Entries    int
	DurationMS int64
	UpdatedAt  time.Time
}

// PlaylistEntry is one entry of a playlist: its own id, and the title it plays. A smart playlist's
// entries are its rule's findings rather than rows of its own, so each has no id.
type PlaylistEntry struct {
	ID   uuid.UUID
	Card Card
}

// playlistRow is a playlist as its table holds it, its library and rule unset where it has none.
type playlistRow struct {
	ID         uuid.UUID
	Name       string
	LibraryID  *uuid.UUID
	Rule       *SmartRule
	Entries    int
	DurationMS int64
	UpdatedAt  time.Time
}

// Playlists answers a profile's playlists, by name.
func (s *Store) Playlists(ctx context.Context, profile uuid.UUID) ([]PlaylistSummary, error) {
	rows, err := queryStructs[playlistRow](ctx, s.pool, `
		SELECT p.id, p.name, p.library_id, p.rule, p.updated_at, count(e.id) AS entries,
			coalesce(sum((SELECT max(v.duration_ms) FROM versions v WHERE v.item_id = e.item_id AND v.missing_since IS NULL)), 0) AS duration_ms
		FROM playlists p LEFT JOIN playlist_entries e ON e.playlist_id = p.id
		WHERE p.profile_id = $1 GROUP BY p.id ORDER BY lower(p.name), p.id`, profile)
	if err != nil {
		return nil, err
	}
	out := make([]PlaylistSummary, len(rows))
	for n, r := range rows {
		out[n] = PlaylistSummary{
			ID: r.ID, Name: r.Name, Library: deref(r.LibraryID), Rule: r.Rule,
			Entries: r.Entries, DurationMS: r.DurationMS, UpdatedAt: r.UpdatedAt,
		}
		if r.Rule == nil {
			continue
		}
		// A smart playlist holds no entries of its own, so what its rule finds now is counted for
		// each; a profile has a handful of playlists, not a wall of them.
		if out[n].Entries, out[n].DurationMS, err = s.smartSize(ctx, profile, out[n].Library, *r.Rule); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AddPlaylist makes a profile's playlist of titles (see AddToPlaylist).
func (s *Store) AddPlaylist(ctx context.Context, profile uuid.UUID, name string, items []uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO playlists (profile_id, name) VALUES ($1, $2) RETURNING id`, profile, name).Scan(&id)
		if err != nil {
			return err
		}
		return appendEntries(ctx, tx, id, items)
	})
	return id, err
}

// PlaylistEntries answers a page of a profile's playlist, in its order, and how many entries it
// has: a smart playlist's are what its rule finds now. ErrNotFound for no such playlist of the
// profile's.
func (s *Store) PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]PlaylistEntry, int64, error) {
	lib, rule, err := ownPlaylist(ctx, s.pool, profile, playlist)
	if err != nil {
		return nil, 0, err
	}
	if rule != nil {
		return s.smartEntries(ctx, profile, lib, *rule, offset, limit)
	}
	// An entry the profile may no longer see, as a library taken from it, is passed over.
	const seen = `FROM playlist_entries e WHERE e.playlist_id = $1
		AND EXISTS (SELECT 1 FROM items i, viewer($2) v WHERE i.id = e.item_id AND sees(v, i))`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+seen, playlist, profile).Scan(&total); err != nil {
		return nil, 0, err
	}
	entries, err := queryRows[model.PlaylistEntry](ctx, s.pool, `
		SELECT e.id, e.item_id, e.position `+seen+` ORDER BY e.position, e.id OFFSET $3 LIMIT $4`,
		playlist, profile, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	in := make([]uuid.UUID, len(entries))
	for n, x := range entries {
		in[n] = x.ItemID
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id = ANY($1)`, in)
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
		out[n] = PlaylistEntry{ID: x.ID, Card: byID[x.ItemID]}
	}
	return out, total, nil
}

// AddToPlaylist puts titles at the end of a profile's playlist, in order: a film or episode as it
// is, a show or season as its episodes, a collection as its titles.
func (s *Store) AddToPlaylist(ctx context.Context, profile, playlist uuid.UUID, items []uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := byHand(ctx, tx, profile, playlist); err != nil {
			return err
		}
		return appendEntries(ctx, tx, playlist, items)
	})
}

// RemoveFromPlaylist takes one entry out of a profile's playlist.
func (s *Store) RemoveFromPlaylist(ctx context.Context, profile, playlist, entry uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := byHand(ctx, tx, profile, playlist); err != nil {
			return err
		}
		if err := affected(tx.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id = $1 AND id = $2`, playlist, entry)); err != nil {
			return err
		}
		return touch(ctx, tx, playlist)
	})
}

// MovePlaylistEntry moves one entry of a profile's playlist to a position, counted from zero; one
// past the end goes last.
func (s *Store) MovePlaylistEntry(ctx context.Context, profile, playlist, entry uuid.UUID, position int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := byHand(ctx, tx, profile, playlist); err != nil {
			return err
		}
		entries, err := queryRows[model.PlaylistEntry](ctx, tx, `
			SELECT id, item_id, position FROM playlist_entries WHERE playlist_id = $1 ORDER BY position, id`,
			playlist)
		if err != nil {
			return err
		}
		from := slices.IndexFunc(entries, func(x *model.PlaylistEntry) bool { return x.ID == entry })
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
			if _, err := tx.Exec(ctx, `UPDATE playlist_entries SET position = $2 WHERE id = $1`, x.ID, n); err != nil {
				return err
			}
		}
		return touch(ctx, tx, playlist)
	})
}

// RenamePlaylist renames a profile's playlist.
func (s *Store) RenamePlaylist(ctx context.Context, profile, playlist uuid.UUID, name string) error {
	return affected(s.pool.Exec(ctx, `UPDATE playlists SET name = $3 WHERE id = $1 AND profile_id = $2`, playlist, profile, name))
}

// RemovePlaylist removes a profile's playlist.
func (s *Store) RemovePlaylist(ctx context.Context, profile, playlist uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1 AND profile_id = $2`, playlist, profile))
}

// ErrSmartPlaylist is a change to the entries of a playlist whose rule finds them.
var ErrSmartPlaylist = errors.New("a smart playlist's entries are what its rule finds: change the rule instead")

// ErrNotSmartPlaylist is a rule given to a playlist that holds its own entries.
var ErrNotSmartPlaylist = errors.New("the playlist holds the entries put in it by hand, not a rule's findings")

// ownPlaylist answers the library a profile's playlist reads and the rule it reads it by, both
// zero for one made by hand; ErrNotFound for no such playlist of the profile's.
func ownPlaylist(ctx context.Context, q db, profile, playlist uuid.UUID) (uuid.UUID, *SmartRule, error) {
	var lib *uuid.UUID
	var rule *SmartRule
	err := q.QueryRow(ctx, `SELECT library_id, rule FROM playlists WHERE id = $1 AND profile_id = $2`,
		playlist, profile).Scan(&lib, &rule)
	return deref(lib), rule, found(err)
}

// byHand is whether a profile's playlist is one that holds its own entries; ErrSmartPlaylist for
// a smart one, whose entries no client sets.
func byHand(ctx context.Context, q db, profile, playlist uuid.UUID) error {
	_, rule, err := ownPlaylist(ctx, q, profile, playlist)
	if err != nil {
		return err
	}
	if rule != nil {
		return ErrSmartPlaylist
	}
	return nil
}

// AddSmartPlaylist makes a profile's playlist whose entries a rule finds on one library's wall, as
// Plex's smart playlists: ErrNotFound for a library the profile does not see.
func (s *Store) AddSmartPlaylist(ctx context.Context, profile, lib uuid.UUID, name string, rule SmartRule) (uuid.UUID, error) {
	if err := rule.CheckPlayable(); err != nil {
		return uuid.UUID{}, err
	}
	if seen, err := s.HasLibrary(ctx, profile, lib); err != nil || !seen {
		return uuid.UUID{}, cmp.Or(err, ErrNotFound)
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO playlists (profile_id, name, library_id, rule) VALUES ($1, $2, $3, $4) RETURNING id`,
		profile, name, lib, rule).Scan(&id)
	return id, err
}

// SetPlaylistRule replaces a smart playlist's rule, and so its entries with what the new one
// finds; ErrNotSmartPlaylist for a playlist made by hand.
func (s *Store) SetPlaylistRule(ctx context.Context, profile, playlist uuid.UUID, rule SmartRule) error {
	if err := rule.CheckPlayable(); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, kept, err := ownPlaylist(ctx, tx, profile, playlist)
		if err != nil {
			return err
		}
		if kept == nil {
			return ErrNotSmartPlaylist
		}
		if _, err := tx.Exec(ctx, `UPDATE playlists SET rule = $2 WHERE id = $1`, playlist, rule); err != nil {
			return err
		}
		return touch(ctx, tx, playlist)
	})
}

// smartEntries answers a page of what a smart playlist's rule finds now, and how many it finds.
func (s *Store) smartEntries(ctx context.Context, profile, lib uuid.UUID, rule SmartRule, offset, limit int) ([]PlaylistEntry, int64, error) {
	cards, total, err := s.Wall(ctx, []uuid.UUID{lib}, rule.playlistPage(profile, offset, limit))
	if err != nil {
		return nil, 0, err
	}
	if rule.Limit > 0 {
		total = min(total, int64(rule.Limit))
	}
	out := make([]PlaylistEntry, len(cards))
	for n, c := range cards {
		out[n] = PlaylistEntry{Card: c}
	}
	return out, total, nil
}

// smartSize counts what a smart playlist's rule finds now and how long those run altogether.
func (s *Store) smartSize(ctx context.Context, profile, lib uuid.UUID, rule SmartRule) (int, int64, error) {
	where, args, err := s.wallQuery(ctx, []uuid.UUID{lib}, profile, rule.Kind, rule.Filter)
	if err != nil {
		return 0, 0, err
	}
	from, _, _ := wallRows(rule.Kind)
	// A rule that takes all its findings needs no order to count them, and sorting half a library
	// to add up its runtimes would be the cost of the page itself.
	var join, taken string
	if rule.Limit > 0 {
		var order string
		join, order = rule.playlistPage(profile, 0, 0).orderBy(args)
		args["limit"] = rule.Limit
		taken = " " + order + " LIMIT @limit"
	}
	var entries int
	var ms int64
	err = s.pool.QueryRow(ctx, `SELECT count(*), coalesce(sum(found.duration_ms), 0) FROM (
		SELECT (SELECT max(v.duration_ms) FROM versions v WHERE v.item_id = items.id AND v.missing_since IS NULL) AS duration_ms
		FROM `+from+join+` `+where+taken+`) found`, args).Scan(&entries, &ms)
	return entries, ms, err
}

func touch(ctx context.Context, tx db, playlist uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE playlists SET updated_at = $2 WHERE id = $1`, playlist, time.Now())
	return err
}

// appendEntries puts the films and episodes titles stand for after a playlist's last entry.
func appendEntries(ctx context.Context, tx db, playlist uuid.UUID, items []uuid.UUID) error {
	var playable []uuid.UUID
	for _, id := range items {
		more, err := playableOf(ctx, tx, id)
		if err != nil {
			return err
		}
		playable = append(playable, more...)
	}
	if len(playable) > 0 {
		_, err := tx.Exec(ctx, `
			INSERT INTO playlist_entries (playlist_id, item_id, position)
			SELECT $1, item, coalesce((SELECT max(position) + 1 FROM playlist_entries WHERE playlist_id = $1), 0) + n - 1
			FROM unnest($2::uuid[]) WITH ORDINALITY AS t(item, n)`, playlist, playable)
		if err != nil {
			return err
		}
	}
	return touch(ctx, tx, playlist)
}

// PlayOrder answers the films and episodes a title stands for that a profile may see, as cards in
// the order they play.
func (s *Store) PlayOrder(ctx context.Context, profile, id uuid.UUID) ([]Card, error) {
	if ok, err := s.visible(ctx, profile, id); err != nil || !ok {
		return nil, cmp.Or(err, ErrNotFound)
	}
	ids, err := playableOf(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	return s.Cards(ctx, profile, ids)
}

// playableOf answers the films and episodes a title stands for, in the order they play; ErrNotFound
// for no title, or one that plays nothing, such as an extra.
func playableOf(ctx context.Context, tx db, id uuid.UUID) ([]uuid.UUID, error) {
	var kind domain.ItemKind
	if err := tx.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, id).Scan(&kind); err != nil {
		return nil, found(err)
	}
	var sql string
	switch kind {
	case domain.ItemMovie, domain.ItemEpisode:
		return []uuid.UUID{id}, nil
	case domain.ItemSeason:
		sql = `SELECT e.id FROM items here ` + seasonEpisodes + ` WHERE here.id = $1 ORDER BY ` + seasonOrder()
	case domain.ItemShow:
		sql = `SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id
			WHERE s.parent_id = $1 AND e.kind = 'episode'
			ORDER BY ` + airedSeason("e") + ` = 0, ` + episodeOrderBy("ASC")
	case domain.ItemCollection:
		sql = `SELECT m.item_id FROM collection_members m JOIN collections c ON c.item_id = m.collection_id
			JOIN items i ON i.id = m.item_id WHERE m.collection_id = $1
			ORDER BY CASE WHEN c.origin = 'user' THEN m.position END, i.released_asc, i.id`
	case domain.ItemExtra:
		return nil, ErrNotFound
	}
	out, err := queryColumn[uuid.UUID](ctx, tx, sql, id)
	if err != nil || kind != domain.ItemCollection {
		return out, err
	}
	var all []uuid.UUID
	for _, m := range out {
		more, err := playableOf(ctx, tx, m)
		if err != nil {
			return nil, err
		}
		all = append(all, more...)
	}
	return all, nil
}
