package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ErrMoving is a move asked for while another is under way.
var ErrMoving = errors.New("artwork and previews are being moved already")

// StorageMove answers the move under way, if one is.
func (s *Store) StorageMove(ctx context.Context) (domain.StorageMove, bool, error) {
	var m domain.StorageMove
	var target []byte
	err := s.pool.QueryRow(ctx, `SELECT target, started FROM storage_move`).Scan(&target, &m.Started)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	if err := json.Unmarshal(target, &m.To); err != nil {
		return m, false, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT source, copied, total, done, seen FROM storage_move_sources ORDER BY source`)
	if err != nil {
		return m, false, err
	}
	m.Sources, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MoveSource, error) {
		var src domain.MoveSource
		err := row.Scan(&src.Node, &src.Copied, &src.Total, &src.Done, &src.Seen)
		return src, err
	})
	return m, true, err
}

// StartStorageMove begins moving artwork and previews to to, or answers ErrMoving.
func (s *Store) StartStorageMove(ctx context.Context, to domain.Storage) error {
	target, err := json.Marshal(to)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO storage_move (target) VALUES ($1)`, target)
	if violates(err, uniqueViolation) {
		return ErrMoving
	}
	return err
}

// CancelStorageMove stops the move under way, leaving what is kept where it was; ErrNotFound
// where none is.
func (s *Store) CancelStorageMove(ctx context.Context) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM storage_move`))
}

// ClaimMoveSource takes the copy from source for this node, while a move is under way: one not
// begun, or one not done that has said nothing since stale, as one whose node stopped has not.
func (s *Store) ClaimMoveSource(ctx context.Context, source uuid.UUID, stale time.Time) (bool, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO storage_move_sources (source) SELECT $1 WHERE EXISTS (SELECT FROM storage_move)
		ON CONFLICT (source) DO UPDATE SET copied = 0, total = 0, seen = now()
			WHERE NOT storage_move_sources.done AND storage_move_sources.seen < $2
		RETURNING source`, source, stale).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ReportMoveSource says how far the copy from source has got.
func (s *Store) ReportMoveSource(ctx context.Context, source uuid.UUID, copied, total int, done bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE storage_move_sources SET copied = $2, total = $3, done = $4, seen = now() WHERE source = $1`,
		source, copied, total, done)
	return err
}

// FinishStorageMove keeps artwork and previews where the move under way takes them, once every
// one of sources is copied, and answers whether it did.
func (s *Store) FinishStorageMove(ctx context.Context, sources []uuid.UUID) (bool, error) {
	finished := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var target []byte
		err := tx.QueryRow(ctx, `SELECT target FROM storage_move FOR UPDATE`).Scan(&target)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var done int
		err = tx.QueryRow(ctx, `
			SELECT count(*) FROM storage_move_sources WHERE done AND source = ANY($1)`, sources).Scan(&done)
		if err != nil || done < len(sources) {
			return err
		}
		var to domain.Storage
		if err := json.Unmarshal(target, &to); err != nil {
			return err
		}
		if err := setStorage(ctx, tx, to); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM storage_move`)
		finished = err == nil
		return err
	})
	return finished, err
}
