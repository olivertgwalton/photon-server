package store

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// HomeRow is one row of a profile's home page.
type HomeRow struct {
	Kind domain.HomeRow
	// Collection is the collection a RowCollection row is.
	Collection *TitleRef
	// Library is the library a row of libraryRows is.
	Library *LibraryRef
	Cards   []Card
}

// LibraryRef names a library.
type LibraryRef struct {
	ID   uuid.UUID
	Name string
}

// libraryRows are the rows made once for each library the profile sees, as Plex's library hubs
// are, and the kinds of library each is made for. A title in two libraries is in each one's row.
var libraryRows = map[domain.HomeRow][]domain.LibraryKind{
	domain.RowRecentFilms:       {domain.LibraryMovies},
	domain.RowRecentShows:       {domain.LibraryShows},
	domain.RowRecentlyReleased:  {domain.LibraryMovies, domain.LibraryShows},
	domain.RowTopRatedUnwatched: {domain.LibraryMovies, domain.LibraryShows},
}

// rowQueries are the rows' queries, each taking the profile and a limit, a library of the
// libraryRows and an offset of the rest, and holding only what the profile may see; the profile's
// own rows hold a title in several libraries once.
var rowQueries = map[domain.HomeRow]string{
	domain.RowContinueWatching: `
		SELECT ` + itemColumnsOf("i") + ` FROM watch_state w JOIN items i ON i.id = w.item_id
		WHERE w.profile_id = @profile AND w.position_ms > 0 AND i.kind IN ('movie', 'episode')
			AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, i) AND first_of_title(v, i))
		ORDER BY w.last_played_at DESC, i.id DESC OFFSET @offset LIMIT @limit`,
	// As Jellyfin's: the episode after the furthest one watched of each show, in the show's order
	// and specials aside, unless it is under way already and so in Continue Watching; the shows
	// in the order that episode was last played.
	domain.RowNextUp: `
		WITH last AS (
			SELECT DISTINCT ON (show.id) show.id AS show_id, show, e.season_number, e.episode_number,
				w.last_played_at
			FROM watch_state w
			JOIN items e ON e.id = w.item_id AND e.kind = 'episode'
			JOIN items season ON season.id = e.parent_id
			JOIN items show ON show.id = season.parent_id
			WHERE w.profile_id = @profile AND w.watched_at IS NOT NULL AND e.season_number > 0
				AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, show))
			ORDER BY show.id, e.season_number DESC, e.episode_number DESC NULLS LAST
		)
		SELECT ` + itemColumnsOf("next") + ` FROM last
		CROSS JOIN LATERAL (
			SELECT ` + itemColumnsOf("e") + ` FROM items season
			JOIN items e ON e.parent_id = season.id AND e.kind = 'episode'
			LEFT JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
			WHERE season.parent_id = last.show_id AND season.kind = 'season' AND season.season_number > 0
				AND (e.season_number, coalesce(e.episode_number, 0)) > (last.season_number, coalesce(last.episode_number, 0))
				AND w.watched_at IS NULL
			ORDER BY e.season_number, e.episode_number LIMIT 1
		) next
		LEFT JOIN watch_state started ON started.item_id = next.id AND started.profile_id = @profile
		WHERE coalesce(started.position_ms, 0) = 0
			AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE first_of_title(v, last.show))
		ORDER BY last.last_played_at DESC, last.show_id DESC OFFSET @offset LIMIT @limit`,
	domain.RowWatchlist:  listRow("watchlist"),
	domain.RowFavourites: listRow("favourites"),
	domain.RowRecentFilms: `
		SELECT ` + itemColumns + ` FROM items
		WHERE library_id = @lib AND kind = 'movie' AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, items))
		ORDER BY added_at DESC, id DESC LIMIT @limit`,
	// A show by its newest episode. The episodes are walked newest first, one step to the next
	// show not yet found, so only the newest are read rather than every episode of every show.
	// Whether a show is seen is asked of that one show once it is found: asked in the walk's
	// step, the planner would judge every show and sort all their episodes at each step.
	domain.RowRecentShows: `
		WITH RECURSIVE latest AS (
			SELECT 'infinity'::timestamptz AS added_at, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid AS id,
				NULL::uuid AS show_id, '{}'::uuid[] AS found, 0 AS shown, false AS seen
			UNION ALL
			SELECT next.added_at, next.id, next.show_id, latest.found || next.show_id,
				latest.shown + judged.seen::int, judged.seen
			FROM latest
			CROSS JOIN LATERAL (
				SELECT e.added_at, e.id, season.parent_id AS show_id FROM items e
				JOIN items season ON season.id = e.parent_id AND season.kind = 'season'
				WHERE e.library_id = @lib AND e.kind = 'episode' AND (e.added_at, e.id) < (latest.added_at, latest.id)
					AND season.parent_id <> ALL (latest.found)
				ORDER BY e.added_at DESC, e.id DESC LIMIT 1
			) next
			CROSS JOIN LATERAL (
				SELECT EXISTS (SELECT 1 FROM items show, viewer(@profile) v
					WHERE show.id = next.show_id AND show.kind = 'show' AND sees(v, show)) AS seen
			) judged
			WHERE latest.shown < @limit
		)
		SELECT ` + itemColumnsOf("show") + ` FROM latest JOIN items show ON show.id = latest.show_id
		WHERE latest.seen
		ORDER BY latest.added_at DESC, show.id DESC`,
	// A film by its release date, else the first of its year, as the wall sorts; a show by when its
	// newest episode aired, as its page dates it; nothing yet to come.
	domain.RowRecentlyReleased: `
		WITH released AS (
			SELECT id, released_desc AS released FROM items
			WHERE library_id = @lib AND kind = 'movie' AND released_desc BETWEEN current_date - ` + strconv.Itoa(releasedWithinDays) + ` AND current_date
			UNION ALL
			SELECT season.parent_id, max(coalesce(e.release_date, e.air_date)) FROM items e JOIN items season ON season.id = e.parent_id
			WHERE e.library_id = @lib AND e.kind = 'episode' AND coalesce(e.release_date, e.air_date) BETWEEN current_date - ` + strconv.Itoa(releasedWithinDays) + ` AND current_date
			GROUP BY season.parent_id
		)
		SELECT ` + itemColumnsOf("i") + ` FROM released JOIN items i ON i.id = released.id
		WHERE EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, i))
		ORDER BY released.released DESC, i.id DESC LIMIT @limit`,
	// The titles the profile has not begun by their IMDb rating, as the wall sorts by it, leaving
	// out a rating too few voted for where its site counts votes. A title's best rating is the one
	// no other of its own beats, so the ratings are read down their index and only as many titles
	// as the row takes are tried.
	domain.RowTopRatedUnwatched: `
		SELECT ` + itemColumns + ` FROM ratings r JOIN items ON items.id = r.item_id
		WHERE r.site = '` + string(domain.SiteIMDb) + `' AND (r.votes IS NULL OR r.votes >= ` + strconv.Itoa(leastVotes) + `)
			AND NOT EXISTS (SELECT 1 FROM ratings o WHERE o.item_id = r.item_id AND o.site = r.site
				AND (o.votes IS NULL OR o.votes >= ` + strconv.Itoa(leastVotes) + `)
				AND (o.score > r.score OR o.score = r.score AND o.source < r.source))
			AND items.library_id = @lib AND items.kind IN ('movie', 'show') AND NOT ` + begun + `
			AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, items))
		ORDER BY r.score DESC, r.item_id DESC LIMIT @limit`,
}

