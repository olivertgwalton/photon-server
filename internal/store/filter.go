package store

import (
	"context"
	"database/sql"
	"slices"
	"uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// WallFilter narrows a library's titles, as Jellyfin's item filters and Plex's section filters do.
// Each list is any of its values; every field set must hold. A zero filter narrows nothing.
type WallFilter struct {
	// StartsWith is a letter its sort title starts with, unaccented, or "#" for before A.
	StartsWith   string
	Marks        []domain.Mark
	Genres       []string
	Years        []int
	Certificates []string
	Studios      []string
	Resolutions  []domain.Resolution
	Ranges       []domain.Range
	// People are credited on it, or on one of its episodes.
	People []uuid.UUID
	// MinRating is the least RatingSite's score out of 100 may be, zero for any.
	RatingSite domain.RatingSite
	MinRating  float64
}

// firstLetter is a sort title's first letter unaccented, or "#" for anything before A.
const firstLetter = `CASE WHEN upper(left(unaccent(items.sort_title), 1)) BETWEEN 'A' AND 'Z'
	THEN upper(left(unaccent(items.sort_title), 1)) ELSE '#' END`

// versionOf is a copy on disk of a film, or of any episode of a show.
const versionOf = `SELECT 1 FROM versions v JOIN items e ON e.id = v.item_id
	WHERE v.missing_since IS NULL AND (e.id = items.id OR e.parent_id = items.id
		OR e.parent_id IN (SELECT s.id FROM items s WHERE s.parent_id = items.id))`

// episodesOf are a show's episodes, and a film itself.
const episodesOf = `SELECT e.id FROM items e WHERE (items.kind = 'movie' AND e.id = items.id)
	OR (e.kind = 'episode' AND (e.parent_id = items.id OR e.parent_id IN (SELECT s.id FROM items s WHERE s.parent_id = items.id)))`

// apply narrows a query over items to what the filter lets through, for a profile.
func (f WallFilter) apply(q *gorm.DB, profile uuid.UUID) *gorm.DB {
	if f.StartsWith != "" {
		q = q.Where(firstLetter+" = ?", f.StartsWith)
	}
	for _, m := range f.Marks {
		q = q.Where(markSQL(m), sql.Named("profile", profile.String()))
	}
	if len(f.Genres) > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM jsonb_array_elements_text(items.genres) g WHERE g IN ?)", f.Genres)
	}
	if len(f.Studios) > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM jsonb_array_elements_text(items.studios) g WHERE g IN ?)", f.Studios)
	}
	if len(f.Years) > 0 {
		q = q.Where("items.year IN ?", f.Years)
	}
	if len(f.Certificates) > 0 {
		q = q.Where("items.certificate IN ?", f.Certificates)
	}
	if len(f.Resolutions) > 0 {
		var widths []clause.Expression
		for _, r := range f.Resolutions {
			from, to := r.Widths()
			if to == 0 {
				widths = append(widths, clause.Expr{SQL: "v.width >= ?", Vars: []any{from}})
			} else {
				widths = append(widths, clause.Expr{SQL: "v.width >= ? AND v.width < ?", Vars: []any{from, to}})
			}
		}
		q = q.Where("EXISTS ("+versionOf+" AND (?))", clause.OrConditions{Exprs: widths})
	}
	if len(f.Ranges) > 0 {
		q = q.Where("EXISTS ("+versionOf+" AND v.video_range IN ?)", f.Ranges)
	}
	if len(f.People) > 0 {
		people := make([]string, len(f.People))
		for n, p := range f.People {
			people[n] = p.String()
		}
		q = q.Where(`EXISTS (SELECT 1 FROM credits c WHERE c.person_id IN ? AND (c.item_id = items.id
			OR c.item_id IN (SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id WHERE s.parent_id = items.id)))`, people)
	}
	if f.MinRating > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM ratings r WHERE r.item_id = items.id AND r.site = ? AND r.score >= ?)", f.RatingSite, f.MinRating)
	}
	return q
}

