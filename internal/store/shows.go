package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Show is a series as its folder names it.
type Show struct {
	Title  string
	Year   int
	Folder string
	IDs    map[domain.Provider]string
	NFO    *domain.Metadata
	// Seasons is what NFOs say about the show's seasons, by number.
	Seasons map[int]domain.Metadata
	// Artwork is the pictures of the show in its folder, read when that folder is, and Themes its
	// theme tunes there.
	Artwork []domain.Artwork
	Themes  []string
	// SeasonArtwork is the pictures of each season found by the scan of a folder: its own
	// season's from a season folder (and those kept for it in the series' folder), every season's
	// from the series' folder.
	SeasonArtwork map[int][]domain.Artwork
}

type Episode struct {
	Season   int
	Episodes []int
	AirDate  time.Time
	Title    string
	Folder   string
	IDs      map[domain.Provider]string
	NFO      *domain.Metadata
	Artwork  []domain.Artwork
	// ByNumber lets the episode join one already known by its season and numbers. It is false for
	// an episode read from a bare number, which joins nothing but its own copies.
	ByNumber bool
	Copies   []Copy
}

// SaveShowFolder writes a folder of a series' episodes and extras and remembers its fingerprint,
// nil for none, in one transaction.
func (s *Store) SaveShowFolder(ctx context.Context, lib uuid.UUID, path string, fingerprint []byte, show Show, episodes []Episode, extras []Extra) (Saved, error) {
	saved := Saved{Titles: Changed{}}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		// A series' own folder, holding its NFO and extras, comes before any of its episodes.
		if len(episodes) > 0 || ((len(extras) > 0 || show.NFO != nil || len(show.Artwork) > 0 || len(show.Themes) > 0) && show.Folder != "") {
			showID, err := ensureShow(ctx, tx, lib, show, saved.Titles)
			if err != nil {
				return err
			}
			seasons := map[int]uuid.UUID{}
			var saving []string
			for _, e := range episodes {
				saving = append(saving, partPathsOf(e.Copies)...)
			}
			for _, e := range episodes {
				seasonID, ok := seasons[e.Season]
				if !ok {
					var said *domain.Metadata
					if m, ok := show.Seasons[e.Season]; ok {
						said = &m
					}
					if seasonID, err = ensureSeason(ctx, tx, lib, showID, e.Folder, e.Season, said, saved.Titles); err != nil {
						return err
					}
					// The season's sound is compared again; the comparison passes over a season with
					// nothing new.
					if settings.Markers == domain.MarkersAll {
						if err := insertJob(ctx, tx, domain.JobMarkers, seasonID, markersQuiet, 0, settings.MarkersDue); err != nil {
							return err
						}
					}
					seasons[e.Season] = seasonID
				}
				if err := saveEpisode(ctx, tx, lib, settings, showID, seasonID, e, saving, saved.Titles); err != nil {
					return fmt.Errorf("%s season %d %v: %w", show.Title, e.Season, e.Episodes, err)
				}
			}
			if path == show.Folder {
				if err := saveFolderArtwork(ctx, tx, showID, path, show.Artwork); err != nil {
					return err
				}
				if err := saveFolderThemes(ctx, tx, showID, path, show.Themes); err != nil {
					return err
				}
			}
			if err := saveSeasonArtwork(ctx, tx, showID, path, show); err != nil {
				return err
			}
			if err := keyTitle(ctx, tx, showID); err != nil {
				return err
			}
			// A season named by tvshow.nfo keeps its episodes in a folder of its own.
			for number, said := range show.Seasons {
				if _, done := seasons[number]; done {
					continue
				}
				season, err := seasonOf(ctx, tx, showID, number)
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				if err := applyMetadata(ctx, tx, season, domain.SourceNFO, said); err != nil {
					return err
				}
			}
		}
		if saved.Unowned, err = saveExtras(ctx, tx, lib, settings, extras); err != nil {
			return err
		}
		return rememberFolder(ctx, tx, lib, path, fingerprint)
	})
	return saved, err
}

