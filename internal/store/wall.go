package store

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Card is a title as a wall shows it.
type Card struct {
	ID          uuid.UUID
	Kind        domain.ItemKind
	Title       string
	Year        int
	ReleaseDate time.Time
	AddedAt     time.Time
	// Poster and Backdrop are the title's best pictures of each kind, by id.
	Poster   uuid.UUID
	Backdrop uuid.UUID
	// State is what the profile asking has made of it.
	State TitleState
	// DurationMS is how long it runs, for a progress bar: its longest copy on disk.
	DurationMS int64
	// VersionCount is how many copies of it are on disk, for a choice of which to play.
	VersionCount int
	// An episode's card says which show and season it is of, where in them, and carries its still
	// and its season's poster, which a search shows it by.
	Show          *TitleRef
	Season        *TitleRef
	SeasonPoster  uuid.UUID
	SeasonNumber  *int
	EpisodeNumber *int
	EpisodeEnd    *int
	// AirsBeforeSeason, AirsBeforeEpisode and AirsAfterSeason are where a special aired among the
	// show's seasons.
	AirsBeforeSeason, AirsBeforeEpisode, AirsAfterSeason *int
	Thumb                                                uuid.UUID
	// ShowThumb is an episode's show's thumb, the landscape picture lettered with its name, which a
	// row of what is watched draws the episode by where the profile chose it. It is not answered
	// beside the card's own.
	ShowThumb uuid.UUID
	// Origin is who made a collection: an admin's is changed by hand, a provider's only by it.
	Origin domain.CollectionOrigin
	// What a showcase or a wall's hover says of it: its write-up, lettering, genres, certificate
	// and each site's score.
	Overview    string
	Logo        uuid.UUID
	Genres      []string
	Certificate string
	Ratings     []domain.Rating
	Blurhashes  Blurhashes
}

// WallPage asks for one page of a library's titles: Limit of them from Offset, as Jellyfin's
// StartIndex and Plex's X-Plex-Container-Start page, narrowed by Filter.
type WallPage struct {
	Profile uuid.UUID
	// Kind is what the wall pages, its titles where it names none.
	Kind  domain.WallKind
	Sort  domain.WallSort
	Order domain.Order
	// RatingSite is whose rating SortRating sorts by.
	RatingSite domain.RatingSite
	Filter     WallFilter
	// BoxSets is how it shows every box set: collapsed, expanded or beside its titles; where it says
	// nothing, as each collection's own mode says, else its library's.
	BoxSets domain.BoxSetMode
	Offset  int
	Limit   int
}

// Wall answers a page of the films and shows of libraries, or of their episodes, sorted together,
// by id, and how many there are in all; Cards draws them. Ties in the sort are broken by id, so a
// page is the same whenever it is asked for while the libraries are.
func (s *Store) Wall(ctx context.Context, libs []uuid.UUID, p WallPage) ([]uuid.UUID, int64, error) {
	with, where, boxed, args, err := s.wallWhere(ctx, libs, p.Profile, p.Kind, p.Filter, p.BoxSets)
	if err != nil {
		return nil, 0, err
	}
	from, _, _ := wallRows(p.Kind)
	join, order := p.orderBy(args)
	args["offset"], args["limit"] = p.Offset, p.Limit
	count := `SELECT count(*) FROM ` + from + ` ` + where
	page := `SELECT items.id FROM ` + from + join + ` ` + where + ` ` + order + ` OFFSET @offset LIMIT @limit`
	if boxed != "" {
		// The titles and the box sets are each read to the page's end in order, and merged: as one
		// WHERE, the box sets' rows in no library cost the titles' index its order, and the wall a
		// sort of the whole library.
		_, key, nulls := p.sortKey(args)
		outer := `ORDER BY wall_key ` + p.dir() + nulls + `, items.id ` + p.dir()
		args["end"] = p.Offset + p.Limit
		arm := func(where, key, order string) string {
			return `(SELECT items.id, ` + key + ` AS wall_key FROM items` + join + ` ` + where + ` ` + order + ` LIMIT @end)`
		}
		// A box set is as old as its first title, as Jellyfin dates one.
		setKey := key
		if p.Sort == domain.SortReleased {
			setKey = firstReleased(p.dir())
		}
		count = `SELECT (` + count + `) + (SELECT count(*) FROM items ` + boxed + `)`
		page = `SELECT items.id FROM (` + arm(where, key, order) + ` UNION ALL ` + arm(boxed, setKey, outer) + `) items
			` + outer + ` OFFSET @offset LIMIT @limit`
	}
	var ids []uuid.UUID
	total, err := s.counted(ctx, with+count, args, func(ctx context.Context) (err error) {
		ids, err = queryColumn[uuid.UUID](ctx, s.pool, with+page, args)
		return err
	})
	return ids, total, err
}

