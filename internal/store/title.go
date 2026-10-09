package store

import (
	"cmp"
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// TitlePage is everything a title's page shows, read in one go: a film's or episode's versions, a
// show's seasons, a season's episodes, and whatever extras and videos the title has.
type TitlePage struct {
	ID            uuid.UUID
	Library       uuid.UUID
	Kind          domain.ItemKind
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	Certificate   string
	Year          int
	ReleaseDate   domain.Date
	Genres        []string
	Studios       []string
	IDs           map[domain.Provider]string
	Ratings       []domain.Rating
	// Collections are the box sets it is in.
	Collections []CollectionCard
	// Credits are its cast and crew, as the highest-ranked source gives them.
	Credits []CreditRef
	// Origin is who made a collection: an admin's is changed by hand, a provider's only by it.
	Origin    domain.CollectionOrigin
	Placement domain.CollectionPlacement
	// Rule is what finds a smart collection's titles, List the list a list collection holds.
	Rule *SmartRule
	List *ListRef
	// EpisodeOrder is the order a show's episode files are numbered in.
	EpisodeOrder domain.EpisodeOrder
	// Locale is a film's or show's own metadata language and certification country, over its
	// library's; empty where it takes its library's.
	Locale        domain.Locale
	AddedAt       time.Time
	SeasonNumber  *int
	EpisodeNumber *int
	EpisodeEnd    *int
	Show          *TitleRef
	Season        *TitleRef
	Versions      []VersionPage
	Seasons       []SeasonCard
	Episodes      []EpisodeCard
	Extras        []ExtraCard
	Videos        []VideoLink
	// State is what the profile asking has made of it.
	State TitleState
	// Artwork is the title's pictures by kind, best first, by id: /api/v1/artwork/{id}.
	Artwork map[domain.ArtworkKind][]uuid.UUID
	// Blurhashes are those of its pictures that have one, by id.
	Blurhashes Blurhashes
	// Themes are the tunes to play under its page, in order, by id: /api/v1/themes/{id}. A season's
	// and an episode's are its show's.
	Themes []uuid.UUID
}

type TitleRef struct {
	ID    uuid.UUID
	Title string
}

// SignChapterImages signs the address of each chapter's picture, and of each extra's still, as
// sign signs a path.
func (p *TitlePage) SignChapterImages(sign func(path string) string) {
	for _, v := range p.Versions {
		for c := range v.Chapters {
			if ref := &v.Chapters[c]; ref.Image != "" {
				ref.Image = sign(ref.Image)
			}
		}
	}
	for e := range p.Extras {
		if ref := &p.Extras[e]; ref.Image != "" {
			ref.Image = sign(ref.Image)
		}
	}
}

type SeasonCard struct {
	ID       uuid.UUID
	Number   int
	Title    string
	Overview string
	Year     int
	Aired    domain.Date
	Episodes int
	Poster   uuid.UUID
	State    TitleState
	// Blurhashes are those of its pictures that have one, by id, as on every card.
	Blurhashes Blurhashes
}

type EpisodeCard struct {
	ID         uuid.UUID
	Number     *int
	End        *int
	Title      string
	Overview   string
	Aired      domain.Date
	DurationMS int64
	Thumb      uuid.UUID
	State      TitleState
	Blurhashes Blurhashes
}

type CollectionCard struct {
	ID         uuid.UUID
	Title      string
	Poster     uuid.UUID
	Blurhashes Blurhashes
}

// VideoLink is a provider's link to a video hosted elsewhere, with its site's still of it where
// the site publishes one, served at /api/v1/artwork/{thumb}.
type VideoLink struct {
	Kind      domain.ExtraKind
	Site      string
	Key       string
	Name      string
	Language  string
	Published *time.Time
	Thumb     uuid.UUID
}

// Title answers a title's page for a profile, or ErrNotFound.
func (s *Store) Title(ctx context.Context, profile, id uuid.UUID) (TitlePage, error) {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return TitlePage{}, err
	}
	if ok, err := s.visible(ctx, profile, id); err != nil || !ok {
		return TitlePage{}, cmp.Or(err, ErrNotFound)
	}
	p := TitlePage{
		ID: id, Library: item.LibraryID, Kind: item.Kind, Title: item.Title, OriginalTitle: deref(item.OriginalTitle),
		Overview: deref(item.Overview), Tagline: deref(item.Tagline), Certificate: deref(item.Certificate),
		Year: deref(item.Year), ReleaseDate: domain.Date(deref(item.ReleaseDate)), Genres: item.Genres, Studios: item.Studios,
		AddedAt: item.AddedAt, SeasonNumber: item.SeasonNumber, EpisodeNumber: item.EpisodeNumber,
		EpisodeEnd: item.EpisodeEnd,
	}
	if p.IDs, err = s.externalIDs(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if err := s.parents(ctx, item, &p); err != nil {
		return TitlePage{}, err
	}
	ratings, err := s.ratings(ctx, []*model.Item{item})
	if err != nil {
		return TitlePage{}, err
	}
	p.Ratings = ratings[item.ID]
	if p.Collections, err = s.collectionsOf(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Credits, err = s.credits(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Show != nil {
		show, err := s.credits(ctx, p.Show.ID)
		if err != nil {
			return TitlePage{}, err
		}
		p.Credits = billed(show, p.Credits)
	}
	if err := s.ofKind(ctx, profile, item, &p); err != nil {
		return TitlePage{}, err
	}
	if p.Extras, err = s.extras(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if p.Videos, err = s.videos(ctx, item.ID); err != nil {
		return TitlePage{}, err
	}
	if err := s.dress(ctx, item, &p); err != nil {
		return TitlePage{}, err
	}
	states, err := s.states(ctx, profile, []*model.Item{item})
	p.State = states[item.ID]
	return p, err
}

// ofKind fills in what only a title of item's kind has: a film's or show's locale, a show's
// seasons, a season's episodes, a film's or episode's copies, a collection's rule and list.
func (s *Store) ofKind(ctx context.Context, profile uuid.UUID, item *model.Item, p *TitlePage) error {
	if item.Kind == domain.ItemMovie || item.Kind == domain.ItemShow {
		err := s.pool.QueryRow(ctx, `
			SELECT coalesce(metadata_language, ''), coalesce(certification_country, '') FROM items WHERE id = $1`, item.ID).
			Scan(&p.Locale.Language, &p.Locale.Country)
		if err != nil {
			return err
		}
	}
	var err error
	switch item.Kind {
	case domain.ItemShow:
		p.EpisodeOrder = item.EpisodeOrder
		p.Seasons, err = s.seasons(ctx, profile, item.ID)
	case domain.ItemSeason:
		p.Episodes, err = s.episodes(ctx, profile, item.ID)
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		var versions map[uuid.UUID][]VersionPage
		versions, err = s.Versions(ctx, []uuid.UUID{item.ID})
		p.Versions = versions[item.ID]
	case domain.ItemCollection:
		var source, list *string
		var missing int
		err = s.pool.QueryRow(ctx, `SELECT origin, placement, rule, list_source, list_id, list_missing FROM collections WHERE item_id = $1`, item.ID).
			Scan(&p.Origin, &p.Placement, &p.Rule, &source, &list, &missing)
		if source != nil && list != nil {
			p.List = &ListRef{Source: domain.FieldSource(*source), ID: *list, Missing: missing}
		}
	}
	return err
}

// dress fills in the pictures item wears, their blurhashes, and its themes, which an episode or
// season takes from its show.
func (s *Store) dress(ctx context.Context, item *model.Item, p *TitlePage) error {
	shows, err := s.showsOf(ctx, []*model.Item{item})
	if err != nil {
		return err
	}
	pictures, hashes, err := s.picturesWorn(ctx, []*model.Item{item}, shows)
	if err != nil {
		return err
	}
	p.Artwork = pictures[item.ID]
	var shown []uuid.UUID
	for _, of := range p.Artwork {
		shown = append(shown, of...)
	}
	p.Blurhashes = blurhashesOf(hashes, shown...)
	owner := item.ID
	if p.Show != nil {
		owner = p.Show.ID
	}
	themes, err := s.themes(ctx, owner)
	if len(themes) > 0 {
		p.Themes = themes
	}
	return err
}

// readItem answers a title's row, or ErrNotFound.
func readItem(ctx context.Context, q db, id uuid.UUID) (*model.Item, error) {
	row, err := readRow[model.Item](ctx, q, `SELECT `+itemColumns+` FROM items WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// visible reports whether a profile may see a title: a library it has, and a certificate within its
// age; a title it may not is not there to it.
func (s *Store) visible(ctx context.Context, profile, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items i, viewer($2) v WHERE i.id = $1 AND sees(v, i))`,
		id, profile).Scan(&ok)
	return ok, err
}

// Visible answers those of titles a profile may see, in their order.
func (s *Store) Visible(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]uuid.UUID, error) {
	if len(titles) == 0 {
		return nil, nil
	}
	return queryColumn[uuid.UUID](ctx, s.pool, `
		SELECT t.id FROM unnest($1::uuid[]) WITH ORDINALITY AS t(id, n)
		JOIN items i ON i.id = t.id, viewer($2) v
		WHERE sees(v, i) ORDER BY t.n`, titles, profile)
}

// Cards answers those of titles a profile may see as cards, in their order, each once.
func (s *Store) Cards(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]Card, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items
		WHERE id = ANY($1) AND EXISTS (SELECT 1 FROM viewer($2) v WHERE sees(v, items))`, titles, profile)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]*model.Item{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	rows = rows[:0]
	for _, id := range titles {
		if r, ok := byID[id]; ok {
			rows = append(rows, r)
			delete(byID, id)
		}
	}
	return s.cards(ctx, profile, rows)
}

// SameTitles answers a title and those the same as it in other libraries that a profile may see.
func (s *Store) SameTitles(ctx context.Context, profile, title uuid.UUID) ([]uuid.UUID, error) {
	return queryColumn[uuid.UUID](ctx, s.pool, `
		SELECT t FROM same_title($1) t JOIN items i ON i.id = t, viewer($2) v
		WHERE sees(v, i) ORDER BY t`, title, profile)
}

// HasLibrary reports whether a library is there and a profile may see its titles at all: as
// sees() asks, it has every library where none are listed for it.
func (s *Store) HasLibrary(ctx context.Context, profile, lib uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM libraries WHERE id = $2)
			AND (NOT EXISTS (SELECT 1 FROM profile_libraries WHERE profile_id = $1)
				OR EXISTS (SELECT 1 FROM profile_libraries WHERE profile_id = $1 AND library_id = $2))`,
		profile, lib).Scan(&ok)
	return ok, err
}

func (s *Store) externalIDs(ctx context.Context, item uuid.UUID) (map[domain.Provider]string, error) {
	ids, err := s.ExternalIDs(ctx, []uuid.UUID{item})
	return ids[item], err
}

// ExternalIDs answers each title's ids at the providers that have it; a title with none is left out.
func (s *Store) ExternalIDs(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]map[domain.Provider]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT item_id, provider, value FROM external_ids WHERE item_id = ANY($1)`, items)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]map[domain.Provider]string{}
	var item uuid.UUID
	var provider domain.Provider
	var value string
	_, err = pgx.ForEachRow(rows, []any{&item, &provider, &value}, func() error {
		if out[item] == nil {
			out[item] = map[domain.Provider]string{}
		}
		out[item][provider] = value
		return nil
	})
	return out, err
}

// Seasons answers a show's seasons, ErrNotFound for a show the profile may not see.
func (s *Store) Seasons(ctx context.Context, profile, show uuid.UUID) ([]SeasonCard, error) {
	if ok, err := s.visible(ctx, profile, show); err != nil || !ok {
		return nil, cmp.Or(err, ErrNotFound)
	}
	return s.seasons(ctx, profile, show)
}

// Episodes answers the episodes of a show, every season's, or of one season, in order, as cards;
// ErrNotFound for one the profile may not see.
func (s *Store) Episodes(ctx context.Context, profile, of uuid.UUID) ([]Card, error) {
	if ok, err := s.visible(ctx, profile, of); err != nil || !ok {
		return nil, cmp.Or(err, ErrNotFound)
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumnsOf("e")+` FROM items e JOIN items season ON season.id = e.parent_id
		WHERE e.kind = 'episode' AND (season.id = $1 OR season.parent_id = $1)
		ORDER BY e.season_number, e.episode_number, e.air_date, e.sort_title`, of)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}

// parents names the show a season belongs to, and the season and show an episode does.
func (s *Store) parents(ctx context.Context, item *model.Item, p *TitlePage) error {
	parent := item.ParentID
	for parent != nil && (item.Kind == domain.ItemSeason || item.Kind == domain.ItemEpisode) {
		row, err := readItem(ctx, s.pool, *parent)
		if err != nil {
			return err
		}
		ref := &TitleRef{ID: row.ID, Title: row.Title}
		// A season or episode with no certificate of its own wears the nearest it is under.
		p.Certificate = cmp.Or(p.Certificate, deref(row.Certificate))
		switch row.Kind {
		case domain.ItemSeason:
			p.Season = ref
		case domain.ItemShow:
			p.Show = ref
		case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection:
		}
		parent = row.ParentID
	}
	return nil
}

func (s *Store) seasons(ctx context.Context, profile, show uuid.UUID) ([]SeasonCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'season' ORDER BY season_number`, show)
	if err != nil {
		return nil, err
	}
	episodes, err := queryMap[uuid.UUID, int](ctx, s.pool, `
		SELECT parent_id, count(*) FROM items WHERE parent_id = ANY($1) AND kind = 'episode' GROUP BY parent_id`,
		ids(rows))
	if err != nil {
		return nil, err
	}
	pictures, hashes, err := s.pictureOrder(ctx, rows)
	if err != nil {
		return nil, err
	}
	states, err := s.states(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	out := make([]SeasonCard, len(rows))
	for n, r := range rows {
		out[n] = SeasonCard{
			ID: r.ID, Number: deref(r.SeasonNumber), Title: r.Title, Overview: deref(r.Overview),
			Year: deref(r.Year), Aired: domain.Date(deref(r.ReleaseDate)), Episodes: episodes[r.ID],
			Poster: first(pictures[r.ID][domain.ArtworkPoster]), State: states[r.ID],
		}
		out[n].Blurhashes = blurhashesOf(hashes, out[n].Poster)
	}
	return out, nil
}

