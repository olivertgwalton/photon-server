package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// WallFilter narrows a library's titles, as Jellyfin's item filters and Plex's section filters do.
// Each list is any of its values; every field set must hold. A zero filter narrows nothing.
type WallFilter struct {
	// StartsWith is a letter its sort title starts with, unaccented, or "#" for before A.
	StartsWith   string              `json:"starts_with,omitzero"`
	Marks        []domain.Mark       `json:"marks,omitzero"`
	Genres       []string            `json:"genres,omitzero"`
	Years        []int               `json:"years,omitzero"`
	Certificates []string            `json:"certificates,omitzero"`
	Studios      []string            `json:"studios,omitzero"`
	Resolutions  []domain.Resolution `json:"resolutions,omitzero"`
	Ranges       []domain.Range      `json:"ranges,omitzero"`
	// People are credited on it, or on one of its episodes.
	People []uuid.UUID `json:"people,omitzero"`
	// MinRating is the least RatingSite's score out of 100 may be, zero for any.
	RatingSite domain.RatingSite `json:"rating_site,omitzero"`
	MinRating  float64           `json:"min_rating,omitzero"`
}

// Check is what a filter's values must be beyond their kinds: a letter, and a score out of 100.
func (f WallFilter) Check() error {
	if s := f.StartsWith; s != "" && s != "#" && (len(s) != 1 || s < "A" || s > "Z") {
		return errors.New("starts_with is a letter or #")
	}
	if f.MinRating < 0 || f.MinRating > 100 {
		return errors.New("min_rating is a score from 0 to 100")
	}
	return nil
}

// firstLetter is a sort title's first letter unaccented, or "#" for anything before A.
const firstLetter = `CASE WHEN upper(left(unaccent(items.sort_title), 1)) BETWEEN 'A' AND 'Z'
	THEN upper(left(unaccent(items.sort_title), 1)) ELSE '#' END`

// versionOf is a copy on disk of a film, or of any episode of a show. Each level is its own arm,
// as an OR across them is a scan of every item for every title.
const versionOf = `SELECT 1 FROM (
		SELECT items.id
		UNION ALL SELECT c.id FROM items c WHERE c.parent_id = items.id
		UNION ALL SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = items.id
	) t JOIN versions v ON v.item_id = t.id WHERE v.missing_since IS NULL`

// episodesOf are a show's episodes, two levels down, and a film itself.
const episodesOf = `SELECT items.id WHERE items.kind = 'movie'
	UNION ALL SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id
		WHERE s.parent_id = items.id AND e.kind = 'episode'`

// begun is whether the profile given as @profile has watched or started a film, or an episode of a show.
const begun = `EXISTS (SELECT 1 FROM (` + episodesOf + `) e
	JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
	WHERE w.watched_at IS NOT NULL OR w.position_ms > 0)`

// where is the conditions, each led by AND, that narrow a statement over items to what the filter
// lets through, for the profile args holds as @profile; it adds the values they take to args.
func (f WallFilter) where(args pgx.NamedArgs) string {
	var w strings.Builder
	and := func(cond, name string, value any) {
		w.WriteString(" AND " + cond)
		if name != "" {
			args[name] = value
		}
	}
	if f.StartsWith != "" {
		and(firstLetter+" = @letter", "letter", f.StartsWith)
	}
	for _, m := range f.Marks {
		and(markSQL(m), "", nil)
	}
	if len(f.Genres) > 0 {
		and("items.genres ?| @genres", "genres", f.Genres)
	}
	if len(f.Studios) > 0 {
		and("items.studios ?| @studios", "studios", f.Studios)
	}
	if len(f.Years) > 0 {
		and("items.year = ANY(@years)", "years", f.Years)
	}
	if len(f.Certificates) > 0 {
		and("items.certificate = ANY(@certificates)", "certificates", f.Certificates)
	}
	if len(f.Resolutions) > 0 {
		widths := make([]string, len(f.Resolutions))
		for n, r := range f.Resolutions {
			from, to := r.Widths()
			args[fmt.Sprint("from", n)] = from
			widths[n] = fmt.Sprintf("v.width >= @from%d", n)
			if to > 0 {
				args[fmt.Sprint("to", n)] = to
				widths[n] += fmt.Sprintf(" AND v.width < @to%d", n)
			}
		}
		and("EXISTS ("+versionOf+" AND ("+strings.Join(widths, " OR ")+"))", "", nil)
	}
	if len(f.Ranges) > 0 {
		and("EXISTS ("+versionOf+" AND v.video_range = ANY(@ranges))", "ranges", f.Ranges)
	}
	if len(f.People) > 0 {
		// Read from the people's credits up, as one set: asked of each title, the OR across its own
		// credits and its episodes' is a scan of the credits for every title.
		and(`items.id IN (SELECT c.item_id FROM credits c WHERE c.person_id = ANY(@people)
			UNION ALL SELECT s.parent_id FROM credits c JOIN items e ON e.id = c.item_id JOIN items s ON s.id = e.parent_id
			WHERE c.person_id = ANY(@people))`,
			"people", f.People)
	}
	if f.MinRating > 0 {
		args["rating_site"] = f.RatingSite
		and("EXISTS (SELECT 1 FROM ranked_ratings(items) r WHERE r.site = @rating_site AND r.score >= @min_rating)",
			"min_rating", f.MinRating)
	}
	return w.String()
}

