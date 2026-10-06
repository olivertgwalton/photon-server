package store

import (
	"cmp"
	"context"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

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
	// An episode's card says which show it is of, where in it, and carries its still.
	Show          *TitleRef
	SeasonNumber  *int
	EpisodeNumber *int
	EpisodeEnd    *int
	Thumb         uuid.UUID
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
	Sort    domain.WallSort
	Order   domain.Order
	// RatingSite is whose rating SortRating sorts by.
	RatingSite domain.RatingSite
	Filter     WallFilter
	Offset     int
	Limit      int
}

// Wall answers a page of a library's films or shows and how many there are in all. Ties in the
// sort are broken by id, so a page is the same whenever it is asked for while the library is.
func (s *Store) Wall(ctx context.Context, lib uuid.UUID, p WallPage) ([]Card, int64, error) {
	titles, args, err := s.wallQuery(ctx, lib, p.Profile, p.Filter)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+titles, args).Scan(&total); err != nil {
		return nil, 0, err
	}
	dir := "ASC"
	if p.Order == domain.Descending {
		dir = "DESC"
	}
	// What has no value to sort by comes last whichever way the rest run. The columns are never
	// null, and saying so of them would stop their indexes reading a page in descending order.
	var key, nulls string
	switch p.Sort {
	case domain.SortAdded:
		key = "items.added_at"
	case domain.SortReleased:
		key = "items.released_asc"
		if dir == "DESC" {
			key = "items.released_desc"
		}
	case domain.SortRating:
		key, nulls = "(SELECT max(r.score) FROM ratings r WHERE r.item_id = items.id AND r.site = @sort_site)", " NULLS LAST"
		args["sort_site"] = p.RatingSite
	case domain.SortRuntime:
		key, nulls = "(SELECT max(v.duration_ms) FROM versions v WHERE v.item_id = items.id AND v.missing_since IS NULL)", " NULLS LAST"
	case domain.SortPlayed:
		key, nulls = "(SELECT max(w.last_played_at) FROM watch_state w WHERE w.profile_id = @profile AND w.item_id IN ("+episodesOf+"))", " NULLS LAST"
	case domain.SortTitle:
		key = "items.sort_title"
	}
	args["offset"], args["limit"] = p.Offset, p.Limit
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` `+titles+`
		ORDER BY `+key+` `+dir+nulls+`, items.id `+dir+` OFFSET @offset LIMIT @limit`, args)
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, p.Profile, rows)
	return cards, total, err
}