func (s *Store) episodes(ctx context.Context, profile, season uuid.UUID) ([]EpisodeCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'episode'
		ORDER BY episode_number, air_date, sort_title`, season)
	if err != nil {
		return nil, err
	}
	lengths, err := s.durations(ctx, ids(rows))
	if err != nil {
		return nil, err
	}
	pictures, hashes, err := s.pictureOrder(ctx, rows)
	if err != nil {
		return nil, err
	}
	states, err := s.states(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	out := make([]EpisodeCard, len(rows))
	for n, r := range rows {
		aired := r.ReleaseDate
		if aired == nil {
			aired = r.AirDate
		}
		out[n] = EpisodeCard{
			ID: r.ID, Number: r.EpisodeNumber, End: r.EpisodeEnd, Title: r.Title,
			Overview: deref(r.Overview), Aired: domain.Date(deref(aired)), DurationMS: lengths[r.ID].ms,
			Thumb: first(pictures[r.ID][domain.ArtworkThumb]), State: states[r.ID],
		}
		out[n].Blurhashes = blurhashesOf(hashes, out[n].Thumb)
	}
	return out, nil
}

// onDisk is what of a title is on disk: how long its longest copy runs, and how many copies.
type onDisk struct {
	ms       int64
	versions int
}

// durations answers what of each title is on disk.
func (s *Store) durations(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]onDisk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, max(duration_ms), count(*) FROM versions WHERE item_id = ANY($1) AND missing_since IS NULL
		GROUP BY item_id`, items)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]onDisk{}
	var item uuid.UUID
	var held onDisk
	_, err = pgx.ForEachRow(rows, []any{&item, &held.ms, &held.versions}, func() error {
		out[item] = held
		return nil
	})
	return out, err
}

func (s *Store) videos(ctx context.Context, item uuid.UUID) ([]VideoLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, site, key, name, coalesce(language, ''), published_at, thumb_id
		FROM remote_videos WHERE item_id = $1 ORDER BY source, position`, item)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (VideoLink, error) {
		var v VideoLink
		var thumb *uuid.UUID
		err := r.Scan(&v.Kind, &v.Site, &v.Key, &v.Name, &v.Language, &v.Published, &thumb)
		v.Thumb = deref(thumb)
		return v, err
	})
	if len(out) == 0 {
		out = nil
	}
	return out, err
}