func ensureShow(ctx context.Context, tx db, lib uuid.UUID, show Show, changed Changed) (uuid.UUID, error) {
	row := model.Item{
		LibraryID: lib, Kind: domain.ItemShow,
		ScanTitle: show.Title, Title: show.Title, SortTitle: sortTitle(show.Title), Folder: show.Folder,
	}
	err := tx.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1 AND kind = 'show' AND folder = $2 LIMIT 1`,
		lib, show.Folder).Scan(&row.ID)
	change := domain.TitleUpdated
	switch {
	case err == nil:
		_, err = tx.Exec(ctx, `UPDATE items SET scan_title = $2 WHERE id = $1`, row.ID, row.ScanTitle)
	case errors.Is(err, pgx.ErrNoRows):
		change = domain.TitleAdded
		if err = insertItem(ctx, tx, &row); err == nil {
			err = enqueue(ctx, tx, domain.JobIdentify, row.ID)
		}
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	changed.add(change, row.ID)
	return row.ID, describe(ctx, tx, row.ID, show.Title, show.Year, show.IDs, show.NFO)
}

func ensureSeason(ctx context.Context, tx db, lib, showID uuid.UUID, folder string, number int, nfo *domain.Metadata, changed Changed) (uuid.UUID, error) {
	title := fmt.Sprintf("Season %d", number)
	if number == 0 {
		title = "Specials"
	}
	id, err := seasonOf(ctx, tx, showID, number)
	change := domain.TitleUpdated
	if errors.Is(err, pgx.ErrNoRows) {
		change = domain.TitleAdded
		row := model.Item{
			LibraryID: lib, Kind: domain.ItemSeason, ParentID: &showID, SeasonNumber: &number,
			ScanTitle: title, Title: title, SortTitle: sortTitle(title), Folder: folder,
		}
		err = insertItem(ctx, tx, &row)
		id = row.ID
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	changed.add(change, id)
	return id, describe(ctx, tx, id, title, 0, nil, nfo)
}

// seasonOf is a show's season of a number; pgx.ErrNoRows for none.
func seasonOf(ctx context.Context, tx db, showID uuid.UUID, number int) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM items WHERE parent_id = $1 AND kind = 'season' AND season_number = $2 LIMIT 1`,
		showID, number).Scan(&id)
	return id, err
}

func saveEpisode(ctx context.Context, tx db, lib uuid.UUID, settings analysis, showID, seasonID uuid.UUID, e Episode, saving []string, changed Changed) error {
	row := model.Item{
		LibraryID: lib, Kind: domain.ItemEpisode, ParentID: &seasonID, SeasonNumber: &e.Season,
		ScanTitle: e.Title, Title: e.Title, SortTitle: sortTitle(e.Title), Folder: e.Folder,
	}
	if len(e.Episodes) > 0 {
		first, last := e.Episodes[0], e.Episodes[len(e.Episodes)-1]
		row.EpisodeNumber = &first
		if last != first {
			row.EpisodeEnd = &last
		}
	}
	if !e.AirDate.IsZero() {
		row.AirDate = &e.AirDate
	}
	var err error
	row.ID, err = episodeItem(ctx, tx, lib, e, row, saving)
	if err != nil {
		return err
	}
	change := domain.TitleUpdated
	if row.ID == (uuid.UUID{}) {
		change = domain.TitleAdded
		if err := insertItem(ctx, tx, &row); err != nil {
			return err
		}
		// The show's match describes its episodes, so a new one asks for it again.
		if err := enqueue(ctx, tx, domain.JobIdentify, showID); err != nil {
			return err
		}
	} else {
		_, err := tx.Exec(ctx, `
			UPDATE items SET parent_id = $2, season_number = $3, episode_number = $4, episode_end = $5, air_date = $6,
				scan_title = $7, folder = $8
			WHERE id = $1`,
			row.ID, row.ParentID, row.SeasonNumber, row.EpisodeNumber, row.EpisodeEnd, row.AirDate, row.ScanTitle, row.Folder)
		if err != nil {
			return err
		}
	}
	changed.add(change, row.ID)
	if err := describe(ctx, tx, row.ID, e.Title, 0, e.IDs, e.NFO); err != nil {
		return err
	}
	if err := saveFolderArtwork(ctx, tx, row.ID, e.Folder, e.Artwork); err != nil {
		return err
	}
	for _, c := range e.Copies {
		if err := saveCopy(ctx, tx, lib, settings, row.ID, c); err != nil {
			return err
		}
	}
	return nil
}

