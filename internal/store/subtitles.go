package store

import (
	"context"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// SubtitleSearch is what a copy's subtitles are searched by, and where its first file is, which
// its release is hashed by: one file of a copy in several is no release on its own.
type SubtitleSearch struct {
	Version       uuid.UUID
	Query         domain.SubtitleQuery
	Root, RelPath string
	Parts         int
}

// SubtitleSearchOf answers what a film's or an episode's copy, the one asked for else its
// longest, is searched for subtitles by: a film's ids, or an episode's show's and its numbers.
// ErrNotFound for no such title, one the profile may not see, or none of its copies on disk.
func (s *Store) SubtitleSearchOf(ctx context.Context, profile, item, version uuid.UUID) (SubtitleSearch, error) {
	c, err := s.Playable(ctx, profile, item, version)
	if err != nil {
		return SubtitleSearch{}, err
	}
	out := SubtitleSearch{Version: c.Version, Parts: len(c.Parts)}
	if out.Root, out.RelPath, err = s.PartFile(ctx, c.Parts[0].ID); err != nil {
		return SubtitleSearch{}, err
	}
	var show *uuid.UUID
	var season, episode *int
	err = s.pool.QueryRow(ctx, `
		SELECT i.kind, i.season_number, i.episode_number, s.parent_id FROM items i LEFT JOIN items s ON s.id = i.parent_id
		WHERE i.id = $1`, item).Scan(&out.Query.Kind, &season, &episode, &show)
	if err != nil {
		return SubtitleSearch{}, found(err)
	}
	of := item
	if out.Query.Kind == domain.ItemEpisode && show != nil && season != nil && episode != nil {
		of, out.Query.Season, out.Query.Episode = *show, *season, *episode
	}
	ids, err := s.ExternalIDs(ctx, []uuid.UUID{of})
	out.Query.IDs = ids[of]
	return out, err
}

// FetchedSubtitleFile is a subtitle fetched from a provider for a copy as SubRip: what it is, and
// its text.
type FetchedSubtitleFile struct {
	Language        language.Tag
	Title           string
	HearingImpaired bool
	Forced          bool
	Body            []byte
}

// SaveFetchedSubtitle keeps a subtitle fetched for a copy among the files beside it, its text
// kept with it; it answers its id.
func (s *Store) SaveFetchedSubtitle(ctx context.Context, version uuid.UUID, f FetchedSubtitleFile) (uuid.UUID, error) {
	var id uuid.UUID
	var lang *string
	if f.Language != language.Und {
		lang = new(f.Language.String())
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO subtitle_files (version_id, library_id, rel_path, codec, language, title, forced, is_default,
			hearing_impaired, size_bytes, mtime_ns, body)
		SELECT v.id, v.library_id, 'fetched/' || $2 || '.srt', 'subrip', $3, $4, $5, false, $6, $7, $8, $9
		FROM versions v WHERE v.id = $1
		RETURNING id`,
		version, uuid.NewV7().String(), lang, optional(f.Title), f.Forced, f.HearingImpaired, len(f.Body),
		time.Now().UnixNano(), f.Body).Scan(&id)
	return id, found(err)
}

// SubtitleBody answers the text of a fetched subtitle; ErrNotFound for none, or one in a library.
func (s *Store) SubtitleBody(ctx context.Context, id uuid.UUID) ([]byte, error) {
	var body []byte
	err := s.pool.QueryRow(ctx, `SELECT body FROM subtitle_files WHERE id = $1 AND body IS NOT NULL`, id).Scan(&body)
	return body, found(err)
}

// RemoveFetchedSubtitle forgets a fetched subtitle; one in a library is the library's, and
// ErrNotFound.
func (s *Store) RemoveFetchedSubtitle(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM subtitle_files WHERE id = $1 AND body IS NOT NULL`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