// orderBy is the ORDER BY of a page of a wall, its ties broken by id, and what it joins to the
// wall's rows to read, adding the values it takes to args. Episodes are in their shows' order where
// the sort ties, as Plex's default episode sort has them, and by title means by show; every key
// runs the one way, as Jellyfin's SortOrder does.
func (p WallPage) orderBy(args pgx.NamedArgs) (join, order string) {
	join, key, nulls := p.sortKey(args)
	dir := p.dir()
	if p.Kind != domain.WallEpisodes {
		return join, "ORDER BY " + key + " " + dir + nulls + ", items.id " + dir
	}
	order = "ORDER BY "
	if p.Sort != domain.SortTitle {
		order += key + " " + dir + nulls + ", "
	}
	return join, order + strings.Join([]string{"show.sort_title", "show.id", "items.season_number", "items.episode_number", "items.id"}, " "+dir+", ") + " " + dir
}

func (p WallPage) dir() string {
	if p.Order == domain.Descending {
		return "DESC"
	}
	return "ASC"
}

// sortKey is what a wall's page is sorted by before its ties, what it joins to the wall's rows to
// read it, and where it puts what has none.
func (p WallPage) sortKey(args pgx.NamedArgs) (join, key, nulls string) {
	// What has no value to sort by comes last whichever way the rest run. The columns are never
	// null, and saying so of them would stop their indexes reading a page in descending order.
	switch p.Sort {
	case domain.SortAdded:
		key = "items.added_at"
	case domain.SortReleased:
		key = "items.released_asc"
		if p.dir() == "DESC" {
			key = "items.released_desc"
		}
		if p.Kind == domain.WallEpisodes {
			key = episodeAired(p.dir())
		}
	case domain.SortRating:
		join = ` LEFT JOIN shown_ratings rated ON rated.item_id = items.id AND rated.site = @sort_site
			AND rated.library_id = ANY(@libs) AND rated.kind = ANY(@sort_kinds)`
		key, nulls = "rated.score", " NULLS LAST"
		args["sort_site"], args["sort_kinds"] = p.RatingSite, cmp.Or(p.Kind, domain.WallTitles).Of(domain.LibraryMixed)
	case domain.SortRuntime:
		// Each is its longest copy on disk, the wall's copies read once rather than asked after for
		// each title: a third quicker over seventy thousand.
		join = ` LEFT JOIN (SELECT v.item_id, max(v.duration_ms) AS ms FROM versions v
			WHERE v.library_id = ANY(@libs) AND v.missing_since IS NULL GROUP BY v.item_id) runtime ON runtime.item_id = items.id`
		key, nulls = "runtime.ms", " NULLS LAST"
	case domain.SortPlayed:
		// A show's is its episodes' latest, as the counts kept of them say.
		key, nulls = `coalesce((SELECT w.last_played_at FROM watch_state w WHERE w.profile_id = @profile AND w.item_id = items.id),
			(SELECT c.last_played_at FROM watched_counts c WHERE c.profile_id = @profile AND c.group_id = items.id))`, " NULLS LAST"
		// An episode is its own one watch, joined rather than asked after for each so the sort reads
		// in parallel: three times quicker over half a million.
		if p.Kind == domain.WallEpisodes {
			join, key = " LEFT JOIN watch_state played ON played.item_id = items.id AND played.profile_id = @profile", "played.last_played_at"
		}
	case domain.SortTitle:
		key = "items.sort_title"
	}
	return join, key, nulls
}

// episodeAired is when an episode came out, for a wall sorted in dir: its release date, else the
// day it aired, and one undated last whichever way, as released_asc and released_desc pad a title.
func episodeAired(dir string) string {
	if dir == "DESC" {
		return "coalesce(items.release_date, items.air_date, '0001-01-01')"
	}
	return "coalesce(items.release_date, items.air_date, '9999-12-31')"
}

// wallQuery is the WHERE, over wallRows, of a library's films or shows a profile may see, or of its
// episodes, as a filter narrows them, and the values they take, each title on its own; ErrNotFound
// for no such library.
func (s *Store) wallQuery(ctx context.Context, libs []uuid.UUID, profile uuid.UUID, wall domain.WallKind, f WallFilter) (string, pgx.NamedArgs, error) {
	_, where, _, args, err := s.wallWhere(ctx, libs, profile, wall, f, domain.BoxSetsExpanded)
	return where, args, err
}

