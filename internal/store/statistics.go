package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Watched is how much was watched: how many plays, and how long they ran altogether. A play runs
// from when it began to when it stopped, which is what the server saw of it; a play left paused
// counts the time it was paused for.
type Watched struct {
	Plays     int
	WatchedMS int64
}

// ProfileWatched is how much one profile watched, by the name it had when the report was read.
type ProfileWatched struct {
	Profile uuid.UUID
	Name    string
	Watched
}

// TitleWatched is how much one title was watched, a show standing for every episode of it, as
// Tautulli's most watched counts a show.
type TitleWatched struct {
	Card Card
	Watched
}

// DayWatched is how much was watched on one day, in the zone the report was asked in. A day with
// nothing watched is there with nothing counted, so a chart has no gaps.
type DayWatched struct {
	Day domain.Date
	Watched
}

// Statistics is what was watched over a span: altogether, by each profile who watched, by the
// titles watched most, and day by day.
type Statistics struct {
	Watched
	Profiles []ProfileWatched
	Titles   []TitleWatched
	Days     []DayWatched
}

// mostWatched is how many titles a report names: the few worth naming, not a second wall.
const mostWatched = 10

// playedMS is how long the plays a statement counts ran, in milliseconds.
const playedMS = `coalesce(sum(extract(epoch FROM (p.stopped_at - p.started_at)) * 1000), 0)::bigint`

// Statistics answers what was watched from the day since falls on to today, in zone, by everyone
// or by one profile. Days are counted in zone, so a play late on Sunday night is Sunday's.
func (s *Store) Statistics(ctx context.Context, since time.Time, zone *time.Location, profile uuid.UUID) (Statistics, error) {
	args := pgx.NamedArgs{
		"since":   since,
		"zone":    zone.String(),
		"profile": optional(profile),
		"from":    since.In(zone).Format(time.DateOnly),
		"to":      time.Now().In(zone).Format(time.DateOnly),
	}
	// The plays counted: those of the span, and of one profile where a report names one.
	const whose = ` WHERE p.stopped_at >= @since AND (@profile::uuid IS NULL OR p.profile_id = @profile)`

	days, err := queryStructs[dayRow](ctx, s.pool, `
		WITH watched AS (
			SELECT (p.stopped_at AT TIME ZONE @zone)::date AS day, count(*)::int AS plays, `+playedMS+` AS watched_ms
			FROM plays p`+whose+`
			GROUP BY 1
		)
		SELECT d::date AS day, coalesce(w.plays, 0) AS plays, coalesce(w.watched_ms, 0) AS watched_ms
		FROM generate_series(@from::date, @to::date, interval '1 day') d
		LEFT JOIN watched w ON w.day = d::date
		ORDER BY d`, args)
	if err != nil {
		return Statistics{}, err
	}
	out := Statistics{Days: make([]DayWatched, len(days))}
	for n, d := range days {
		out.Days[n] = DayWatched{Day: domain.Date(d.Day), Plays: d.Plays, WatchedMS: d.WatchedMS}
		out.Plays += d.Plays
		out.WatchedMS += d.WatchedMS
	}

	profiles, err := queryStructs[profileRow](ctx, s.pool, `
		SELECT p.profile_id, who.name, count(*)::int AS plays, `+playedMS+` AS watched_ms
		FROM plays p JOIN profiles who ON who.id = p.profile_id`+whose+`
		GROUP BY p.profile_id, who.name ORDER BY watched_ms DESC, who.name`, args)
	if err != nil {
		return Statistics{}, err
	}
	out.Profiles = make([]ProfileWatched, len(profiles))
	for n, r := range profiles {
		out.Profiles[n] = ProfileWatched{
			Profile: r.ProfileID, Name: r.Name,
			Plays: r.Plays, WatchedMS: r.WatchedMS,
		}
	}

	// An episode counts towards its show, which is the title an admin reads a report by; a film,
	// and anything else played, counts as itself.
	args["most"] = mostWatched
	titles, err := queryStructs[titleRow](ctx, s.pool, `
		SELECT coalesce(season.parent_id, p.item_id) AS item_id, count(*)::int AS plays, `+playedMS+` AS watched_ms
		FROM plays p
		LEFT JOIN items played ON played.id = p.item_id AND played.kind = 'episode'
		LEFT JOIN items season ON season.id = played.parent_id`+whose+`
		GROUP BY 1 ORDER BY watched_ms DESC, item_id LIMIT @most`, args)
	if err != nil {
		return Statistics{}, err
	}
	if len(titles) == 0 {
		return out, nil
	}
	in := make([]uuid.UUID, len(titles))
	for n, r := range titles {
		in[n] = r.ItemID
	}
	cards, err := s.Cards(ctx, profile, in)
	if err != nil {
		return Statistics{}, err
	}
	byID := map[uuid.UUID]Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	// A title removed since it was played is left out rather than named as a blank.
	for _, r := range titles {
		if card, ok := byID[r.ItemID]; ok {
			out.Titles = append(out.Titles, TitleWatched{
				Card: card, Plays: r.Plays, WatchedMS: r.WatchedMS,
			})
		}
	}
	return out, nil
}

type dayRow struct {
	Day       time.Time
	Plays     int
	WatchedMS int64
}

type profileRow struct {
	ProfileID uuid.UUID
	Name      string
	Plays     int
	WatchedMS int64
}

type titleRow struct {
	ItemID    uuid.UUID
	Plays     int
	WatchedMS int64
}
