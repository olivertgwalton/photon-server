package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// NamedKind is what an id an app opens by names.
type NamedKind string

const (
	NamedTitle     NamedKind = "title"
	NamedAnnounced NamedKind = "announced"
)

// Named is what an id names, and of a title, its kind.
type Named struct {
	Kind  NamedKind
	Title domain.ItemKind
}

// Named answers what an id names that a profile may open, as reading it would find it: a title it
// sees, or an episode announced with no file of a show it sees. ErrNotFound for nothing.
func (s *Store) Named(ctx context.Context, profile, id uuid.UUID) (Named, error) {
	var n Named
	err := s.pool.QueryRow(ctx, `
		SELECT 'title', i.kind FROM items i, viewer(@profile) v WHERE i.id = @id AND sees(v, i)
		UNION ALL
		SELECT 'announced', '' FROM announced_episodes a JOIN items show ON show.id = a.show_id
		WHERE a.id = @id AND `+unfiled+` AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, show))
		LIMIT 1`, pgx.NamedArgs{"id": id, "profile": profile}).Scan(&n.Kind, &n.Title)
	return n, found(err)
}