// wallWhere is wallQuery with box sets shown as boxes says, every one; where it says nothing, as
// each collection's own mode says, else its library's. Where some are shown, boxed is the WHERE,
// over items, of those box sets, where's titles leave out those a collapsed one holds, and with is
// the WITH both read the box sets' titles from, which leads the statement. Each
// kind of item a library's wall lists is its own arm, which lets the wall's sort indexes read a
// page in order.
func (s *Store) wallWhere(ctx context.Context, libs []uuid.UUID, profile uuid.UUID, wall domain.WallKind, f WallFilter, boxes domain.BoxSetMode) (with, where, boxed string, args pgx.NamedArgs, err error) {
	rows, err := s.pool.Query(ctx, `SELECT id, kind, box_sets, EXISTS (SELECT 1 FROM collections WHERE box_set IN ('collapsed', 'beside'))
		FROM libraries WHERE id = ANY($1) ORDER BY id`, libs)
	if err != nil {
		return "", "", "", nil, err
	}
	args = pgx.NamedArgs{"libs": libs, "profile": profile}
	// A wall that names no kind pages titles.
	wall = cmp.Or(wall, domain.WallTitles)
	_, row, seen := wallRows(wall)
	filter := f.where(args, row)
	// Each library's titles are what its kind makes them.
	var arms []string
	boxedLibs, boxedKinds := []uuid.UUID{}, []domain.ItemKind{}
	// The libraries whose own mode shows box sets, by the mode.
	shownLibs := map[domain.BoxSetMode][]uuid.UUID{domain.BoxSetsCollapsed: {}, domain.BoxSetsBeside: {}}
	var id uuid.UUID
	var kind domain.LibraryKind
	var mode domain.BoxSetMode
	var setShown, showing bool
	found, err := pgx.ForEachRow(rows, []any{&id, &kind, &mode, &setShown}, func() error {
		showing = showing || mode != domain.BoxSetsExpanded || setShown
		if libs, ok := shownLibs[mode]; ok {
			shownLibs[mode] = append(libs, id)
		}
		for _, item := range wall.Of(kind) {
			n := strconv.Itoa(len(arms))
			args["lib"+n], args["kind"+n] = id, item
			arms = append(arms, `(items.library_id = @lib`+n+` AND items.kind = @kind`+n+`)`)
			boxedLibs, boxedKinds = append(boxedLibs, id), append(boxedKinds, item)
		}
		return nil
	})
	if err != nil {
		return "", "", "", nil, err
	}
	if int(found.RowsAffected()) != len(libs) {
		return "", "", "", nil, ErrNotFound
	}
	where = `WHERE items.library_id = ANY(@libs)
		AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE ` + seen + ` AND (` + cmp.Or(strings.Join(arms, " OR "), "false") + `))` + filter
	// A filtered wall answers titles alone, as Jellyfin's does: a box set has no genre or year.
	if boxes == domain.BoxSetsExpanded || filter != "" || wall == domain.WallEpisodes || !showing && boxes == "" {
		return "", where, "", args, nil
	}
	args["boxed_libs"], args["boxed_kinds"] = boxedLibs, boxedKinds
	args["collapsed_libs"], args["beside_libs"] = shownLibs[domain.BoxSetsCollapsed], shownLibs[domain.BoxSetsBeside]
	args["boxes"] = (*domain.BoxSetMode)(nil)
	if boxes != "" {
		args["boxes"] = &boxes
	}
	// Seeing a box set's titles, the profile sees it. Each set's titles are counted in one pass:
	// as an IN of boxedSets, the sets were grouped again for every title in one, most of a page's time.
	// They are made once for the statement, though the titles and the sets both read them: made for
	// each, they were half a small library's page.
	return `WITH boxed_titles AS MATERIALIZED (` + boxedTitles + `) `,
		where + ` AND items.id NOT IN (SELECT b.item_id FROM (SELECT b.item_id, b.mode,
			count(*) OVER (PARTITION BY b.collection_id) AS shown FROM boxed_titles b) b
			WHERE b.mode = 'collapsed' AND b.shown >= ` + strconv.Itoa(minShown) + `)`,
		`WHERE items.kind = 'collection' AND items.id IN (` + boxedSets + `)`, args, nil
}

