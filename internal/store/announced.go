package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// AnnouncedEpisode answers an episode announced with no file, by its id, as a card: ErrNotFound for
// one of a show the profile may not see, or one whose file has landed.
func (s *Store) AnnouncedEpisode(ctx context.Context, profile, id uuid.UUID) (Card, error) {
	rows, err := queryRows[announcedRow](ctx, s.pool, `
		SELECT `+announcedColumns+` FROM announced_episodes a JOIN items show ON show.id = a.show_id
		WHERE a.id = @id AND `+unfiled+`
			AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, show))`, pgx.NamedArgs{"id": id, "profile": profile})
	if err != nil {
		return Card{}, err
	}
	if len(rows) == 0 {
		return Card{}, ErrNotFound
	}
	cards, err := s.announcedCards(ctx, profile, rows)
	if err != nil {
		return Card{}, err
	}
	return cards[0], nil
}
