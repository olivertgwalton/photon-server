package store

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// CalendarQuery asks for the days from Start to End, both included, of the titles Filter holds, of
// Library, or of every library where it is zero.
type CalendarQuery struct {
	Profile    uuid.UUID
	Start, End time.Time
	Filter     domain.CalendarFilter
	Library    uuid.UUID
}

// CalendarDay is a day's films and episodes, by show or film title, then where in the show.
type CalendarDay struct {
	Date    time.Time
	Entries []CalendarEntry
}

// CalendarEntry is a film or an episode out that day. An announced episode's card wears its show's
// pictures.
type CalendarEntry struct {
	Availability domain.Availability
	Milestones   []domain.Milestone
	Card
}

// calendarChosen are the films and shows a filter holds, of what the profile marked: a season or an
// episode stands for its show.
const calendarChosen = `
	WITH marked AS (
		SELECT item_id FROM watchlist WHERE profile_id = @profile AND @filter IN ('mine', 'watchlist')
		UNION ALL
		SELECT item_id FROM favourites WHERE profile_id = @profile AND @filter IN ('mine', 'favourites')
		UNION ALL
		SELECT item_id FROM watch_state WHERE profile_id = @profile AND @filter = 'mine'
	), chosen AS (
		SELECT DISTINCT CASE i.kind WHEN 'season' THEN i.parent_id WHEN 'episode' THEN s.parent_id ELSE i.id END AS id
		FROM marked m JOIN items i ON i.id = m.item_id LEFT JOIN items s ON s.id = i.parent_id
	)`

// seasonLastOf is the last episode known of a show's season, from what its provider lists and what
// is here.
func seasonLastOf(show, season string) string {
	return `(SELECT max(n) FROM (
		SELECT listed.episode_number AS n FROM announced_episodes listed WHERE listed.show_id = ` + show + ` AND listed.season_number = ` + season + `
		UNION ALL
		SELECT coalesce(here.episode_end, here.episode_number) FROM items filed JOIN items here ON here.parent_id = filed.id
		WHERE filed.parent_id = ` + show + ` AND filed.kind = 'season' AND filed.season_number = ` + season + `) known) AS season_last`
}

// calendarHere are the films released and the episodes aired between two days that the profile
// sees, each title in several libraries once. A film is read along its library's index of release
// dates, so each library is named.
var calendarHere = calendarChosen + `
	SELECT NULL::int AS season_last, ` + itemColumns + ` FROM items
	WHERE library_id IN (SELECT id FROM libraries WHERE @library::uuid IS NULL OR id = @library) AND kind = 'movie'
		AND released_asc BETWEEN @start::date AND @end::date AND release_date IS NOT NULL
		AND (@filter = 'all' OR id IN (SELECT id FROM chosen))
		AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, items) AND NOT EXISTS (SELECT 1 FROM seen_before(v, items)))
	UNION ALL
	SELECT ` + seasonLastOf("season.parent_id", "e.season_number") + `, ` + itemColumnsOf("e") + `
	FROM items e JOIN items season ON season.id = e.parent_id
	WHERE e.kind = 'episode' AND coalesce(e.release_date, e.air_date) BETWEEN @start::date AND @end::date
		AND (@library::uuid IS NULL OR e.library_id = @library)
		AND (@filter = 'all' OR season.parent_id IN (SELECT id FROM chosen))
		AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, e) AND NOT EXISTS (SELECT 1 FROM seen_before(v, e)))`

// announcedColumns are an announced episode's own, its season's last, and its show's.
var announcedColumns = `a.id AS announced_id, a.season_number AS announced_season, a.episode_number AS announced_episode,
	a.title AS announced_title, a.overview AS announced_overview, a.air_date AS announced_on,
	` + seasonLastOf("a.show_id", "a.season_number") + `, ` + itemColumnsOf("show")

// unfiled is an announced episode with no file.
const unfiled = `NOT EXISTS (SELECT 1 FROM items s JOIN items e ON e.parent_id = s.id
	WHERE s.parent_id = a.show_id AND s.kind = 'season' AND e.season_number = a.season_number
		AND a.episode_number BETWEEN e.episode_number AND coalesce(e.episode_end, e.episode_number))`

// calendarAnnounced are the episodes announced between two days that have no file, with their
// shows, of the shows the profile sees.
var calendarAnnounced = calendarChosen + `
	SELECT ` + announcedColumns + `
	FROM announced_episodes a JOIN items show ON show.id = a.show_id
	WHERE a.air_date BETWEEN @start::date AND @end::date AND ` + unfiled + `
		AND (@library::uuid IS NULL OR show.library_id = @library)
		AND (@filter = 'all' OR a.show_id IN (SELECT id FROM chosen))
		AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, show) AND NOT EXISTS (SELECT 1 FROM seen_before(v, show)))`

