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
	Statistics(ctx context.Context, since time.Time, zone *time.Location, profile uuid.UUID) (store.Statistics, error)
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
func (a *API) adminStatistics(w http.ResponseWriter, r *http.Request) {
	days, ok := a.queryNumber(w, r, "days", defaultStatisticsDays, 1, mostStatisticsDays)
	if !ok {
		return
	}
	profile, ok := a.queryID(w, r, "profile")
	if !ok {
		return
	}
	zone := time.UTC
	if name := r.URL.Query().Get("zone"); name != "" {
		where, err := time.LoadLocation(name)
		if err != nil {
			writeProblem(w, a.logger, codeInvalidParameter, "zone is a time zone's name, such as Europe/London")
			return
		}
		zone = where
	}
	// The span starts at the first moment of the earliest day it covers, so today and the days
	// before it are whole days wherever the report is read.
	midnight := time.Now().In(zone).AddDate(0, 0, -(days - 1))
	since := time.Date(midnight.Year(), midnight.Month(), midnight.Day(), 0, 0, 0, 0, zone)
	stats, err := a.svc.Statistics.Statistics(r.Context(), since, zone, profile)
	if err != nil {
		a.internal(w, r, err)
		return
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
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

func watchTotalOf(w store.Watched) watchTotalJSON {
	return watchTotalJSON{Plays: w.Plays, WatchedMS: w.WatchedMS}
}

func (a *API) statisticsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/statistics", access: admin,
			summary: "What was watched over the last so many days: altogether, by profile, by title, and day by day",
			query: []param{
				{"days", 0, "How many days back the report reaches, today counted; 30 by default, 3650 at most."},
				{"zone", "", "The time zone its days are counted in, such as Europe/London; UTC by default."},
				{"profile", uuid.UUID{}, "Only this profile's plays."},
			},
			status: http.StatusOK, reply: statisticsJSON{}, handle: a.adminStatistics,
		},
	}
}