// onList is the titles on one of the profile's lists, its watchlist or its favourites, that it
// sees.
func onList(table string) string {
	return ` FROM ` + table + ` l JOIN items i ON i.id = l.item_id
		WHERE l.profile_id = @profile AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, i) AND first_of_title(v, i))`
}

// listRow is one of the profile's lists, the latest added first.
func listRow(table string) string {
	return `SELECT ` + itemColumnsOf("i") + onList(table) + ` ORDER BY l.added_at DESC, i.id DESC OFFSET @offset LIMIT @limit`
}

// collectionRowsQuery is, for each collection placed on the home page that the profile sees, by
// name, up to the limit of its titles the profile sees, in memberOrder.
var collectionRowsQuery = `
	SELECT col.id AS collection_id, col.title AS collection_title, ` + itemColumnsOf("member") + `
	FROM collections c JOIN items col ON col.id = c.item_id
	CROSS JOIN LATERAL (
		SELECT items.*, row_number() OVER (ORDER BY ` + memberOrder + `) AS n
		FROM collection_members m JOIN items ON items.id = m.item_id
		WHERE m.collection_id = c.item_id AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, items))
		ORDER BY n LIMIT @limit
	) member
	WHERE c.placement = 'home' AND c.item_id IN (` + shownCollections + `)
		AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, col))
	ORDER BY col.sort_title, col.id, member.n`

