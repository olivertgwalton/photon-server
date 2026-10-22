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

// firstLetter is the first letter of a row's sort title unaccented, or "#" for anything before A.
func firstLetter(row string) string {
	return `CASE WHEN upper(left(unaccent(` + row + `.sort_title), 1)) BETWEEN 'A' AND 'Z'
	THEN upper(left(unaccent(` + row + `.sort_title), 1)) ELSE '#' END`
}

// versionOf is a version on disk of a film, or of any episode of a show. Each level is its own arm,
// as an OR across them is a scan of every item for every title.
const versionOf = `SELECT 1 FROM (
		SELECT items.id
		UNION ALL SELECT c.id FROM items c WHERE c.parent_id = items.id
		UNION ALL SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = items.id
	) t JOIN versions v ON v.item_id = t.id WHERE v.missing_since IS NULL`

// begun is whether the profile given as @profile has watched or started a film, or an episode of a
// show. A show is asked of the counts kept of its episodes: one with none of them watched or
// played is not begun, one with any watched is, and only one with episodes played but none
// watched has its episodes looked up for one under way.
const begun = `CASE WHEN items.kind = 'show' THEN EXISTS (SELECT 1 FROM watched_counts c
		WHERE c.profile_id = @profile AND c.group_id = items.id AND (c.watched > 0 OR EXISTS (SELECT 1 FROM items season
			JOIN items e ON e.parent_id = season.id JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
			WHERE season.parent_id = items.id AND e.kind = 'episode' AND w.position_ms > 0)))
	ELSE items.kind IN ('movie', 'episode') AND EXISTS (SELECT 1 FROM watch_state w
		WHERE w.profile_id = @profile AND w.item_id = items.id AND (w.watched_at IS NOT NULL OR w.position_ms > 0)) END`

