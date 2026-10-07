package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const nodeColumns = `id, name, first_seen, role, transcode_limit, availability, note`

func scanNode(row pgx.Row) (domain.NodeRecord, error) {
	var n domain.NodeRecord
	var limit *int
	err := row.Scan(&n.ID, &n.Name, &n.FirstSeen, &n.Role, &limit, &n.Availability, &n.Note)
	n.LimitSource = domain.LimitAutomatic
	if limit != nil {
		n.LimitSource, n.Limit = domain.LimitSet, *limit
	}
	return n, err
}

// JoinNode keeps a node starting up as named name, and answers what an admin has set of it: as a
// node of all, its limit worked out, where it is new.
func (s *Store) JoinNode(ctx context.Context, id uuid.UUID, name string) (domain.NodeRecord, error) {
	return scanNode(s.pool.QueryRow(ctx, `
		INSERT INTO node (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name
		RETURNING `+nodeColumns, id, name))
}

// Node answers a node as the server keeps it.
func (s *Store) Node(ctx context.Context, id uuid.UUID) (domain.NodeRecord, error) {
	n, err := scanNode(s.pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM node WHERE id = $1`, id))
	return n, found(err)
}

// KnownNodes answers every node there is or has been, by when each first started.
func (s *Store) KnownNodes(ctx context.Context) ([]domain.NodeRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+nodeColumns+` FROM node ORDER BY first_seen, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.NodeRecord, error) { return scanNode(row) })
}

// SetNodeSettings keeps what an admin sets of a node; ErrNotFound for a node there has never been.
func (s *Store) SetNodeSettings(ctx context.Context, id uuid.UUID, n domain.NodeSettings) error {
	var limit *int
	switch n.LimitSource {
	case domain.LimitSet:
		limit = &n.Limit
	case domain.LimitAutomatic:
	}
	return affected(s.pool.Exec(ctx, `
		UPDATE node SET role = $2, transcode_limit = $3, availability = $4, note = $5 WHERE id = $1`,
		id, n.Role, limit, n.Availability, n.Note))
}