// boxedTitles are the titles of @boxed_libs and @boxed_kinds the profile may see in the
// collections the wall shows, by collection, each with the mode it is shown in there: collapsed,
// in place of its titles, or beside them. The viewer is made once: joined, it was made again for
// every title, ten times the time.
const boxedTitles = `WITH seer AS MATERIALIZED (SELECT v FROM viewer(@profile) v)
	SELECT * FROM (SELECT m.collection_id, m.item_id, coalesce(CAST(@boxes AS text), nullif(c.box_set, 'library'),
			CASE WHEN t.library_id = ANY(@collapsed_libs) THEN 'collapsed' WHEN t.library_id = ANY(@beside_libs) THEN 'beside' END) AS mode
		FROM collection_members m JOIN collections c ON c.item_id = m.collection_id JOIN items t ON t.id = m.item_id, seer
		WHERE (t.library_id, t.kind) IN (SELECT * FROM unnest(CAST(@boxed_libs AS uuid[]), CAST(@boxed_kinds AS text[])))
			AND sees(seer.v, t)) shown
	WHERE mode IN ('collapsed', 'beside')`

// boxedSets are the collections a wall shows among its titles: those holding minShown of
// boxedTitles. A box set is in no library, so it is on each wall where it holds enough of the
// titles.
var boxedSets = `SELECT b.collection_id FROM boxed_titles b GROUP BY b.collection_id HAVING count(*) >= ` + strconv.Itoa(minShown)

// wallRows are the items a wall pages, as from; the row of them whose letter, genres, studios and
// certificate a title has, an episode's being its show's; and whether the viewer v sees one. An
// episode's show is said to be a show of the wall's libraries, as it is of its episode's, which lets
// Postgres judge how many episodes there are by their shows: judged by the episodes, every one
// joined to a season and a show it took for one in a hundred, and Postgres sorted every episode to
// page them, where it now walks the shows in order and stops at the page.
func wallRows(wall domain.WallKind) (from, row, seen string) {
	if wall == domain.WallEpisodes {
		return `items JOIN items season ON season.id = items.parent_id
			JOIN items show ON show.id = season.parent_id AND show.kind = 'show' AND show.library_id = ANY(@libs)`, "show",
			"sees(v, items, season, show)"
	}
	return "items", "items", "sees(v, items)"
}

// Letter is how many of a library's titles sort under a letter: "#" for those before A.
type Letter struct {
	Letter string
	Count  int
}

// Letters counts a library's titles, as a filter narrows them, by the first letter they sort by,
// in title order, as Plex's firstCharacter does, so a client can jump to a letter by its offset; its
// episodes by their shows'. Letters are read unaccented, so "Émile" counts under E where the wall
// sorts it.
func (s *Store) Letters(ctx context.Context, lib, profile uuid.UUID, wall domain.WallKind, f WallFilter) ([]Letter, error) {
	with, where, boxed, args, err := s.wallWhere(ctx, []uuid.UUID{lib}, profile, wall, f, "")
	if err != nil {
		return nil, err
	}
	// Each sort title is read once: unaccenting it for each of a show's episodes doubled the time.
	from, row, _ := wallRows(wall)
	titles := `SELECT ` + row + `.sort_title, count(*) AS n FROM ` + from + ` ` + where + ` GROUP BY ` + row + `.sort_title`
	if boxed != "" {
		titles += ` UNION ALL SELECT items.sort_title, count(*) FROM items ` + boxed + ` GROUP BY items.sort_title`
	}
	out, err := queryStructs[Letter](ctx, s.pool, with+`SELECT `+firstLetter("t")+` AS letter, sum(t.n)::int AS count
		FROM (`+titles+`) t GROUP BY letter ORDER BY letter`, args)
	// Titles before A sort first in the wall, whatever the collation makes of "#".
	if n := slices.IndexFunc(out, func(l Letter) bool { return l.Letter == "#" }); n > 0 {
		out = append([]Letter{out[n]}, slices.Delete(out, n, n+1)...)
	}
	return out, err
}

