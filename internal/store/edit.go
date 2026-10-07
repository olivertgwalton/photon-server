package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// EditMetadata writes what an admin says of a title. A reader's edit outranks every source, so it
// stands until it is reset; Locked fields are claimed even where nothing is written, so no source
// fills them, as Jellyfin's locked fields are.
func (s *Store) EditMetadata(ctx context.Context, id uuid.UUID, m domain.Metadata) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := readItem(ctx, tx, id); err != nil {
			return err
		}
		return applyMetadata(ctx, tx, id, domain.SourceUser, m)
	})
}

// ResetEdits gives fields an admin edited or locked back to the sources: its folder is read again
// at the next scan and it is matched again, so each field takes the best value a source has. No
// fields resets every one.
func (s *Store) ResetEdits(ctx context.Context, id uuid.UUID, fields []domain.Field) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		q, args := `DELETE FROM item_fields WHERE item_id = $1 AND source = 'user'`, []any{item.ID}
		if len(fields) > 0 {
			q, args = q+` AND field = ANY($2)`, append(args, fields)
		}
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
		return rematch(ctx, tx, item)
	})
}

// PinMatch fixes which title a provider matches a film or show to: the id is the admin's, above
// any an NFO or path gives, and the ids an earlier match led to are dropped so the match finds
// them again.
func (s *Store) PinMatch(ctx context.Context, id uuid.UUID, provider domain.Provider, value string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Kind != domain.ItemMovie && item.Kind != domain.ItemShow {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM external_ids WHERE item_id = $1 AND source = $2`, item.ID, domain.IDFromMatch); err != nil {
			return err
		}
		if err := saveIDs(ctx, tx, item.ID, domain.IDFromUser, map[domain.Provider]string{provider: value}); err != nil {
			return err
		}
		if err := keyTitle(ctx, tx, item.ID); err != nil {
			return err
		}
		if err := release(ctx, tx, item.ID); err != nil {
			return err
		}
		return enqueue(ctx, tx, domain.JobIdentify, item.ID)
	})
}

// SetTitleLocale gives a film or show a metadata language and certification country of its own,
// "" for its library's, and describes it again in them, its seasons and episodes with it.
func (s *Store) SetTitleLocale(ctx context.Context, id uuid.UUID, loc domain.Locale) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := affected(tx.Exec(ctx, `
			UPDATE items SET metadata_language = nullif($2, ''), certification_country = nullif($3, '')
			WHERE id = $1 AND kind IN ('movie', 'show')`, id, loc.Language, loc.Country))
		if err != nil {
			return err
		}
		err = describeAgain(ctx, tx, `
			SELECT s.id FROM items s WHERE s.parent_id = @show AND s.kind = 'season'
			UNION SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id WHERE s.parent_id = @show AND e.kind = 'episode'`,
			pgx.NamedArgs{"show": id})
		if err != nil {
			return err
		}
		if err := forgetDescriptions(ctx, tx, `SELECT @show::uuid AS id`, pgx.NamedArgs{"show": id}); err != nil {
			return err
		}
		return enqueueAsked(ctx, tx, domain.JobIdentify, id)
	})
}

// rematch has a title's folder read again, and its film or show matched again.
func rematch(ctx context.Context, tx db, item *model.Item) error {
	if _, err := tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1 AND path = $2`, item.LibraryID, item.Folder); err != nil {
		return err
	}
	title, err := matchedAs(ctx, tx, item)
	if err != nil {
		return err
	}
	if title != nil {
		if err := enqueue(ctx, tx, domain.JobIdentify, title.ID); err != nil {
			return err
		}
	}
	return askScan(ctx, tx, item.LibraryID, []string{item.Folder}, 0)
}

// matchedAs answers the film or show an item is matched to providers as: itself, or the show a
// season or episode is in. Nil for one that is neither, as a collection is.
func matchedAs(ctx context.Context, tx db, item *model.Item) (*model.Item, error) {
	title := item
	for title.Kind == domain.ItemSeason || title.Kind == domain.ItemEpisode {
		if title.ParentID == nil {
			return nil, nil
		}
		parent, err := readItem(ctx, tx, *title.ParentID)
		if err != nil {
			return nil, err
		}
		title = parent
	}
	if title.Kind != domain.ItemMovie && title.Kind != domain.ItemShow {
		return nil, nil
	}
	return title, nil
}

