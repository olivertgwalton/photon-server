package store

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Album is an album as a folder of a music library holds it: its title, the artist its tags name
// as the album's, and the tracks of it there.
type Album struct {
	Title, SortTitle   string
	Artist, ArtistSort string
	Year               int
	Released           time.Time
	Genres             []string
	// Folder is the album's own folder, which a folder of one of its discs is under.
	Folder  string
	Artwork []domain.Artwork
	Tracks  []Track
}

// Track is a sound file of an album, as its tags and name place it.
type Track struct {
	Title, SortTitle string
	Disc, Number     int
	Artists          []string
	Year             int
	Released         time.Time
	Genres           []string
	// Folder is the folder the file is in, its disc's where the album keeps one.
	Folder  string
	Version Version
}

// SaveMusicFolder writes a folder of a music library's albums, each under its artist, and
// remembers its fingerprint, nil for none, in one transaction. An album is found by its title
// under its artist, so the folders of its discs, read apart, fill one album.
func (s *Store) SaveMusicFolder(ctx context.Context, lib uuid.UUID, folder string, fingerprint []byte, albums []Album) (Saved, error) {
	saved := Saved{Titles: Changed{}}
	err := s.pipelined(ctx, func(tx db) error {
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		for _, a := range albums {
			if err := saveAlbum(ctx, tx, lib, settings, folder, a, saved.Titles); err != nil {
				return fmt.Errorf("%s: %w", a.Title, err)
			}
		}
		return rememberFolder(ctx, tx, lib, folder, fingerprint)
	})
	return saved, err
}

func saveAlbum(ctx context.Context, tx db, lib uuid.UUID, settings analysis, folder string, a Album, changed Changed) error {
	artistFolder := "."
	if a.Folder != "." {
		artistFolder = path.Dir(a.Folder)
	}
	artist, err := ensureMusic(ctx, tx, model.Item{LibraryID: &lib, Kind: domain.ItemArtist, Folder: artistFolder},
		domain.Metadata{Title: a.Artist, SortTitle: a.ArtistSort}, changed)
	if err != nil {
		return err
	}
	album, err := ensureMusic(ctx, tx, model.Item{LibraryID: &lib, Kind: domain.ItemAlbum, ParentID: &artist, Folder: a.Folder},
		domain.Metadata{Title: a.Title, SortTitle: a.SortTitle, Year: a.Year, ReleaseDate: a.Released, Genres: a.Genres}, changed)
	if err != nil {
		return err
	}
	if err := saveFolderArtwork(ctx, tx, album, folder, a.Artwork); err != nil {
		return err
	}
	for _, t := range a.Tracks {
		if err := saveTrack(ctx, tx, lib, settings, album, t, changed); err != nil {
			return fmt.Errorf("disc %d track %d: %w", t.Disc, t.Number, err)
		}
	}
	return keyTitle(ctx, tx, artist)
}

// ensureMusic is the artist or album of a title under row's parent, by its name in any case,
// added where there is none; what the files say of it is written either way.
func ensureMusic(ctx context.Context, tx db, row model.Item, said domain.Metadata, changed Changed) (uuid.UUID, error) {
	row.ScanTitle, row.Title, row.SortTitle = said.Title, said.Title, sortTitle(said.Title)
	err := tx.QueryRow(ctx, `
		SELECT id FROM items WHERE library_id = $1 AND kind = $2 AND parent_id IS NOT DISTINCT FROM $3::uuid
			AND lower(scan_title) = lower($4) LIMIT 1`, row.LibraryID, row.Kind, row.ParentID, row.ScanTitle).Scan(&row.ID)
	change := domain.TitleUpdated
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		change = domain.TitleAdded
		err = insertItem(ctx, tx, &row)
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	changed.add(change, row.ID)
	return row.ID, applyMetadata(ctx, tx, row.ID, domain.SourceFile, said)
}