// markSQL is the condition a mark sets on a title, for the profile given as its one argument. A
// show is watched when every episode is, as Jellyfin and Plex count it.
func markSQL(m domain.Mark) string {
	unwatched := `EXISTS (SELECT 1 FROM (` + episodesOf + `) e
		LEFT JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
		WHERE w.watched_at IS NULL)`
	switch m {
	case domain.MarkWatched:
		return "NOT " + unwatched
	case domain.MarkUnwatched:
		return unwatched
	case domain.MarkInProgress:
		return unwatched + ` AND EXISTS (SELECT 1 FROM (` + episodesOf + `) e
			JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
			WHERE w.watched_at IS NOT NULL OR w.position_ms > 0)`
	case domain.MarkFavourite:
		return "EXISTS (SELECT 1 FROM favourites f WHERE f.item_id = items.id AND f.profile_id = @profile)"
	}
	return "true"
}

// Facets are the values a library's titles have, which its wall can be narrowed to: what Jellyfin's
// /Items/Filters and Plex's filter values list.
type Facets struct {
	Genres       []string
	Years        []int
	Certificates []string
	Studios      []string
	Resolutions  []domain.Resolution
	Ranges       []domain.Range
	RatingSites  []domain.RatingSite
}

// Facets answers the values a library's films and shows have; ErrNotFound for no such library.
func (s *Store) Facets(ctx context.Context, lib uuid.UUID) (Facets, error) {
	var f Facets
	if _, err := s.wallQuery(ctx, lib, uuid.UUID{}, WallFilter{}); err != nil {
		return f, err
	}
	db := s.q.Item.WithContext(ctx).UnderlyingDB()
	titles := `SELECT * FROM items WHERE library_id = @lib AND kind IN ('movie', 'show')`
	// Copies on disk of the library's films and episodes.
	copies := `SELECT v.* FROM versions v JOIN items e ON e.id = v.item_id
		WHERE e.library_id = @lib AND v.missing_since IS NULL`
	at := sql.Named("lib", lib.String())
	for _, q := range []struct {
		sql  string
		into any
	}{
		{`SELECT DISTINCT g FROM (` + titles + `) t, jsonb_array_elements_text(t.genres) g ORDER BY g`, &f.Genres},
		{`SELECT DISTINCT year FROM (` + titles + `) t WHERE year IS NOT NULL ORDER BY year DESC`, &f.Years},
		{`SELECT DISTINCT certificate FROM (` + titles + `) t WHERE certificate IS NOT NULL ORDER BY certificate`, &f.Certificates},
		{`SELECT DISTINCT st FROM (` + titles + `) t, jsonb_array_elements_text(t.studios) st ORDER BY st`, &f.Studios},
		{`SELECT DISTINCT video_range FROM (` + copies + `) c WHERE video_range IS NOT NULL`, &f.Ranges},
		{`SELECT DISTINCT r.site FROM ratings r JOIN items t ON t.id = r.item_id WHERE t.library_id = @lib`, &f.RatingSites},
	} {
		if err := db.Raw(q.sql, at).Scan(q.into).Error; err != nil {
			return Facets{}, err
		}
	}
	var widths []int
	if err := db.Raw(`SELECT DISTINCT width FROM (`+copies+`) c WHERE width IS NOT NULL`, at).Scan(&widths).Error; err != nil {
		return Facets{}, err
	}
	for _, r := range domain.Resolutions() {
		from, to := r.Widths()
		if slices.ContainsFunc(widths, func(w int) bool { return w >= from && (to == 0 || w < to) }) {
			f.Resolutions = append(f.Resolutions, r)
		}
	}
	f.Ranges = order(f.Ranges, domain.Ranges())
	f.RatingSites = order(f.RatingSites, domain.RatingSites())
	return f, nil
}

// order puts values in the order of all.
func order[T comparable](values, all []T) []T {
	var out []T
	for _, v := range all {
		if slices.Contains(values, v) {
			out = append(out, v)
		}
	}
	return out
}
