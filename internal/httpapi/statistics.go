package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type statistics interface {
	Statistics(ctx context.Context, days int, zone *time.Location, profile uuid.UUID) (store.Statistics, error)
}

// defaultStatisticsDays is the span a report covers where none is asked for, and mostStatisticsDays
// the longest it may cover: ten years, beyond which a day-by-day chart says nothing a client could
// draw.
const (
	defaultStatisticsDays = 30
	mostStatisticsDays    = 3650
)

type watchTotalJSON struct {
	Plays int `json:"plays"`
	// WatchedMS is how long the plays ran altogether, each from when it began to when it stopped.
	WatchedMS int64 `json:"watched_ms"`
}

type profileWatchedJSON struct {
	ProfileID uuid.UUID `json:"profile_id"`
	Name      string    `json:"name"`
	watchTotalJSON
}

type titleWatchedJSON struct {
	Title cardJSON `json:"title"`
	watchTotalJSON
}

type dayWatchedJSON struct {
	Day domain.Date `json:"day"`
	watchTotalJSON
}

// statisticsJSON is what was watched over a span, as the admin's report draws it.
type statisticsJSON struct {
	Days int    `json:"days"`
	Zone string `json:"zone"`
	watchTotalJSON
	Profiles []profileWatchedJSON `json:"profiles"`
	Titles   []titleWatchedJSON   `json:"titles"`
	Daily    []dayWatchedJSON     `json:"daily"`
}

// adminStatistics answers what was watched over the last so many days, by everyone or by one
// profile, as Tautulli's and Emby's reports do.
func (a *API) adminStatistics(r *http.Request, _ none) (statisticsJSON, error) {
	days, err := queryNumber(r, "days", defaultStatisticsDays, 1, mostStatisticsDays)
	if err != nil {
		return statisticsJSON{}, err
	}
	profile, err := queryID(r, "profile")
	if err != nil {
		return statisticsJSON{}, err
	}
	zone := time.UTC
	if name := r.URL.Query().Get("zone"); name != "" {
		where, err := time.LoadLocation(name)
		// "Local" is this server's own zone to Go, and no zone's name to the database.
		if err != nil || name == "Local" {
			return statisticsJSON{}, refuse(codeInvalidParameter, "zone is a time zone's name, such as Europe/London")
		}
		zone = where
	}
	stats, err := a.svc.Statistics.Statistics(r.Context(), days, zone, profile)
	if err != nil {
		return statisticsJSON{}, err
	}
	out := statisticsJSON{
		Days: days, Zone: zone.String(), watchTotalJSON: watchTotalOf(stats.Watched),
		Profiles: make([]profileWatchedJSON, len(stats.Profiles)),
		Titles:   make([]titleWatchedJSON, len(stats.Titles)),
		Daily:    make([]dayWatchedJSON, len(stats.Days)),
	}
	for i, p := range stats.Profiles {
		out.Profiles[i] = profileWatchedJSON{ProfileID: p.Profile, Name: p.Name, watchTotalJSON: watchTotalOf(p.Watched)}
	}
	for i, t := range stats.Titles {
		out.Titles[i] = titleWatchedJSON{Title: cardOf(t.Card), watchTotalJSON: watchTotalOf(t.Watched)}
	}
	for i, d := range stats.Days {
		out.Daily[i] = dayWatchedJSON{Day: d.Day, watchTotalJSON: watchTotalOf(d.Watched)}
	}
	return out, nil
}

func watchTotalOf(w store.Watched) watchTotalJSON {
	return watchTotalJSON{Plays: w.Plays, WatchedMS: w.WatchedMS}
}

func (a *API) statisticsRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/statistics", access: admin,
			summary: "What was watched over the last so many days: altogether, by profile, by title, and day by day",
			query: []param{
				{"days", 0, "How many days back the report reaches, today counted; 30 by default, 3650 at most."},
				{"zone", "", "The time zone its days are counted in, such as Europe/London; UTC by default."},
				{"profile", uuid.UUID{}, "Only this profile's plays."},
			},
			status: http.StatusOK,
		}, a.adminStatistics),
	}
}
