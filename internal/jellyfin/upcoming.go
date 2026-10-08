package jellyfin

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// upcomingDays is how far ahead upcoming episodes are looked for. Jellyfin looks without end, but
// a show's episodes are known only about a season ahead.
const upcomingDays = 42

// upcoming answers the episodes airing from yesterday on, as Jellyfin's, by the day they air: those
// with no file as virtual episodes, which Jellyfin's apps show as unaired or missing and do not
// offer to play. A library's Upcoming tab names the library, as its parent; any other parent has
// none.
func (a *API) upcoming(w http.ResponseWriter, r *http.Request) {
	yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	q := store.CalendarQuery{
		Profile: auth.SessionOf(r.Context()).Profile.ID, Start: yesterday, End: yesterday.AddDate(0, 0, upcomingDays-1), Filter: domain.CalendarAll,
	}
	_, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if parent, err := uuid.Parse(query(r, "parentId")); err == nil {
		if _, ok := seen[parent]; !ok {
			a.writeJSON(w, queryResult{Items: []item{}})
			return
		}
		q.Library = parent
	}
	days, err := a.svc.Catalogue.Calendar(r.Context(), q)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	var cards []store.Card
	var announced []bool
	for _, d := range days {
		for _, e := range d.Entries {
			if e.Kind == domain.ItemEpisode {
				cards = append(cards, e.Card)
				announced = append(announced, e.Availability == domain.AvailabilityAnnounced)
			}
		}
	}
	l := listedOf(w, r)
	from := min(l.start, len(cards))
	to := from + min(l.limit, len(cards)-from)
	items, err := a.list(r.Context(), cards[from:to], l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	for n := range items {
		if announced[from+n] {
			items[n].virtual()
		}
	}
	a.writeJSON(w, queryResult{Items: items, TotalRecordCount: len(cards), StartIndex: from})
}

// announcedItem answers an episode announced with no file, as an app opens one from upcoming.
func (a *API) announcedItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	c, err := a.svc.Catalogue.AnnouncedEpisode(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	it := a.fromCard(c)
	it.virtual()
	a.writeJSON(w, it)
}

// virtual makes an episode one with no file, as Jellyfin's missing and unaired episodes are.
func (it *item) virtual() {
	it.LocationType, it.VideoType, it.MediaSources = "Virtual", "", nil
	it.Etag = etag(*it)
}