// episodeItem is the episode a copy belongs to: the episode read from one of its paths, else the
// episode of the same bytes moved or renamed from elsewhere, else, for an episode read from a
// canonical form, the episode of its season with the same numbers or date. An episode under other
// numbers is taken only when none of its other paths is among saving, the paths of the folder
// being saved: the same bytes under two numbers at once are two episodes, as Jellyfin's items are
// one per path.
func episodeItem(ctx context.Context, tx db, lib uuid.UUID, e Episode, row model.Item, saving []string) (uuid.UUID, error) {
	keys := make([][]byte, len(e.Copies))
	for i, c := range e.Copies {
		keys[i] = c.ContentKey
	}
	own := partPathsOf(e.Copies)
	var id uuid.UUID
	// The copies found by path and by bytes are looked up apart: asked for at once, by an OR across
	// two joins, Postgres reads every episode copy of the library to answer.
	err := tx.QueryRow(ctx, `
		WITH found AS (
			SELECT p.version_id, true AS own FROM part_files f JOIN parts p ON p.id = f.part_id
			WHERE f.library_id = $1 AND f.rel_path = ANY($2)
			UNION ALL
			SELECT id, false FROM versions WHERE library_id = $1 AND fingerprint = ANY($3)
		)
		SELECT i.id FROM found JOIN versions v ON v.id = found.version_id JOIN items i ON i.id = v.item_id
		WHERE i.kind = 'episode' AND v.split_at IS NULL
			AND ((i.season_number, i.episode_number, i.episode_end) IS NOT DISTINCT FROM ($4::int, $5::int, $6::int)
				OR NOT EXISTS (SELECT 1 FROM versions ov JOIN parts op ON op.version_id = ov.id JOIN part_files o ON o.part_id = op.id
					WHERE ov.item_id = i.id AND o.rel_path = ANY($7) AND o.rel_path <> ALL($2)))
		ORDER BY found.own DESC,
			(i.season_number, i.episode_number, i.episode_end) IS NOT DISTINCT FROM ($4::int, $5::int, $6::int) DESC
		LIMIT 1`, lib, own, keys, row.SeasonNumber, row.EpisodeNumber, row.EpisodeEnd, saving).Scan(&id)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	if !e.ByNumber {
		return uuid.UUID{}, nil
	}
	sql := `SELECT id FROM items WHERE parent_id = $1 AND kind = 'episode' AND `
	args := []any{*row.ParentID}
	if row.EpisodeNumber != nil {
		sql += `episode_number = $2 AND episode_end IS NOT DISTINCT FROM $3`
		args = append(args, *row.EpisodeNumber, row.EpisodeEnd)
	} else {
		sql += `episode_number IS NULL AND air_date = $2`
		args = append(args, *row.AirDate)
	}
	err = tx.QueryRow(ctx, sql+` LIMIT 1`, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, nil
	}
	return id, err
}

// saveSeasonArtwork keeps the pictures a folder's scan found for each season. The series' folder
// speaks for every season's pictures kept there; a season's folder for its own, in it and in the
// series' folder.
func saveSeasonArtwork(ctx context.Context, tx db, showID uuid.UUID, folder string, show Show) error {
	seasons, err := queryRows[model.Item](ctx, tx, `SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'season'`, showID)
	if err != nil {
		return err
	}
	folders := []string{folder}
	if folder != show.Folder {
		folders = append(folders, show.Folder)
	}
	for _, season := range seasons {
		pictures, found := show.SeasonArtwork[*season.SeasonNumber]
		if folder != show.Folder && !found {
			continue
		}
		for _, f := range folders {
			if err := saveFolderArtwork(ctx, tx, season.ID, f, pictures); err != nil {
				return err
			}
		}
	}
	return nil
}

func partPathsOf(copies []Copy) []string {
	var paths []string
	for _, c := range copies {
		for _, p := range c.Parts {
			paths = append(paths, p.RelPath)
		}
	}
	return paths
}
