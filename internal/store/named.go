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
	NamedPerson    NamedKind = "person"
	NamedPlaylist  NamedKind = "playlist"
	// NamedVersion is a copy of a title, which Jellyfin's apps open as an item of its own.
	NamedVersion NamedKind = "version"
)

// Named is what an id names; of a title, its kind; of a version, its title's kind and id.
type Named struct {
	Kind  NamedKind
	Title domain.ItemKind
	Of    uuid.UUID
}

// Named answers what an id names that a profile may open, as reading it would find it: a title it
// sees or a version of one, an episode announced with no file of a show it sees, someone credited, or
// a playlist of its own. ErrNotFound for nothing.
func (s *Store) Named(ctx context.Context, profile, id uuid.UUID) (Named, error) {
	var n Named
	var of *uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT 'title', i.kind, NULL::uuid FROM items i, viewer(@profile) v WHERE i.id = @id AND sees(v, i)
		UNION ALL
		SELECT 'version', i.kind, i.id FROM versions ve JOIN items i ON i.id = ve.item_id, viewer(@profile) v
		WHERE ve.id = @id AND sees(v, i)
		UNION ALL
		SELECT 'announced', '', NULL FROM announced_episodes a JOIN items show ON show.id = a.show_id
		WHERE a.id = @id AND `+unfiled+` AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, show))
		UNION ALL
		SELECT 'person', '', NULL FROM people WHERE id = @id
		UNION ALL
		SELECT 'playlist', '', NULL FROM playlists WHERE id = @id AND profile_id = @profile
		LIMIT 1`, pgx.NamedArgs{"id": id, "profile": profile}).Scan(&n.Kind, &n.Title, &of)
	if of != nil {
		n.Of = *of
	}
	return n, found(err)
}