// librariesSeen are the libraries a profile sees, in the order it put them, those it has not placed
// after, by name, as its library list is.
const librariesSeen = `
	SELECT l.id, l.name, l.kind FROM libraries l CROSS JOIN viewer(@profile) v
	LEFT JOIN library_order o ON o.library_id = l.id AND o.profile_id = @profile
	WHERE v.libraries IS NULL OR l.id = ANY (v.libraries)
	ORDER BY o.position NULLS LAST, l.name, l.id`

type SeenLibrary struct {
	ID   uuid.UUID
	Name string
	Kind domain.LibraryKind
}

// LibrariesSeen answers the libraries a profile sees, in the order it put them.
func (s *Store) LibrariesSeen(ctx context.Context, profile uuid.UUID) ([]*SeenLibrary, error) {
	return queryRows[SeenLibrary](ctx, s.pool, librariesSeen, pgx.NamedArgs{"profile": profile})
}

// rowItems are a home row and its titles, before their cards are made.
type rowItems struct {
	row   HomeRow
	items []*model.Item
}

type collectionMember struct {
	CollectionID    uuid.UUID
	CollectionTitle string
	model.Item
}

// leastVotes is how many votes a rating needs to put a title on the top rated row, so a title a
// handful rated highly does not lead it.
const leastVotes = 1000

// releasedWithinDays is how lately a title is released to be on the recently released row: a
// year, as a film's files come months after it opens in cinemas.
const releasedWithinDays = 365

