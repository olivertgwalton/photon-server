package store

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// LibraryCounts counts each library's films, shows, seasons and episodes a profile may see; the
// zero id is the server's own count of everything.
//
// It is sees() over every title at once: each certificate's age is read once rather than once for
// every title under it, and a title's certificates are its own, its season's and its show's, as
// far up as a film, show, season or episode goes. Every title of a large library is read, so
// the titles are read narrow and joined by hash.
func (s *Store) LibraryCounts(ctx context.Context, profile uuid.UUID) (map[uuid.UUID]domain.TitleCounts, error) {
	rows, err := s.pool.Query(ctx, `
		WITH v AS MATERIALIZED (SELECT * FROM viewer($1)),
		t AS MATERIALIZED (
			SELECT i.id, i.parent_id, i.library_id, i.kind, i.certificate FROM items i, v
			WHERE i.kind IN ('movie', 'show', 'season', 'episode')
				AND (v.libraries IS NULL OR i.library_id = ANY (v.libraries))
		),
		ok AS MATERIALIZED (
			SELECT d.c, coalesce(certificate_age(d.c) <= v.max_age, v.unrated = 'allow') AS ok
			FROM v, (SELECT DISTINCT certificate FROM t WHERE certificate IS NOT NULL) d (c)
			WHERE v.max_age IS NOT NULL
		)
		SELECT i.library_id::text, i.kind, count(*) FROM v, t i
		LEFT JOIN t s ON s.id = i.parent_id
		LEFT JOIN t g ON g.id = s.parent_id
		LEFT JOIN ok oi ON oi.c = i.certificate
		LEFT JOIN ok os ON os.c = s.certificate
		LEFT JOIN ok og ON og.c = g.certificate
		WHERE v.max_age IS NULL OR (coalesce(oi.ok, true) AND coalesce(os.ok, true) AND coalesce(og.ok, true)
			AND (coalesce(i.certificate, s.certificate, g.certificate) IS NOT NULL OR v.unrated = 'allow'))
		GROUP BY 1, 2`, profile.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]domain.TitleCounts{}
	for rows.Next() {
		var lib string
		var kind domain.ItemKind
		var n int
		if err := rows.Scan(&lib, &kind, &n); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(lib)
		if err != nil {
			return nil, err
		}
		c := out[id]
		switch kind {
		case domain.ItemMovie:
			c.Movies = n
		case domain.ItemShow:
			c.Shows = n
		case domain.ItemSeason:
			c.Seasons = n
		case domain.ItemEpisode:
			c.Episodes = n
		case domain.ItemExtra, domain.ItemCollection:
		}
		out[id] = c
	}
	return out, rows.Err()
}