// where is the conditions, each led by AND, that narrow a statement over items to what the filter
// lets through, for the profile args holds as @profile; it adds the values they take to args. A
// title's letter, genres, studios and certificate are read from row: an episode's are its show's,
// as Plex's show.genre filters episodes.
func (f WallFilter) where(args pgx.NamedArgs, row string) string {
	var w strings.Builder
	and := func(cond, name string, value any) {
		w.WriteString(" AND " + cond)
		if name != "" {
			args[name] = value
		}
	}
	if f.StartsWith != "" {
		and(firstLetter(row)+" = @letter", "letter", f.StartsWith)
	}
	for _, m := range f.Marks {
		and(markSQL(m), "", nil)
	}
	if len(f.Genres) > 0 {
		and(row+".genres ?| @genres", "genres", f.Genres)
	}
	if len(f.Studios) > 0 {
		and(row+".studios ?| @studios", "studios", f.Studios)
	}
	if len(f.Years) > 0 {
		and("items.year = ANY(@years)", "years", f.Years)
	}
	if len(f.Certificates) > 0 {
		and(row+".certificate = ANY(@certificates)", "certificates", f.Certificates)
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
	// A show's episodes are counted from what is kept of them, not looked up one by one.
	unwatched := `CASE WHEN items.kind = 'show' THEN EXISTS (SELECT 1 FROM episode_counts c
			LEFT JOIN watched_counts w ON w.group_id = c.group_id AND w.profile_id = @profile
			WHERE c.group_id = items.id AND c.episodes > coalesce(w.watched, 0))
		ELSE items.kind IN ('movie', 'episode') AND NOT EXISTS (SELECT 1 FROM watch_state w
			WHERE w.item_id = items.id AND w.profile_id = @profile AND w.watched_at IS NOT NULL) END`
	switch m {
	case domain.MarkWatched:
		return "NOT " + unwatched
	case domain.MarkUnwatched:
		return unwatched
	case domain.MarkInProgress:
		// A film or an episode is under way when its one watch state is played part way and not
		// watched, which is a single lookup rather than one for each half.
		return `CASE WHEN items.kind = 'show' THEN ` + unwatched + ` AND ` + begun + `
			ELSE items.kind IN ('movie', 'episode') AND EXISTS (SELECT 1 FROM watch_state w
				WHERE w.item_id = items.id AND w.profile_id = @profile AND w.watched_at IS NULL AND w.position_ms > 0) END`
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

// Facets answers the values the films and shows of libraries, or their episodes, a profile may see
// have; ErrNotFound for no such library.
func (s *Store) Facets(ctx context.Context, libs []uuid.UUID, profile uuid.UUID, wall domain.WallKind) (Facets, error) {
	var f Facets
	_, at, err := s.wallQuery(ctx, libs, profile, wall, WallFilter{})
	if err != nil {
		return f, err
	}
	// The facets are of the kinds of title each library's wall lists.
	ids, kinds := []any{}, []any{}
	for n := 0; at["lib"+strconv.Itoa(n)] != nil; n++ {
		ids, kinds = append(ids, at["lib"+strconv.Itoa(n)]), append(kinds, at["kind"+strconv.Itoa(n)])
	}
	at["ids"], at["kinds"] = ids, kinds
	at["sites"] = domain.RatingSites()
	// The titles are read once for all four of their facets. The kinds of version and the rating sites
	// are those of the titles read, as the wall's filters find them: a version is a film's, its extras'
	// or a show's episodes'. Each kind and site is asked whether any of those titles holds one,
	// rather than every version and rating of the library being read.
	from, row, seen := wallRows(wall)
	// An episode's versions are its own; a title's are its extras' and its episodes' too. A title's
	// genres, studios and certificate are its own, and an episode's its show's, read once from each
	// show the episodes are of: copied to each episode, they were most of the facets' time.
	owner := "coalesce(p.parent_id, e.parent_id, e.id)"
	described, about := row+".genres, "+row+".studios, "+row+".certificate", "titles"
	if wall == domain.WallEpisodes {
		owner = "e.id"
		described, about = row+".id AS show", "(SELECT genres, studios, certificate FROM items WHERE id IN (SELECT show FROM titles))"
	}
	var widths []int
	// Genres and studios are ordered by their letters, accents and case aside, as a reader reads
	// them, whatever collation the database was made with.
	err = s.pool.QueryRow(ctx, `
		WITH titles AS MATERIALIZED (
			SELECT items.id, items.year, `+described+` FROM `+from+`
			JOIN unnest(CAST(@ids AS uuid[]), CAST(@kinds AS text[])) l (lib, kind) ON items.library_id = l.lib AND items.kind = l.kind,
			viewer(@profile) v
			WHERE `+seen+`
		),
		pictures AS MATERIALIZED (
			SELECT k.video_range, k.width FROM (
				SELECT DISTINCT video_range, width FROM versions WHERE library_id = ANY(@libs) AND missing_since IS NULL
			) k
			WHERE EXISTS (SELECT 1 FROM versions c JOIN items e ON e.id = c.item_id LEFT JOIN items p ON p.id = e.parent_id
				JOIN titles t ON t.id = `+owner+`
				WHERE c.library_id = ANY(@libs) AND c.missing_since IS NULL
					AND c.video_range IS NOT DISTINCT FROM k.video_range AND c.width IS NOT DISTINCT FROM k.width)
		)
		SELECT
			(SELECT array_agg(g ORDER BY lower(unaccent(g)), g) FROM (SELECT DISTINCT jsonb_array_elements_text(genres) g FROM `+about+` t) d),
			(SELECT array_agg(DISTINCT year ORDER BY year DESC) FROM titles WHERE year IS NOT NULL),
			(SELECT array_agg(DISTINCT certificate ORDER BY certificate) FROM `+about+` t WHERE certificate IS NOT NULL),
			(SELECT array_agg(st ORDER BY lower(unaccent(st)), st) FROM (SELECT DISTINCT jsonb_array_elements_text(studios) st FROM `+about+` t) d),
			(SELECT array_agg(DISTINCT video_range) FROM pictures WHERE video_range IS NOT NULL),
			(SELECT array_agg(DISTINCT width) FROM pictures WHERE width IS NOT NULL),
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