// Home answers a profile's home page: each row it shows with anything in it, in the order it
// arranged them, up to limit cards each.
func (s *Store) Home(ctx context.Context, profile uuid.UUID, limit int) ([]HomeRow, error) {
	prefs, err := s.Preferences(ctx, profile)
	if err != nil {
		return nil, err
	}
	libs, err := s.LibrariesSeen(ctx, profile)
	if err != nil {
		return nil, err
	}
	// Each row is asked for at once, on a connection of its own, and kept in the profile's order.
	var asks []func(context.Context) ([]rowItems, error)
	args := func(lib uuid.UUID) pgx.NamedArgs {
		return pgx.NamedArgs{"profile": profile, "limit": limit, "offset": 0, "lib": lib}
	}
	of := func(row HomeRow, args pgx.NamedArgs) func(context.Context) ([]rowItems, error) {
		return func(ctx context.Context) ([]rowItems, error) {
			items, err := queryRows[model.Item](ctx, s.pool, rowQueries[row.Kind], args)
			if err != nil || len(items) == 0 {
				return nil, err
			}
			return []rowItems{{row, items}}, nil
		}
	}
	for _, section := range prefs.Home {
		kind := section.Row
		if section.Visibility == domain.RowHidden {
			continue
		}
		if kind == domain.RowCollection {
			asks = append(asks, func(ctx context.Context) ([]rowItems, error) {
				members, err := queryRows[collectionMember](ctx, s.pool, collectionRowsQuery, args(uuid.UUID{}))
				var out []rowItems
				for n, m := range members {
					if n == 0 || m.CollectionID != members[n-1].CollectionID {
						out = append(out, rowItems{row: HomeRow{Kind: kind, Collection: &TitleRef{ID: m.CollectionID, Title: m.CollectionTitle}}})
					}
					out[len(out)-1].items = append(out[len(out)-1].items, &m.Item)
				}
				return out, err
			})
			continue
		}
		if kinds, ok := libraryRows[kind]; ok {
			for _, lib := range libs {
				if slices.Contains(kinds, lib.Kind) {
					asks = append(asks, of(HomeRow{Kind: kind, Library: &LibraryRef{ID: lib.ID, Name: lib.Name}}, args(lib.ID)))
				}
			}
			continue
		}
		asks = append(asks, of(HomeRow{Kind: kind}, args(uuid.UUID{})))
	}
	found := make([][]rowItems, len(asks))
	g, gctx := errgroup.WithContext(ctx)
	for n, ask := range asks {
		g.Go(func() (err error) {
			found[n], err = ask(gctx)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var rows []HomeRow
	var lengths []int
	var all []*model.Item
	for _, f := range found {
		for _, r := range f {
			rows = append(rows, r.row)
			lengths = append(lengths, len(r.items))
			all = append(all, r.items...)
		}
	}
	if len(all) == 0 {
		return rows, nil
	}
	// Every row's cards are made at once, so the page costs the same few queries however many
	// rows it has.
	cards, err := s.cards(ctx, profile, all)
	if err != nil {
		return nil, err
	}
	for n, length := range lengths {
		rows[n].Cards, cards = cards[:length], cards[length:]
	}
	return rows, nil
}

// RowPage answers a page of one of the profile's own home rows, in the row's order, and how many
// titles it holds: ErrNotFound for a library's or a collection's row, which lead to its own page.
func (s *Store) RowPage(ctx context.Context, profile uuid.UUID, row domain.HomeRow, offset, limit int) ([]Card, int64, error) {
	q, ok := rowQueries[row]
	if _, ofLibrary := libraryRows[row]; !ok || ofLibrary {
		return nil, 0, ErrNotFound
	}
	// LIMIT NULL is no limit.
	all := pgx.NamedArgs{"profile": profile, "offset": 0, "limit": nil}
	var items []*model.Item
	total, err := s.counted(ctx, `SELECT count(*) FROM (`+q+`) page`, all, func(ctx context.Context) (err error) {
		items, err = queryRows[model.Item](ctx, s.pool, q, pgx.NamedArgs{"profile": profile, "offset": offset, "limit": limit})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, items)
	return cards, total, err
}

// LibraryRow answers the first cards, up to limit, of a row made for each library, of library:
// ErrNotFound for any other row.
func (s *Store) LibraryRow(ctx context.Context, profile uuid.UUID, row domain.HomeRow, library uuid.UUID, limit int) ([]Card, error) {
	if _, ok := libraryRows[row]; !ok {
		return nil, ErrNotFound
	}
	items, err := queryRows[model.Item](ctx, s.pool, rowQueries[row], pgx.NamedArgs{"profile": profile, "limit": limit, "offset": 0, "lib": library})
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, items)
}

// ErrNoNext is a title with no episode to play next: a film, a show with no episodes the profile
// has, or the last episode.
var ErrNoNext = errors.New("no episode follows")

// nextQueries answer the episode to play next, in the show's order (its seasons and episodes as
// its files number them, in whichever order it is set to) and, as the next up row, specials aside
// unless the title is one of them. A show's episodes are visible to a profile as the show is.
var nextQueries = map[domain.ItemKind]string{
	// After an episode, the one after it, watched or not, as a player's Up Next.
	domain.ItemEpisode: `
		SELECT ` + itemColumnsOf("e") + ` FROM items e
		JOIN items season ON season.id = e.parent_id
		JOIN items here ON here.id = @id
		WHERE e.kind = 'episode' AND season.parent_id = (SELECT parent_id FROM items WHERE id = here.parent_id)
			AND (e.season_number, coalesce(e.episode_number, 0)) > (here.season_number, coalesce(here.episode_number, 0))
			AND (e.season_number > 0 OR here.season_number = 0)
		ORDER BY e.season_number, e.episode_number, e.id LIMIT 1`,
	// Of a show or a season, where the profile is: the episode under way, else the first unwatched
	// after the furthest one watched, else the first.
	domain.ItemShow:   resumeQuery,
	domain.ItemSeason: resumeQuery,
}

const resumeQuery = `
	WITH e AS (
		SELECT e.id, coalesce(w.position_ms, 0) > 0 AS started, w.watched_at IS NOT NULL AS watched,
			w.last_played_at, row_number() OVER (ORDER BY e.season_number, e.episode_number, e.id) AS n
		FROM items e
		JOIN items season ON season.id = e.parent_id
		LEFT JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
		WHERE e.kind = 'episode' AND (season.id = @id OR (season.parent_id = @id AND season.season_number > 0))
	), last AS (
		SELECT max(n) AS n FROM e WHERE watched
	)
	SELECT ` + itemColumns + ` FROM items WHERE id = (
		SELECT id FROM e ORDER BY
			CASE WHEN started THEN 0 WHEN NOT watched AND n > coalesce((SELECT n FROM last), 0) THEN 1 ELSE 2 END,
			CASE WHEN started THEN last_played_at END DESC NULLS LAST, n
		LIMIT 1)`

// Next answers the episode to play after a title, as a card: ErrNotFound for a title the profile
// may not see, ErrNoNext where nothing follows.
func (s *Store) Next(ctx context.Context, profile, id uuid.UUID) (Card, error) {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return Card{}, err
	}
	if ok, err := s.visible(ctx, profile, id); err != nil || !ok {
		return Card{}, cmp.Or(err, ErrNotFound)
	}
	q, ok := nextQueries[item.Kind]
	if !ok {
		return Card{}, ErrNoNext
	}
	rows, err := queryRows[model.Item](ctx, s.pool, q, pgx.NamedArgs{"profile": profile, "id": id})
	if err != nil {
		return Card{}, err
	}
	if len(rows) == 0 {
		return Card{}, ErrNoNext
	}
	cards, err := s.cards(ctx, profile, rows)
	if err != nil {
		return Card{}, err
	}
	return cards[0], nil
}
