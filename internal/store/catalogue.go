package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Film is a title as the scanner found it in one folder.
type Film struct {
	Title  string
	Year   int
	Folder string
	IDs    map[domain.Provider]string
	NFO    *domain.Metadata
	// Artwork is the pictures of it in its folder, and Themes its theme tunes there.
	Artwork []domain.Artwork
	Themes  []string
	Copies  []Copy
}

// KnownCopies answers which of the content keys are of copies already in the library, by key as
// a string. The same file in two libraries is a copy in each.
func (s *Store) KnownCopies(ctx context.Context, lib uuid.UUID, keys [][]byte) (map[string]bool, error) {
	found, err := queryColumn[[]byte](ctx, s.pool, `SELECT fingerprint FROM versions WHERE library_id = $1 AND fingerprint = ANY($2)`,
		lib, keys)
	known := map[string]bool{}
	for _, k := range found {
		known[string(k)] = true
	}
	return known, err
}

// KnownFile is a file the last scan recorded as a part of a copy: its size and modification time
// then, and the copy's content key, its place in the copy and how many parts the copy has.
type KnownFile struct {
	Size       int64
	ModTime    time.Time
	ContentKey []byte
	Idx        int
	Parts      int
}

// KnownFiles answers the files of paths the library recorded at its last scan, by path.
func (s *Store) KnownFiles(ctx context.Context, lib uuid.UUID, paths []string) (map[string]KnownFile, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.rel_path, f.size_bytes, f.mtime_ns, v.fingerprint, p.idx,
			(SELECT count(*) FROM parts o WHERE o.version_id = v.id)
		FROM part_files f JOIN parts p ON p.id = f.part_id JOIN versions v ON v.id = p.version_id
		WHERE f.library_id = $1 AND f.rel_path = ANY($2)`, lib, paths)
	if err != nil {
		return nil, err
	}
	known := map[string]KnownFile{}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) {
		var rel string
		var f KnownFile
		var mtime int64
		err := r.Scan(&rel, &f.Size, &mtime, &f.ContentKey, &f.Idx, &f.Parts)
		f.ModTime = time.Unix(0, mtime)
		known[rel] = f
		return struct{}{}, err
	})
	return known, err
}

// FolderFingerprints answers the fingerprint each of a library's folders had when it was last
// scanned, by path.
func (s *Store) FolderFingerprints(ctx context.Context, lib uuid.UUID) (map[string][]byte, error) {
	return queryMap[string, []byte](ctx, s.pool, `SELECT path, fingerprint FROM folders WHERE library_id = $1`, lib)
}

// Changed is the titles a write added, changed and removed.
type Changed map[domain.TitleChange][]uuid.UUID

func (c Changed) add(change domain.TitleChange, ids ...uuid.UUID) {
	c[change] = append(c[change], ids...)
}

// Saved is what a folder's save did: the paths of extras no single title owns, and the titles it
// added and changed.
type Saved struct {
	Unowned []string
	Titles  Changed
}

// SaveFolder writes a scanned folder's films and extras and remembers its fingerprint, nil for
// none, in one transaction.
func (s *Store) SaveFolder(ctx context.Context, lib uuid.UUID, path string, fingerprint []byte, films []Film, extras []Extra) (Saved, error) {
	saved := Saved{Titles: Changed{}}
	err := s.pipelined(ctx, func(tx db) error {
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		for _, f := range films {
			if err := saveFilm(ctx, tx, lib, settings, f, saved.Titles); err != nil {
				return fmt.Errorf("%s: %w", f.Title, err)
			}
		}
		if saved.Unowned, err = saveExtras(ctx, tx, lib, settings, extras); err != nil {
			return err
		}
		return rememberFolder(ctx, tx, lib, path, fingerprint)
	})
	return saved, err
}

// analysis is what a library asks to be made of the media a scan adds to it, and when the server's
// timings have that work done.
type analysis struct {
	model.Library
	PreviewsDue, MarkersDue domain.JobDue
}

func analysisOf(ctx context.Context, tx db, lib uuid.UUID) (analysis, error) {
	var a analysis
	var previews, markers domain.Timing
	err := tx.QueryRow(ctx, `
		SELECT l.previews, l.keyframes, l.markers, s.previews_timing, s.markers_timing FROM libraries l, server s
		WHERE l.id = $1`, lib).Scan(&a.Previews, &a.Keyframes, &a.Markers, &previews, &markers)
	a.PreviewsDue, a.MarkersDue = addedDue(previews), addedDue(markers)
	return a, err
}

// rememberFolder keeps the fingerprint a folder had when it was scanned. A nil fingerprint forgets
// the folder's, so the next scan reads it again.
func rememberFolder(ctx context.Context, tx db, lib uuid.UUID, path string, fingerprint []byte) error {
	if fingerprint == nil {
		_, err := tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1 AND path = $2`, lib, path)
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO folders (library_id, path, fingerprint) VALUES ($1, $2, $3)
		ON CONFLICT (library_id, path) DO UPDATE SET fingerprint = excluded.fingerprint`, lib, path, fingerprint)
	return err
}

// insertItem adds a title the scanner found, giving it its id: chosen here, the insert waits on
// nothing and nothing waits on it.
func insertItem(ctx context.Context, tx db, row *model.Item) error {
	row.ID = uuid.NewV7()
	_, err := tx.Exec(ctx, `
		INSERT INTO items (id, library_id, kind, parent_id, season_number, episode_number, episode_end, air_date,
			extra_kind, scan_title, title, sort_title, folder)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		row.ID, row.LibraryID, row.Kind, row.ParentID, row.SeasonNumber, row.EpisodeNumber, row.EpisodeEnd, row.AirDate,
		row.ExtraKind, row.ScanTitle, row.Title, row.SortTitle, row.Folder)
	return err
}

