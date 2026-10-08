package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// maxCalendarDays is the most days a calendar is asked for at once: six weeks, as a month's grid
// of whole weeks shows at most.
const maxCalendarDays = 42

type calendarJSON struct {
	Days []calendarDayJSON `json:"days"`
}

type calendarDayJSON struct {
	Date    domain.Date         `json:"date"`
	Entries []calendarEntryJSON `json:"entries"`
}

type calendarEntryJSON struct {
	Availability domain.Availability `json:"availability"`
	Kind         domain.ItemKind     `json:"kind"`
	// ID is the film's or episode's: an announced episode's leads to nothing here but itself, as a
	// Jellyfin app's missing episode.
	ID            uuid.UUID          `json:"id"`
	Title         string             `json:"title"`
	Show          *titleRefJSON      `json:"show,omitzero"`
	SeasonNumber  *int               `json:"season_number,omitzero"`
	EpisodeNumber *int               `json:"episode_number,omitzero"`
	EpisodeEnd    *int               `json:"episode_end,omitzero"`
	Milestones    []domain.Milestone `json:"milestones,omitzero"`
	// Poster is a film's, or an episode's show's.
	Poster     uuid.UUID        `json:"poster,omitzero"`
	WatchedAt  *time.Time       `json:"watched_at,omitzero"`
	Blurhashes store.Blurhashes `json:"blurhashes,omitzero"`
}

// calendar answers the days from start to end with a film released or an episode aired on them, of
// the titles the filter holds.
func (a *API) calendar(w http.ResponseWriter, r *http.Request) {
	var days [2]time.Time
	for n, name := range []string{"start", "end"} {
		d, err := time.Parse(time.DateOnly, r.URL.Query().Get(name))
		if err != nil {
			writeProblem(w, a.logger, codeInvalidParameter, name+" is a day, as 2006-01-02")
			return
		}
		days[n] = d
	}
	filter, ok := queryEnum(a, w, r, "filter", domain.CalendarAll, domain.CalendarFilters())
	if !ok {
		return
	}
	start, end := days[0], days[1]
	if end.Before(start) || end.Sub(start) >= maxCalendarDays*24*time.Hour {
		writeProblem(w, a.logger, codeInvalidParameter, fmt.Sprintf("end is from start to %d days on", maxCalendarDays-1))
		return
	}
	found, err := a.svc.Catalogue.Calendar(r.Context(), store.CalendarQuery{Profile: auth.SessionOf(r.Context()).Profile.ID, Start: start, End: end, Filter: filter})
	if a.answered(w, r, err) {
		return
	}
	out := calendarJSON{Days: make([]calendarDayJSON, len(found))}
	for n, d := range found {
		out.Days[n] = calendarDayJSON{Date: domain.Date(d.Date), Entries: make([]calendarEntryJSON, len(d.Entries))}
		for m, e := range d.Entries {
			out.Days[n].Entries[m] = calendarEntryJSON{
				Availability: e.Availability, Kind: e.Kind, ID: e.ID, Title: e.Title, Show: (*titleRefJSON)(e.Show),
				SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, EpisodeEnd: e.EpisodeEnd, Milestones: e.Milestones,
				Poster: e.Poster, WatchedAt: e.State.WatchedAt, Blurhashes: e.Blurhashes,
			}
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

func (a *API) calendarRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/calendar", access: signedIn,
			summary: "The days between two with a film released or an episode aired, here or announced, that the profile sees",
			query: []param{
				{"start", domain.Date{}, "The first day; required."},
				{"end", domain.Date{}, "The last day, from start to " + strconv.Itoa(maxCalendarDays-1) + " days on; required."},
				{"filter", domain.CalendarFilter(""), "Whose titles: everything the profile sees by default, or its own, begun, on its watchlist or favourites."},
			},
			status: http.StatusOK, reply: calendarJSON{}, handle: a.calendar,
		},
	}
}