// wallQuery is the FROM and WHERE of a library's films or shows a profile may see, as a filter
// narrows them, and the values they take; ErrNotFound for no such library. A library holds one kind
// of title, and naming it lets the wall's sort indexes read a page in order.
func (s *Store) wallQuery(ctx context.Context, lib, profile uuid.UUID, f WallFilter) (string, pgx.NamedArgs, error) {
	var kind domain.LibraryKind
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM libraries WHERE id = $1`, lib).Scan(&kind); err != nil {
		return "", nil, found(err)
	}
	args := pgx.NamedArgs{"lib": lib, "profile": profile, "kind": kind.ItemKinds()[0]}
	return `FROM items WHERE library_id = @lib AND kind = @kind
		AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, items))` + f.where(args), args, nil
}

// Letter is how many of a library's titles sort under a letter: "#" for those before A.
type Letter struct {
	Letter string
	Count  int
}

// Letters counts a library's titles, as a filter narrows them, by the first letter they sort by,
// in title order, as Plex's firstCharacter does, so a client can jump to a letter by its offset.
// Letters are read unaccented, so "Émile" counts under E where the wall sorts it.
func (s *Store) Letters(ctx context.Context, lib, profile uuid.UUID, f WallFilter) ([]Letter, error) {
	titles, args, err := s.wallQuery(ctx, lib, profile, f)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+firstLetter+` AS letter, count(*) AS count `+titles+`
		GROUP BY letter ORDER BY letter`, args)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[Letter])
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
	pictures, _, err := s.pictureOrder(ctx, rows)
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
	shows, err := s.showsOf(ctx, rows)
	if err != nil {
		return nil, err
	}
	pictures, hashes, err := s.picturesWorn(ctx, rows, shows)
	if err != nil {
		return nil, err
	}
	states, err := s.states(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	lengths, err := s.durations(ctx, ids(rows))
	if err != nil {
		return nil, err
	}
	origins, err := s.origins(ctx, rows)
	if err != nil {
		return nil, err
	}
	ratings, err := s.ratings(ctx, rows)
	if err != nil {
		return nil, err
	}
	cards := make([]Card, len(rows))
	for n, r := range rows {
		cards[n] = Card{
			ID: r.ID, Kind: r.Kind, Title: r.Title, AddedAt: r.AddedAt, Year: deref(r.Year),
			ReleaseDate: deref(r.ReleaseDate), Poster: first(pictures[r.ID][domain.ArtworkPoster]),
			Backdrop: first(pictures[r.ID][domain.ArtworkBackdrop]), State: states[r.ID],
			DurationMS: lengths[r.ID], Show: shows[r.ID].ref, SeasonNumber: r.SeasonNumber,
			EpisodeNumber: r.EpisodeNumber, EpisodeEnd: r.EpisodeEnd, Thumb: first(pictures[r.ID][domain.ArtworkThumb]),
			Origin: origins[r.ID], Overview: deref(r.Overview), Logo: first(pictures[r.ID][domain.ArtworkLogo]),
			Genres: r.Genres, Certificate: cmp.Or(deref(r.Certificate), shows[r.ID].certificate), Ratings: ratings[r.ID],
		}
		c := &cards[n]
		c.Blurhashes = blurhashesOf(hashes, c.Poster, c.Backdrop, c.Thumb, c.Logo)
	}
	return cards, nil
}

type seasonShow struct {
	Season      uuid.UUID
	ID          uuid.UUID
	Title       string
	Certificate *string
}

// picturesWorn answers titles' pictures as pictureOrder does, an episode wearing its show's of
// each kind it has none of (a poster, a backdrop, the lettering), as Plex answers an episode with its
// show's art: its own still stays its thumb. The shows are read in the same lookup.
func (s *Store) picturesWorn(ctx context.Context, rows []*model.Item, shows map[uuid.UUID]episodeShow) (map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, map[uuid.UUID]string, error) {
	var showIDs []uuid.UUID
	for _, show := range shows {
		if show.ref != nil {
			showIDs = append(showIDs, show.ref.ID)
		}
	}
	all := rows
	if len(showIDs) > 0 {
		showRows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id = ANY($1)`, showIDs)
		if err != nil {
			return nil, nil, err
		}
		all = append(slices.Clone(rows), showRows...)
	}
	pictures, hashes, err := s.pictureOrder(ctx, all)
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

// episodeShow is the show an episode is of, and the certificate it wears where it has none of its
// own: its season's, else its show's, as Plex rates an episode by its show.
type episodeShow struct {
	ref         *TitleRef
	certificate string
}

// showsOf answers the show each episode among rows is of.
func (s *Store) showsOf(ctx context.Context, rows []*model.Item) (map[uuid.UUID]episodeShow, error) {
	out := map[uuid.UUID]episodeShow{}
	var seasons []uuid.UUID
	for _, r := range rows {
		if r.Kind == domain.ItemEpisode && r.ParentID != nil {
			seasons = append(seasons, *r.ParentID)
		}
	}
	if len(seasons) == 0 {
		return out, nil
	}
	found, err := s.pool.Query(ctx, `
		SELECT season.id AS season, show.id, show.title,
			coalesce(season.certificate, show.certificate) AS certificate FROM items season
		JOIN items show ON show.id = season.parent_id WHERE season.id = ANY($1)`, seasons)
	if err != nil {
		return nil, err
	}
	pairs, err := pgx.CollectRows(found, pgx.RowToStructByName[seasonShow])
	if err != nil {
		return nil, err
	}
	bySeason := map[uuid.UUID]episodeShow{}
	for _, p := range pairs {
		bySeason[p.Season] = episodeShow{&TitleRef{ID: p.ID, Title: p.Title}, deref(p.Certificate)}
	}
	for _, r := range rows {
		if r.Kind == domain.ItemEpisode && r.ParentID != nil {
			out[r.ID] = bySeason[*r.ParentID]
		}
	}
	return out, nil
}