// markSQL is the condition a mark sets on a title, for the profile given as @profile. A
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
		return unwatched + ` AND ` + begun
	case domain.MarkFavourite:
		return "EXISTS (SELECT 1 FROM favourites f WHERE f.item_id = items.id AND f.profile_id = @profile)"
	case domain.MarkWatchlist:
		return "EXISTS (SELECT 1 FROM watchlist l WHERE l.item_id = items.id AND l.profile_id = @profile)"
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

// Facets answers the values the films and shows of libraries a profile may see have; ErrNotFound
// for no such library.
func (s *Store) Facets(ctx context.Context, libs []uuid.UUID, profile uuid.UUID) (Facets, error) {
	var f Facets
	_, at, err := s.wallQuery(ctx, libs, profile, WallFilter{})
	if err != nil {
		return f, err
	}
	// The facets are of each library's own kind of title.
	ids, kinds := make([]any, len(libs)), make([]any, len(libs))
	for n := range libs {
		ids[n], kinds[n] = at["lib"+strconv.Itoa(n)], at["kind"+strconv.Itoa(n)]
	}
	at["ids"], at["kinds"] = ids, kinds
	at["sites"] = domain.RatingSites()
	// The titles are read once for all four of their facets. The kinds of copy and the rating sites
	// are those of the titles read, as the wall's filters find them: a copy is a film's, its extras'
	// or a show's episodes'. Each kind and site is asked whether any of those titles holds one,
	// rather than every copy and rating of the library being read.
	var widths []int
	err = s.pool.QueryRow(ctx, `
		WITH titles AS MATERIALIZED (
			SELECT items.id, items.genres, items.studios, items.year, items.certificate FROM items
			JOIN unnest(CAST(@ids AS uuid[]), CAST(@kinds AS text[])) l (lib, kind) ON items.library_id = l.lib AND items.kind = l.kind,
			viewer(@profile) v
			WHERE sees(v, items)
		),
		copies AS MATERIALIZED (
			SELECT k.video_range, k.width FROM (
				SELECT DISTINCT video_range, width FROM versions WHERE library_id = ANY(@libs) AND missing_since IS NULL
			) k
			WHERE EXISTS (SELECT 1 FROM versions c JOIN items e ON e.id = c.item_id LEFT JOIN items p ON p.id = e.parent_id
				JOIN titles t ON t.id = coalesce(p.parent_id, e.parent_id, e.id)
				WHERE c.library_id = ANY(@libs) AND c.missing_since IS NULL
					AND c.video_range IS NOT DISTINCT FROM k.video_range AND c.width IS NOT DISTINCT FROM k.width)
		)
		SELECT
			(SELECT array_agg(DISTINCT g ORDER BY g) FROM titles, jsonb_array_elements_text(titles.genres) g),
			(SELECT array_agg(DISTINCT year ORDER BY year DESC) FROM titles WHERE year IS NOT NULL),
			(SELECT array_agg(DISTINCT certificate ORDER BY certificate) FROM titles WHERE certificate IS NOT NULL),
			(SELECT array_agg(DISTINCT st ORDER BY st) FROM titles, jsonb_array_elements_text(titles.studios) st),
			(SELECT array_agg(DISTINCT video_range) FROM copies WHERE video_range IS NOT NULL),
			(SELECT array_agg(DISTINCT width) FROM copies WHERE width IS NOT NULL),
			(SELECT array_agg(s.site) FROM unnest(CAST(@sites AS text[])) s (site)
				WHERE EXISTS (SELECT 1 FROM ratings r JOIN titles t ON t.id = r.item_id WHERE r.site = s.site))`, at,
	).Scan(&f.Genres, &f.Years, &f.Certificates, &f.Studios, &f.Ranges, &widths, &f.RatingSites)
	if err != nil {
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
