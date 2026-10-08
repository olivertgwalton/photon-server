package store

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ImportLogin is what an import signs in to its source with: the source's user, where it has
// one apart from its token, and the token.
type ImportLogin struct {
	User  string
	Token string
}

const importColumns = `id, source, url, profile_id, status, error, matched, imported, skipped, unmatched, misses,
	created_at, finished_at`

// scanImport reads importColumns, then any more columns into extra.
func scanImport(extra ...any) pgx.RowToFunc[domain.HistoryImport] {
	return func(row pgx.CollectableRow) (domain.HistoryImport, error) {
		var h domain.HistoryImport
		var misses []byte
		err := row.Scan(append([]any{
			&h.ID, &h.Source, &h.URL, &h.Profile, &h.Status, &h.Error, &h.Matched,
			&h.Imported, &h.Skipped, &h.Unmatched, &misses, &h.CreatedAt, &h.FinishedAt,
		}, extra...)...)
		if err != nil {
			return h, err
		}
		return h, json.Unmarshal(misses, &h.Misses)
	}
}

// AddImport keeps an import of a source's history into a profile and queues its job, answering its
// id. ErrNotFound for no such profile.
func (s *Store) AddImport(ctx context.Context, source domain.ImportSource, url string, profile uuid.UUID, login ImportLogin) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO history_imports (source, url, profile_id, source_user, token) VALUES ($1, $2, $3, $4, $5)
			RETURNING id`, source, url, profile, login.User, login.Token).Scan(&id)
		if err != nil {
			return err
		}
		return enqueueAsked(ctx, tx, domain.JobImportHistory, id)
	})
	if violates(err, foreignKeyViolation) {
		return id, ErrNotFound
	}
	return id, err
}

// Imports answers every import, the newest first.
func (s *Store) Imports(ctx context.Context) ([]domain.HistoryImport, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+importColumns+` FROM history_imports ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanImport())
}

func (s *Store) Import(ctx context.Context, id uuid.UUID) (domain.HistoryImport, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+importColumns+` FROM history_imports WHERE id = $1`, id)
	if err != nil {
		return domain.HistoryImport{}, err
	}
	h, err := pgx.CollectOneRow(rows, scanImport())
	return h, found(err)
}

// StartImport marks an import running and answers it with its login. ErrNotFound for one that has
// ended, whose login is gone, or that was removed with its profile.
func (s *Store) StartImport(ctx context.Context, id uuid.UUID) (domain.HistoryImport, ImportLogin, error) {
	var login ImportLogin
	rows, err := s.pool.Query(ctx, `
		UPDATE history_imports SET status = 'running' WHERE id = $1 AND token IS NOT NULL
		RETURNING `+importColumns+`, source_user, token`, id)
	if err != nil {
		return domain.HistoryImport{}, login, err
	}
	h, err := pgx.CollectOneRow(rows, scanImport(&login.User, &login.Token))
	return h, login, found(err)
}

// FinishImport keeps how an import ended, done or failed, and forgets its login.
func (s *Store) FinishImport(ctx context.Context, h domain.HistoryImport) error {
	if h.Misses == nil {
		h.Misses = []domain.Missed{}
	}
	misses, err := json.Marshal(h.Misses)
	if err != nil {
		return err
	}
	return affected(s.pool.Exec(ctx, `
		UPDATE history_imports SET status = $2, error = $3, matched = $4, imported = $5, skipped = $6, unmatched = $7,
			misses = $8, finished_at = $9, source_user = '', token = NULL
		WHERE id = $1`,
		h.ID, h.Status, h.Error, h.Matched, h.Imported, h.Skipped, h.Unmatched, misses, time.Now()))
}

// providerOrder is the order a title's ids are tried in, as its key is made.
const providerOrder = `array_position(ARRAY['tmdb', 'tvdb', 'imdb'], e.provider)`

// MatchFilm answers the film with one of ids. ErrNotFound for none; of several, such as a film
// in two libraries, which share a state, any one.
func (s *Store) MatchFilm(ctx context.Context, ids map[domain.Provider]string) (uuid.UUID, error) {
	providers, values := idPairs(ids)
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT i.id FROM items i JOIN external_ids e ON e.item_id = i.id
		WHERE i.kind = 'movie' AND (e.provider, e.value) IN (SELECT * FROM unnest($1::text[], $2::text[]))
		ORDER BY `+providerOrder+`, i.id LIMIT 1`, providers, values).Scan(&id)
	return id, found(err)
}

// MatchEpisode answers the episode of the show with one of ids that is numbered season and
// episode, as the show orders its episodes. ErrNotFound for none.
func (s *Store) MatchEpisode(ctx context.Context, show map[domain.Provider]string, season, episode int) (uuid.UUID, error) {
	providers, values := idPairs(show)
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT ep.id FROM items sh JOIN external_ids e ON e.item_id = sh.id
		JOIN items se ON se.parent_id = sh.id AND se.kind = 'season'
		JOIN items ep ON ep.parent_id = se.id AND ep.kind = 'episode'
		WHERE sh.kind = 'show' AND (e.provider, e.value) IN (SELECT * FROM unnest($1::text[], $2::text[]))
			AND ep.season_number = $3 AND $4 BETWEEN ep.episode_number AND coalesce(ep.episode_end, ep.episode_number)
		ORDER BY `+providerOrder+`, ep.id LIMIT 1`, providers, values, season, episode).Scan(&id)
	return id, found(err)
}

func idPairs(ids map[domain.Provider]string) (providers, values []string) {
	for p, v := range ids {
		providers = append(providers, string(p))
		values = append(values, v)
	}
	return providers, values
}