func saveFilm(ctx context.Context, tx db, lib uuid.UUID, settings analysis, f Film, changed Changed) error {
	itemID, err := filmItem(ctx, tx, lib, f, changed)
	if err != nil {
		return err
	}
	if err := saveFolderArtwork(ctx, tx, itemID, f.Folder, f.Artwork); err != nil {
		return err
	}
	if err := saveFolderThemes(ctx, tx, itemID, f.Folder, f.Themes); err != nil {
		return err
	}
	for _, c := range f.Copies {
		if err := saveCopy(ctx, tx, lib, settings, itemID, c); err != nil {
			return err
		}
	}
	// The end of each copy is read for credits; the reading passes over a copy already read.
	if settings.Markers == domain.MarkersAll && len(f.Copies) > 0 {
		if err := insertJob(ctx, tx, domain.JobMarkers, itemID, 0, 0, settings.MarkersDue); err != nil {
			return err
		}
	}
	return keyTitle(ctx, tx, itemID)
}

// filmItem is the title a film's copies belong to: the title of a copy already known, else a title
// of the same name in the same folder, else a new one.
func filmItem(ctx context.Context, tx db, lib uuid.UUID, f Film, changed Changed) (uuid.UUID, error) {
	id, known, err := knownItem(ctx, tx, lib, domain.ItemMovie, f.Copies)
	if err != nil {
		return uuid.UUID{}, err
	}
	if !known {
		err := tx.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1 AND folder = $2 AND scan_title = $3 LIMIT 1`,
			lib, f.Folder, f.Title).Scan(&id)
		switch {
		case err == nil:
		case !errors.Is(err, pgx.ErrNoRows):
			return uuid.UUID{}, err
		default:
			item := model.Item{
				LibraryID: lib, Kind: domain.ItemMovie,
				ScanTitle: f.Title, Title: f.Title, SortTitle: sortTitle(f.Title), Folder: f.Folder,
			}
			if err := insertItem(ctx, tx, &item); err != nil {
				return uuid.UUID{}, err
			}
			if err := enqueue(ctx, tx, domain.JobIdentify, item.ID); err != nil {
				return uuid.UUID{}, err
			}
			changed.add(domain.TitleAdded, item.ID)
			return item.ID, describe(ctx, tx, item.ID, f.Title, f.Year, f.IDs, f.NFO)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE items SET scan_title = $2, folder = $3 WHERE id = $1`, id, f.Title, f.Folder); err != nil {
		return uuid.UUID{}, err
	}
	changed.add(domain.TitleUpdated, id)
	return id, describe(ctx, tx, id, f.Title, f.Year, f.IDs, f.NFO)
}