type hereRow struct {
	SeasonLast *int
	model.Item
}

type announcedRow struct {
	AnnouncedID       uuid.UUID
	AnnouncedSeason   int
	AnnouncedEpisode  int
	AnnouncedTitle    string
	AnnouncedOverview string
	AnnouncedOn       *time.Time
	SeasonLast        *int
	model.Item
}

// Calendar answers the days from start to end with a film released or an episode aired on them
// that the profile sees, the episodes announced without a file among them.
func (s *Store) Calendar(ctx context.Context, q CalendarQuery) ([]CalendarDay, error) {
	var library *uuid.UUID
	if q.Library != (uuid.UUID{}) {
		library = &q.Library
	}
	args := pgx.NamedArgs{"profile": q.Profile, "start": q.Start, "end": q.End, "filter": q.Filter, "library": library}
	here, err := queryRows[hereRow](ctx, s.pool, calendarHere, args)
	if err != nil {
		return nil, err
	}
	items := make([]*model.Item, len(here))
	for n, h := range here {
		items[n] = &h.Item
	}
	cards, err := s.cards(ctx, q.Profile, items)
	if err != nil {
		return nil, err
	}
	rows, err := queryRows[announcedRow](ctx, s.pool, calendarAnnounced, args)
	if err != nil {
		return nil, err
	}
	announced, err := s.announcedCards(ctx, q.Profile, rows)
	if err != nil {
		return nil, err
	}
	type dated struct {
		on time.Time
		CalendarEntry
	}
	entries := make([]dated, 0, len(here)+len(rows))
	for n, h := range here {
		on := deref(cmp.Or(h.ReleaseDate, h.AirDate))
		entries = append(entries, dated{on, CalendarEntry{domain.AvailabilityHere, milestones(h.SeasonNumber, h.EpisodeNumber, h.EpisodeEnd, h.SeasonLast), cards[n]}})
	}
	for n, a := range rows {
		entries = append(entries, dated{*a.AnnouncedOn, CalendarEntry{domain.AvailabilityAnnounced, milestones(&a.AnnouncedSeason, &a.AnnouncedEpisode, nil, a.SeasonLast), announced[n]}})
	}
	slices.SortFunc(entries, func(a, b dated) int {
		return cmp.Or(a.on.Compare(b.on), strings.Compare(a.heading(), b.heading()),
			cmp.Compare(deref(a.SeasonNumber), deref(b.SeasonNumber)), cmp.Compare(deref(a.EpisodeNumber), deref(b.EpisodeNumber)))
	})
	var days []CalendarDay
	for _, e := range entries {
		if len(days) == 0 || !days[len(days)-1].Date.Equal(e.on) {
			days = append(days, CalendarDay{Date: e.on})
		}
		days[len(days)-1].Entries = append(days[len(days)-1].Entries, e.CalendarEntry)
	}
	return days, nil
}

// announcedCards are announced episodes as cards, each wearing its show's pictures.
func (s *Store) announcedCards(ctx context.Context, profile uuid.UUID, rows []*announcedRow) ([]Card, error) {
	shows := make([]*model.Item, len(rows))
	for n, a := range rows {
		shows[n] = &a.Item
	}
	showCards, err := s.cards(ctx, profile, shows)
	if err != nil {
		return nil, err
	}
	out := make([]Card, len(rows))
	for n, a := range rows {
		show := showCards[n]
		out[n] = Card{
			ID: a.AnnouncedID, Kind: domain.ItemEpisode, Title: a.AnnouncedTitle, Overview: a.AnnouncedOverview,
			ReleaseDate: deref(a.AnnouncedOn), Show: &TitleRef{ID: show.ID, Title: show.Title},
			SeasonNumber: &a.AnnouncedSeason, EpisodeNumber: &a.AnnouncedEpisode,
			Poster: show.Poster, Backdrop: show.Backdrop, Logo: show.Logo, Blurhashes: show.Blurhashes,
		}
	}
	return out, nil
}

// milestones are where an episode, to end where it has an end, stands in its show: a special
// stands nowhere.
func milestones(season, episode, end, last *int) []domain.Milestone {
	if season == nil || episode == nil || *season == 0 {
		return nil
	}
	var out []domain.Milestone
	switch {
	case *episode == 1 && *season == 1:
		out = append(out, domain.MilestoneSeriesPremiere)
	case *episode == 1:
		out = append(out, domain.MilestoneSeasonPremiere)
	}
	if last != nil && cmp.Or(deref(end), *episode) >= *last {
		out = append(out, domain.MilestoneSeasonFinale)
	}
	return out
}

// heading is what an entry is listed under: its show's title, or a film's own.
func (e CalendarEntry) heading() string {
	if e.Show != nil {
		return e.Show.Title
	}
	return e.Title
}