func saveTrack(ctx context.Context, tx db, lib uuid.UUID, settings analysis, album uuid.UUID, t Track, changed Changed) error {
	row := model.Item{
		LibraryID: &lib, Kind: domain.ItemTrack, ParentID: &album, DiscNumber: optional(t.Disc), TrackNumber: optional(t.Number),
		Artists: t.Artists, ScanTitle: t.Title, Title: t.Title, SortTitle: sortTitle(t.Title), Folder: t.Folder,
	}
	// A track is its file's, wherever its tags now place it, or the same bytes' moved from elsewhere.
	err := tx.QueryRow(ctx, `
		WITH found AS (
			SELECT p.version_id, true AS own FROM part_files f JOIN parts p ON p.id = f.part_id
			WHERE f.library_id = $1 AND f.rel_path = ANY($2)
			UNION ALL
			SELECT id, false FROM versions WHERE library_id = $1 AND fingerprint = $3
		)
		SELECT i.id FROM found JOIN versions v ON v.id = found.version_id JOIN items i ON i.id = v.item_id
		WHERE i.kind = 'track' ORDER BY found.own DESC LIMIT 1`,
		lib, partPathsOf([]Version{t.Version}), t.Version.ContentKey).Scan(&row.ID)
	change := domain.TitleUpdated
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		change = domain.TitleAdded
		err = insertItem(ctx, tx, &row)
	case err == nil:
		_, err = tx.Exec(ctx, `
			UPDATE items SET parent_id = $2, disc_number = $3, track_number = $4, artists = $5, scan_title = $6, folder = $7
			WHERE id = $1`, row.ID, row.ParentID, row.DiscNumber, row.TrackNumber, row.Artists, row.ScanTitle, row.Folder)
	}
	if err != nil {
		return err
	}
	changed.add(change, row.ID)
	err = applyMetadata(ctx, tx, row.ID, domain.SourceFile, domain.Metadata{
		Title: t.Title, SortTitle: t.SortTitle, Year: t.Year, ReleaseDate: t.Released, Genres: t.Genres,
	})
	if err != nil {
		return err
	}
	return saveVersion(ctx, tx, lib, settings, row.ID, t.Version)
}

// MusicPage is what only an artist's, an album's or a track's page has: an album's or a track's
// album artist, and a track's album, which lists Tracks as an artist lists Albums. DiscNumber and
// TrackNumber place a track on its album, and Artists are the track's own.
type MusicPage struct {
	Artist, Album           *TitleRef
	DiscNumber, TrackNumber *int
	Artists                 []string
	Albums                  []Card
	Tracks                  []Card
}

// Albums answers an artist's albums as cards, the earliest first, as a discography lists them.
func (s *Store) Albums(ctx context.Context, profile, artist uuid.UUID) ([]Card, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'album' ORDER BY released_asc, sort_title, id`, artist)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}

// Tracks answers an album's tracks as cards, in its order.
func (s *Store) Tracks(ctx context.Context, profile, album uuid.UUID) ([]Card, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'track' ORDER BY `+trackOrder("items"), album)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}

// musicPlace is the album a track is on, and the artist an album or a track is under.
type musicPlace struct {
	artist, album *TitleRef
}

// placesOf answers what each title among rows is under: an episode's or a season's show and
// season, and an album's or a track's artist and album.
func (s *Store) placesOf(ctx context.Context, rows []*model.Item) (map[uuid.UUID]episodeShow, map[uuid.UUID]musicPlace, error) {
	shows, err := s.showsOf(ctx, rows)
	if err != nil {
		return nil, nil, err
	}
	music, err := s.musicOf(ctx, rows)
	return shows, music, err
}

// musicOf answers where each album and track among rows is, read in one lookup.
func (s *Store) musicOf(ctx context.Context, rows []*model.Item) (map[uuid.UUID]musicPlace, error) {
	var asked []uuid.UUID
	for _, r := range rows {
		if r.Kind == domain.ItemAlbum || r.Kind == domain.ItemTrack {
			asked = append(asked, r.ID)
		}
	}
	out := map[uuid.UUID]musicPlace{}
	if len(asked) == 0 {
		return out, nil
	}
	type place struct {
		ID          uuid.UUID
		AlbumID     *uuid.UUID
		AlbumTitle  *string
		ArtistID    uuid.UUID
		ArtistTitle string
	}
	found, err := queryStructs[place](ctx, s.pool, `
		SELECT i.id, al.id AS album_id, al.title AS album_title, ar.id AS artist_id, ar.title AS artist_title
		FROM items i
		LEFT JOIN items al ON i.kind = 'track' AND al.id = i.parent_id
		JOIN items ar ON ar.id = CASE WHEN i.kind = 'track' THEN al.parent_id ELSE i.parent_id END
		WHERE i.id = ANY($1)`, asked)
	for _, f := range found {
		p := musicPlace{artist: &TitleRef{ID: f.ArtistID, Title: f.ArtistTitle}}
		if f.AlbumID != nil {
			p.album = &TitleRef{ID: *f.AlbumID, Title: deref(f.AlbumTitle)}
		}
		out[f.ID] = p
	}
	return out, err
}
