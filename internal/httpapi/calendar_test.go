package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Calendar answers, on its first day, the premiere the profile has and the next episode,
// announced, titled after the filter asked for.
func (fakeCatalogue) Calendar(_ context.Context, q store.CalendarQuery) ([]store.CalendarDay, error) {
	wire := &store.TitleRef{ID: shows, Title: "The Wire"}
	one, two := 1, 2
	return []store.CalendarDay{{Date: q.Start, Entries: []store.CalendarEntry{
		{Availability: domain.AvailabilityHere, Milestones: []domain.Milestone{domain.MilestoneSeriesPremiere}, ID: films, Kind: domain.ItemEpisode, Title: "The Target", Show: wire, SeasonNumber: &one, EpisodeNumber: &one},
		{Availability: domain.AvailabilityAnnounced, ID: shows, Kind: domain.ItemEpisode, Title: string(q.Filter), Show: wire, SeasonNumber: &one, EpisodeNumber: &two},
	}}}, nil
}

func TestCalendar(t *testing.T) {
	for _, tc := range []struct {
		target   string
		want     int
		wantBody string
	}{
		{"/api/v1/calendar?start=2026-10-01&end=2026-11-11", http.StatusOK, `{"days":[{"date":"2026-10-01","entries":[` +
			`{"availability":"available","kind":"episode","id":"` + films.String() + `","title":"The Target","show":{"id":"` + shows.String() + `","title":"The Wire"},"season_number":1,"episode_number":1,"milestones":["series_premiere"]},` +
			`{"availability":"announced","kind":"episode","id":"` + shows.String() + `","title":"all","show":{"id":"` + shows.String() + `","title":"The Wire"},"season_number":1,"episode_number":2}]}]}`},
		{"/api/v1/calendar?start=2026-10-01&end=2026-10-01&filter=mine", http.StatusOK, `"title":"mine"`},
		{"/api/v1/calendar?start=2026-10-01&end=2026-10-01&filter=popular", http.StatusBadRequest, "is not one of [all mine watchlist favourites]"},
		{"/api/v1/calendar?start=2026-10-01&end=2026-11-12", http.StatusBadRequest, "end is from start to 41 days on"},
		{"/api/v1/calendar?start=2026-10-02&end=2026-10-01", http.StatusBadRequest, "end is from start"},
		{"/api/v1/calendar?start=2026-10-01", http.StatusBadRequest, "end is a day"},
		{"/api/v1/calendar?start=1%2F10%2F2026&end=2026-10-01", http.StatusBadRequest, "start is a day"},
	} {
		rec := serve(t, http.MethodGet, tc.target, goodToken, "")
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Errorf("%s: %d %s, want %d %s", tc.target, rec.Code, rec.Body, tc.want, tc.wantBody)
		}
	}
}
