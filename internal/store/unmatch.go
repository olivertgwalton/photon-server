package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ownSources are where a title's own words come from, which an unmatch leaves: its files, its NFO
// and an admin's edits. Every other source is a provider.
const ownSources = `('file', 'nfo', 'user')`

// Unmatch takes a film or show off every provider, as Plex's Unmatch does: their ids, words,
// pictures, scores, credits, videos and box sets go from it, its seasons and its episodes, its
// names fall back to what its files are called, and its folder is read again for the rest its
// files and NFO say. It is held so, past every refresh of its library, until its match is fixed or
// it is refreshed itself.
func (s *Store) Unmatch(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Kind != domain.ItemMovie && item.Kind != domain.ItemShow {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `
			SELECT id FROM items WHERE id = $1
			UNION SELECT s.id FROM items s WHERE s.parent_id = $1 AND s.kind = 'season'
			UNION SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id WHERE s.parent_id = $1 AND e.kind = 'episode'`,
			id)
		if err != nil {
			return err
		}
		scope, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, stmt := range []string{
			`UPDATE items SET unmatched_at = now() WHERE id = @item`,
			`DELETE FROM external_ids WHERE item_id = ANY(@scope) AND source IN ('match', 'user')`,
			`DELETE FROM artwork WHERE item_id = ANY(@scope) AND source NOT IN ` + ownSources,
			`DELETE FROM ratings WHERE item_id = ANY(@scope) AND source NOT IN ` + ownSources,
			`DELETE FROM remote_videos WHERE item_id = ANY(@scope)`,
			`DELETE FROM credits WHERE item_id = ANY(@scope) AND source NOT IN ` + ownSources,
			`DELETE FROM jobs WHERE kind = 'identify' AND subject = @item AND state = 'queued'`,
			// A name a provider gave falls back to the file's; any other word of one's is unsaid.
			`UPDATE items i SET
				title = CASE WHEN f.fields @> '{title}' THEN i.scan_title ELSE i.title END,
				original_title = CASE WHEN f.fields @> '{original_title}' THEN NULL ELSE i.original_title END,
				overview = CASE WHEN f.fields @> '{overview}' THEN NULL ELSE i.overview END,
				tagline = CASE WHEN f.fields @> '{tagline}' THEN NULL ELSE i.tagline END,
				certificate = CASE WHEN f.fields @> '{certificate}' THEN NULL ELSE i.certificate END,
				release_date = CASE WHEN f.fields @> '{release_date}' THEN NULL ELSE i.release_date END,
				year = CASE WHEN f.fields @> '{year}' THEN NULL ELSE i.year END,
				genres = CASE WHEN f.fields @> '{genres}' THEN NULL ELSE i.genres END,
				studios = CASE WHEN f.fields @> '{studios}' THEN NULL ELSE i.studios END
			FROM (SELECT item_id, array_agg(field) AS fields FROM item_fields
				WHERE item_id = ANY(@scope) AND source NOT IN ` + ownSources + ` GROUP BY item_id) f
			WHERE i.id = f.item_id`,
			`UPDATE item_fields SET source = 'file', updated_at = now()
			WHERE item_id = ANY(@scope) AND field = 'title' AND source NOT IN ` + ownSources,
			`DELETE FROM item_fields WHERE item_id = ANY(@scope) AND field <> 'sort_title' AND source NOT IN ` + ownSources,
		} {
			if _, err := tx.Exec(ctx, stmt, pgx.NamedArgs{"scope": scope, "item": id}); err != nil {
				return err
			}
		}
		// A sort title follows its title as the scan sorts one.
		sorted, err := tx.Query(ctx, `
			SELECT f.item_id, i.title FROM item_fields f JOIN items i ON i.id = f.item_id
			WHERE f.item_id = ANY($1) AND f.field = 'sort_title' AND f.source NOT IN `+ownSources, scope)
		if err != nil {
			return err
		}
		type titled struct {
			ItemID uuid.UUID
			Title  string
		}
		resort, err := pgx.CollectRows(sorted, pgx.RowToStructByName[titled])
		if err != nil {
			return err
		}
		for _, r := range resort {
			if _, err := tx.Exec(ctx, `UPDATE items SET sort_title = $2 WHERE id = $1`, r.ItemID, sortTitle(r.Title)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE item_fields SET source = 'file', updated_at = now()
			WHERE item_id = ANY($1) AND field = 'sort_title' AND source NOT IN `+ownSources, scope); err != nil {
			return err
		}
		for source := range groupingProviders {
			if err := saveGroupings(ctx, tx, id, source, nil); err != nil {
				return err
			}
		}
		if err := keyTitle(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1 AND path = $2`, item.LibraryID, item.Folder); err != nil {
			return err
		}
		return askScan(ctx, tx, item.LibraryID, []string{item.Folder}, 0)
	})
}

// held answers whether an admin unmatched a film or show, which no provider is then asked about.
func held(ctx context.Context, q db, id uuid.UUID) (bool, error) {
	var unmatched bool
	err := q.QueryRow(ctx, `SELECT unmatched_at IS NOT NULL FROM items WHERE id = $1`, id).Scan(&unmatched)
	return unmatched, found(err)
}

// release lets a film or show be matched again.
func release(ctx context.Context, q db, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE items SET unmatched_at = NULL WHERE id = $1`, id)
	return err
}