// PlaybackTitle answers what a playback's card says of the title played: its name, where it is in
// its show, and its best pictures. ErrNotFound for no such title.
func (s *Store) PlaybackTitle(ctx context.Context, id uuid.UUID) (domain.PlaybackTitle, error) {
	row, err := readItem(ctx, s.pool, id)
	if err != nil {
		return domain.PlaybackTitle{}, err
	}
	rows := []*model.Item{row}
	pictures, _, err := s.bestPictures(ctx, rows)
	if err != nil {
		return domain.PlaybackTitle{}, err
	}
	shows, err := s.showsOf(ctx, rows)
	if err != nil {
		return domain.PlaybackTitle{}, err
	}
	t := domain.PlaybackTitle{
		ID: id, Kind: row.Kind, Title: row.Title, Year: deref(row.Year), SeasonNumber: row.SeasonNumber,
		EpisodeNumber: row.EpisodeNumber, EpisodeEnd: row.EpisodeEnd, Poster: first(pictures[row.ID][domain.ArtworkPoster]),
		Thumb: first(pictures[row.ID][domain.ArtworkThumb]), Backdrop: first(pictures[row.ID][domain.ArtworkBackdrop]),
	}
	if show := shows[row.ID].ref; show != nil {
		t.ShowID, t.Show = show.ID, show.Title
	}
	return t, nil
}

// cards answers titles as cards for a profile, with their best pictures.
func (s *Store) cards(ctx context.Context, profile uuid.UUID, rows []*model.Item) ([]Card, error) {
	// Each lookup is asked for at once, on a connection of its own; the pictures wait on the shows
	// an episode wears its pictures from.
	var (
		shows    map[uuid.UUID]episodeShow
		pictures map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID
		hashes   map[uuid.UUID]string
		states   map[uuid.UUID]TitleState
		lengths  map[uuid.UUID]onDisk
		sets     map[uuid.UUID]boxSet
		ratings  map[uuid.UUID][]domain.Rating
		posters  []shownPoster
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		if shows, err = s.showsOf(gctx, rows); err != nil {
			return err
		}
		pictures, hashes, err = s.picturesWorn(gctx, rows, shows)
		return err
	})
	g.Go(func() (err error) {
		states, err = s.states(gctx, profile, rows)
		return err
	})
	g.Go(func() (err error) {
		lengths, err = s.durations(gctx, ids(rows))
		return err
	})
	g.Go(func() (err error) {
		sets, err = s.boxSets(gctx, rows)
		return err
	})
	g.Go(func() (err error) {
		ratings, err = s.ratings(gctx, rows)
		return err
	})
	g.Go(func() (err error) {
		posters, err = s.collectionPosters(gctx, profile, rows)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	wearPosters(pictures, hashes, posters)
	cards := make([]Card, len(rows))
	for n, r := range rows {
		cards[n] = Card{
			ID: r.ID, Kind: r.Kind, Title: r.Title, AddedAt: r.AddedAt, Year: deref(r.Year),
			ReleaseDate: deref(r.ReleaseDate), Poster: first(pictures[r.ID][domain.ArtworkPoster]),
			Backdrop: first(pictures[r.ID][domain.ArtworkBackdrop]), State: states[r.ID],
			DurationMS: lengths[r.ID].ms, VersionCount: lengths[r.ID].versions, Show: shows[r.ID].ref, Season: shows[r.ID].season, SeasonNumber: r.SeasonNumber,
			EpisodeNumber: r.EpisodeNumber, EpisodeEnd: r.EpisodeEnd, Thumb: first(pictures[r.ID][domain.ArtworkThumb]),
			SeasonPoster: seasonPoster(pictures, r, shows[r.ID]), ShowThumb: showThumb(pictures, r, shows[r.ID]),
			AirsBeforeSeason: r.AirsBeforeSeason, AirsBeforeEpisode: r.AirsBeforeEpisode, AirsAfterSeason: r.AirsAfterSeason,
			Origin: sets[r.ID].Origin, Overview: deref(r.Overview), Logo: first(pictures[r.ID][domain.ArtworkLogo]),
			Genres: r.Genres, Certificate: cmp.Or(deref(r.Certificate), shows[r.ID].certificate), Ratings: ratings[r.ID],
		}
		c := &cards[n]
		if set, ok := sets[r.ID]; ok {
			c.Year, c.ReleaseDate = deref(set.Year), deref(set.Released)
		}
		c.Blurhashes = blurhashesOf(hashes, c.Poster, c.Backdrop, c.Thumb, c.Logo, c.SeasonPoster, c.ShowThumb)
	}
	return cards, nil
}

type seasonShow struct {
	Season      uuid.UUID
	SeasonTitle string
	ID          uuid.UUID
	Title       string
	Certificate *string
}

// picturesWorn answers titles' pictures as bestPictures does, an episode wearing its show's of
// each kind it has none of (a poster, a backdrop, the lettering), as Plex answers an episode with its
// show's art: its own still stays its thumb. The shows, and the seasons an episode's card carries
// the poster of, are read in the same lookup.
func (s *Store) picturesWorn(ctx context.Context, rows []*model.Item, shows map[uuid.UUID]episodeShow) (map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, map[uuid.UUID]string, error) {
	// A show is ranked as a show of its episode's library; nothing more of it is read.
	all := slices.Clone(rows)
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		if show := shows[r.ID].ref; show != nil && !seen[show.ID] {
			seen[show.ID] = true
			all = append(all, &model.Item{ID: show.ID, LibraryID: r.LibraryID, Kind: domain.ItemShow})
		}
		if season := shows[r.ID].season; r.Kind == domain.ItemEpisode && season != nil && !seen[season.ID] {
			seen[season.ID] = true
			all = append(all, &model.Item{ID: season.ID, LibraryID: r.LibraryID, Kind: domain.ItemSeason})
		}
	}
	pictures, hashes, err := s.bestPictures(ctx, all)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		show := shows[r.ID].ref
		if r.Kind != domain.ItemEpisode || show == nil {
			continue
		}
		for _, kind := range []domain.ArtworkKind{domain.ArtworkPoster, domain.ArtworkBackdrop, domain.ArtworkLogo} {
			if len(pictures[r.ID][kind]) == 0 && len(pictures[show.ID][kind]) > 0 {
				if pictures[r.ID] == nil {
					pictures[r.ID] = map[domain.ArtworkKind][]uuid.UUID{}
				}
				pictures[r.ID][kind] = pictures[show.ID][kind]
			}
		}
	}
	return pictures, hashes, nil
}