// knownItem is the title of kind holding the first of copies the catalogue already has. A copy
// held by a title of another kind does not count: a film's copy first met as an extra is
// reclaimed by the film, and the emptied extra goes at the end of the scan. Nor does one an admin
// split off, whose title is its alone.
func knownItem(ctx context.Context, tx db, lib uuid.UUID, kind domain.ItemKind, copies []Copy) (uuid.UUID, bool, error) {
	for _, c := range copies {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT v.item_id FROM versions v JOIN items i ON i.id = v.item_id
			WHERE v.library_id = $1 AND v.fingerprint = $2 AND i.kind = $3 AND v.split_at IS NULL LIMIT 1`,
			lib, c.ContentKey, kind).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.UUID{}, false, err
		}
	}
	return uuid.UUID{}, false, nil
}

// FinishScan settles a library after every folder of a scan has been seen, scopes being the
// folders it read with everything under them ("." for the whole library). present holds every
// video and subtitle path the walk found, skipped folders included. A path in scope no longer
// present stops being a place to read its part, or is a subtitle no longer there; a version with a
// part left nowhere is marked missing (kept, so an unmounted disk does not cost its titles), and
// one whose every part is somewhere is not. A title left with no version is removed, then a season
// and a show left with nothing in them. A folder in scope the walk did not visit is forgotten. It
// answers the titles whose copies went missing or came back, and those removed.
func (s *Store) FinishScan(ctx context.Context, lib uuid.UUID, scopes, folders, present []string) (Changed, error) {
	var changed Changed
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		changed = Changed{}
		// A fetched subtitle is kept in no folder of the library.
		for _, from := range []string{"part_files WHERE", "subtitle_files WHERE body IS NULL AND"} {
			_, err := tx.Exec(ctx, `DELETE FROM `+from+` library_id = $1 AND `+notIn("rel_path", "$2")+` AND `+inScope("rel_path", "$3"),
				lib, present, scopes)
			if err != nil {
				return err
			}
		}
		// Only a version whose part went missing, or came back, is written. This and the sweeps
		// below read the whole library whatever the scopes: a save in scope can take a path, a copy,
		// an episode or an extra from a title outside them, and identifying empties collections.
		updated, err := queryColumn[uuid.UUID](ctx, tx, `
			UPDATE versions v SET missing_since = CASE WHEN v.missing_since IS NULL THEN now() END
			WHERE v.library_id = $1 AND (v.missing_since IS NULL) = EXISTS (SELECT 1 FROM parts p
				WHERE p.version_id = v.id AND NOT EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id))
			RETURNING v.item_id`, lib)
		if err != nil {
			return err
		}
		changed.add(domain.TitleUpdated, updated...)
		for _, sql := range []string{
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind IN ('movie', 'episode', 'extra')
				AND NOT EXISTS (SELECT 1 FROM versions v WHERE v.item_id = i.id)`,
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind = 'season'
				AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id)`,
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind = 'show'
				AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id)`,
			`DELETE FROM items i USING collections c WHERE c.item_id = i.id AND i.library_id = $1
				AND c.origin NOT IN ` + madeHere + ` AND NOT EXISTS (SELECT 1 FROM collection_members m WHERE m.collection_id = c.item_id)`,
		} {
			removed, err := queryColumn[uuid.UUID](ctx, tx, sql+` RETURNING i.id`, lib)
			if err != nil {
				return err
			}
			changed.add(domain.TitleRemoved, removed...)
		}
		_, err = tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1 AND `+notIn("path", "$2")+` AND `+inScope("path", "$3"),
			lib, folders, scopes)
		return err
	})
	return changed, err
}

// notIn is the condition that a path column is none of the array parameter param. Written as
// NOT x = ANY(param), a cached generic plan compares each row with every element in turn; as an
// anti-join, the planner hashes the array once.
func notIn(column, param string) string {
	return `NOT EXISTS (SELECT 1 FROM unnest(` + param + `::text[]) p WHERE p = ` + column + `)`
}

// inScope is the condition that a path column is one of the folders of the array parameter param,
// or under one.
func inScope(column, param string) string {
	return `EXISTS (SELECT 1 FROM unnest(` + param + `::text[]) s WHERE s = '.' OR ` + column + ` = s OR starts_with(` + column + `, s || '/'))`
}

func firstVideo(f *domain.Facts) *domain.Stream {
	for i := range f.Streams {
		if f.Streams[i].Kind == domain.StreamVideo {
			return &f.Streams[i]
		}
	}
	return nil
}

// optional is v, or NULL for its zero.
func optional[T comparable](v T) *T {
	if v == *new(T) {
		return nil
	}
	return &v
}

// sortTitle orders "The Thing" under T and ignores case.
func sortTitle(title string) string {
	t := strings.ToLower(title)
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && rest != "" {
			return rest
		}
	}
	return t
}