// Refresh matches a title again ahead of anything the schedule or a scan queued, as Jellyfin's
// and Plex's Refresh Metadata do: a season or episode as its show. RefreshAll first says every
// season and episode under the item was titled by its file, so its show's next match asks about
// each again; their values stand until then. ErrNotFound for no film or show, or one under it.
func (s *Store) Refresh(ctx context.Context, id uuid.UUID, mode domain.RefreshMode) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		title, err := matchedAs(ctx, tx, item)
		if err != nil {
			return err
		}
		if title == nil {
			return ErrNotFound
		}
		switch mode {
		case domain.RefreshMissing:
		case domain.RefreshAll:
			err := describeAgain(ctx, tx, `
				SELECT id FROM items WHERE id = @item AND kind IN ('season', 'episode')
				UNION SELECT s.id FROM items s WHERE s.parent_id = @item AND s.kind IN ('season', 'episode')
				UNION SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id WHERE s.parent_id = @item AND e.kind = 'episode'`,
				pgx.NamedArgs{"item": item.ID})
			if err != nil {
				return err
			}
		}
		if err := release(ctx, tx, title.ID); err != nil {
			return err
		}
		return enqueueAsked(ctx, tx, domain.JobIdentify, title.ID)
	})
}

// describeAgain says the seasons and episodes a query selects were titled by their files, so
// their show's next match asks about each again; their values stand until then.
func describeAgain(ctx context.Context, tx db, items string, args pgx.NamedArgs) error {
	_, err := tx.Exec(ctx, `
		UPDATE item_fields SET source = 'file', updated_at = now()
		WHERE field = 'title' AND source NOT IN ('file', 'nfo', 'user') AND item_id IN (`+items+`)`, args)
	return err
}

// RefreshLibrary queues a match of a library's films and shows, as Jellyfin's Refresh Metadata
// on a library does. RefreshMissing takes only those not yet described: never
// matched, with no overview or poster, a season still titled by its file, or a match that failed
// every attempt. RefreshAll takes every one, and every season and episode under it. Either is
// claimed after the titles a scan has just found, so a large refresh never holds up a new film.
// ErrNotFound for no library.
func (s *Store) RefreshLibrary(ctx context.Context, lib uuid.UUID, mode domain.RefreshMode) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibrary(ctx, tx, lib); err != nil {
			return err
		}
		args := pgx.NamedArgs{"lib": lib, "priority": refreshPriority}
		which := ""
		switch mode {
		case domain.RefreshMissing:
			which = `AND (i.identified_at IS NULL
				OR coalesce(i.overview, '') = ''
				OR NOT EXISTS (SELECT 1 FROM artwork a WHERE a.item_id = i.id AND a.kind = 'poster')
				OR EXISTS (SELECT 1 FROM jobs j WHERE j.kind = 'identify' AND j.subject = i.id AND j.state = 'dead')
				OR EXISTS (SELECT 1 FROM items e JOIN item_fields f ON f.item_id = e.id AND f.field = 'title' AND f.source = 'file'
					WHERE e.kind = 'episode' AND e.parent_id IN (SELECT id FROM items WHERE parent_id = i.id)))`
		case domain.RefreshAll:
			err := describeAgain(ctx, tx, `SELECT id FROM items WHERE library_id = @lib AND kind IN ('season', 'episode')`, args)
			if err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO jobs (kind, subject, priority)
			SELECT 'identify', i.id, @priority FROM items i
			WHERE i.library_id = @lib AND i.kind IN ('movie', 'show') `+which+requeue, args)
		return err
	})
}

// SetEpisodeOrder renumbers a show's episodes in the order its files are numbered in: what any
// provider said of its seasons and episodes stops standing, and every season is matched again.
// The episodes keep their old titles until then.
func (s *Store) SetEpisodeOrder(ctx context.Context, id uuid.UUID, order domain.EpisodeOrder) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `UPDATE items SET episode_order = $2 WHERE id = $1 AND kind = 'show'`, id, order)); err != nil {
			return err
		}
		// What providers said under the old numbers: their claims, stills and credits. What files
		// and NFOs say, and a reader's edits and chosen pictures, stand.
		below := `SELECT s.id FROM items s WHERE s.parent_id = @show
			UNION SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id WHERE s.parent_id = @show`
		for _, q := range []string{
			`DELETE FROM item_fields WHERE item_id IN (` + below + `) AND source NOT IN ('file', 'nfo', 'user')`,
			`DELETE FROM artwork WHERE item_id IN (` + below + `) AND source NOT IN ('file', 'user')`,
			`DELETE FROM credits WHERE item_id IN (` + below + `) AND source <> 'nfo'`,
			// Each episode is titled as its file again, so its season is asked about again.
			`INSERT INTO item_fields (item_id, field, source) SELECT id, 'title', 'file' FROM items
				WHERE kind = 'episode' AND id IN (` + below + `) ON CONFLICT (item_id, field) DO NOTHING`,
		} {
			if _, err := tx.Exec(ctx, q, pgx.NamedArgs{"show": id}); err != nil {
				return err
			}
		}
		return enqueue(ctx, tx, domain.JobIdentify, id)
	})
}