// seasonPoster is the poster of the season an episode is of, where it has one of its own: what a
// search shows an episode by, as Infuse shows one by its season's art and not by its still.
func seasonPoster(pictures map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, r *model.Item, of episodeShow) uuid.UUID {
	if r.Kind != domain.ItemEpisode || of.season == nil {
		return uuid.UUID{}
	}
	return first(pictures[of.season.ID][domain.ArtworkPoster])
}

// showThumb is the thumb of the show an episode is of, where the show has one.
func showThumb(pictures map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, r *model.Item, of episodeShow) uuid.UUID {
	if r.Kind != domain.ItemEpisode || of.ref == nil {
		return uuid.UUID{}
	}
	return first(pictures[of.ref.ID][domain.ArtworkThumb])
}

// episodeShow is the show an episode is of, and the certificate it wears where it has none of its
// own: its season's, else its show's, as Plex rates an episode by its show.
type episodeShow struct {
	ref, season *TitleRef
	certificate string
}

// showsOf answers the show each episode or season among rows is of.
func (s *Store) showsOf(ctx context.Context, rows []*model.Item) (map[uuid.UUID]episodeShow, error) {
	out := map[uuid.UUID]episodeShow{}
	// An episode's show is its season's parent; a season's is its own.
	seasonOf := func(r *model.Item) *uuid.UUID {
		switch r.Kind {
		case domain.ItemEpisode:
			return r.ParentID
		case domain.ItemSeason:
			return &r.ID
		case domain.ItemMovie, domain.ItemShow, domain.ItemCollection, domain.ItemExtra:
		}
		return nil
	}
	var seasons []uuid.UUID
	for _, r := range rows {
		if season := seasonOf(r); season != nil {
			seasons = append(seasons, *season)
		}
	}
	if len(seasons) == 0 {
		return out, nil
	}
	pairs, err := queryStructs[seasonShow](ctx, s.pool, `
		SELECT season.id AS season, season.title AS season_title, show.id, show.title,
			coalesce(season.certificate, show.certificate) AS certificate FROM items season
		JOIN items show ON show.id = season.parent_id WHERE season.id = ANY($1)`, seasons)
	if err != nil {
		return nil, err
	}
	bySeason := map[uuid.UUID]episodeShow{}
	for _, p := range pairs {
		bySeason[p.Season] = episodeShow{&TitleRef{ID: p.ID, Title: p.Title}, &TitleRef{ID: p.Season, Title: p.SeasonTitle}, deref(p.Certificate)}
	}
	for _, r := range rows {
		season := seasonOf(r)
		if season == nil {
			continue
		}
		show := bySeason[*season]
		// A season names its show, not itself.
		if r.Kind == domain.ItemSeason {
			show.season = nil
		}
		out[r.ID] = show
	}
	return out, nil
}
