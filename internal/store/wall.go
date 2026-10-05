package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"time"
	"uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	q, err := s.wallQuery(ctx, lib, p.Profile, p.Filter)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	dir := "ASC"
	if p.Order == domain.Descending {
		dir = "DESC"
	}
	var key clause.Expr
	switch p.Sort {
	case domain.SortAdded:
		key = clause.Expr{SQL: "items.added_at"}
	case domain.SortReleased:
		key = clause.Expr{SQL: "items.released_asc"}
		if dir == "DESC" {
			key = clause.Expr{SQL: "items.released_desc"}
		}
	case domain.SortRating:
		key = clause.Expr{SQL: "(SELECT max(r.score) FROM ratings r WHERE r.item_id = items.id AND r.site = ?)", Vars: []any{p.RatingSite}}
	case domain.SortRuntime:
		key = clause.Expr{SQL: "(SELECT max(v.duration_ms) FROM versions v WHERE v.item_id = items.id AND v.missing_since IS NULL)"}
	case domain.SortPlayed:
		key = clause.Expr{SQL: "(SELECT max(w.last_played_at) FROM watch_state w WHERE w.profile_id = ? AND w.item_id IN (" + episodesOf + "))", Vars: []any{p.Profile.String()}}
	case domain.SortTitle:
		key = clause.Expr{SQL: "items.sort_title"}
	}
	// What has no value to sort by comes last whichever way the rest run.
	key.SQL += " " + dir + " NULLS LAST, items.id " + dir
	var rows []*model.Item
	if err := q.Order(clause.OrderBy{Expression: key}).Offset(p.Offset).Limit(p.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, p.Profile, rows)
	return cards, total, err
}

// wallQuery is a library's films and shows a profile may see, as a filter narrows them; ErrNotFound
// for no such library.
func (s *Store) wallQuery(ctx context.Context, lib, profile uuid.UUID, f WallFilter) (*gorm.DB, error) {
	l := s.q.Library
	if _, err := l.WithContext(ctx).Where(l.ID.Eq(model.UUID(lib))).Take(); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	i := s.q.Item
	q := i.WithContext(ctx).Where(i.LibraryID.Eq(model.UUID(lib)), i.Kind.In(string(domain.ItemMovie), string(domain.ItemShow))).UnderlyingDB().
		Where("visible(items.id, ?)", profile.String())
	return f.apply(q, profile), nil
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
	q, err := s.wallQuery(ctx, lib, profile, f)
	if err != nil {
		return nil, err
	}
	var out []Letter
	err = q.Select(firstLetter + " AS letter, count(*) AS count").Group("letter").Order("letter").Scan(&out).Error
	// Titles before A sort first in the wall, whatever the collation makes of "#".
	if n := slices.IndexFunc(out, func(l Letter) bool { return l.Letter == "#" }); n > 0 {
		out = append([]Letter{out[n]}, slices.Delete(out, n, n+1)...)
	}
	return out, err
}

// cards answers titles as cards for a profile, with their best pictures.
func (s *Store) cards(ctx context.Context, profile uuid.UUID, rows []*model.Item) ([]Card, error) {
	pictures, err := s.pictureOrder(ctx, rows)
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
	shows, err := s.showsOf(ctx, rows)
	if err != nil {
		return nil, err
	}
	cards := make([]Card, len(rows))
	for n, r := range rows {
		cards[n] = Card{
			ID: uuid.UUID(r.ID), Kind: r.Kind, Title: r.Title, AddedAt: r.AddedAt, Year: deref(r.Year),
			ReleaseDate: deref(r.ReleaseDate), Poster: first(pictures[r.ID][domain.ArtworkPoster]),
			Backdrop: first(pictures[r.ID][domain.ArtworkBackdrop]), State: states[r.ID],
			DurationMS: lengths[r.ID], Show: shows[r.ID], SeasonNumber: r.SeasonNumber,
			EpisodeNumber: r.EpisodeNumber, EpisodeEnd: r.EpisodeEnd, Thumb: first(pictures[r.ID][domain.ArtworkThumb]),
		}
	}
	return cards, nil
}

// showsOf answers the show each episode among rows is of.
func (s *Store) showsOf(ctx context.Context, rows []*model.Item) (map[model.UUID]*TitleRef, error) {
	out := map[model.UUID]*TitleRef{}
	var seasons []driver.Valuer
	for _, r := range rows {
		if r.Kind == domain.ItemEpisode && r.ParentID != nil {
			seasons = append(seasons, *r.ParentID)
		}
	}
	if len(seasons) == 0 {
		return out, nil
	}
	i := s.q.Item
	var pairs []struct {
		Season model.UUID
		ID     model.UUID
		Title  string
	}
	// gen cannot alias a table joined to itself, so this one query is SQL.
	err := i.WithContext(ctx).UnderlyingDB().Raw(`
		SELECT season.id AS season, show.id, show.title FROM items season
		JOIN items show ON show.id = season.parent_id WHERE season.id IN ?`, seasons).Scan(&pairs).Error
	if err != nil {
		return nil, err
	}
	bySeason := map[model.UUID]*TitleRef{}
	for _, p := range pairs {
		bySeason[p.Season] = &TitleRef{ID: uuid.UUID(p.ID), Title: p.Title}
	}
	for _, r := range rows {
		if r.Kind == domain.ItemEpisode && r.ParentID != nil {
			out[r.ID] = bySeason[*r.ParentID]
		}
	}
	return out, nil
}
