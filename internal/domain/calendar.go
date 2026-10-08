package domain

// Availability is whether a calendar's film or episode is here to play.
type Availability string

const (
	AvailabilityHere Availability = "available"
	// AvailabilityAnnounced is an episode a provider lists that has no file, aired or yet to air,
	// as Jellyfin's missing and unaired episodes.
	AvailabilityAnnounced Availability = "announced"
)

func Availabilities() []Availability {
	return []Availability{AvailabilityHere, AvailabilityAnnounced}
}

// Milestone is where in its show an episode stands out: the first of the show or of a season, or a
// season's last of those its provider lists, as Silo badges them.
type Milestone string

const (
	MilestoneSeriesPremiere Milestone = "series_premiere"
	MilestoneSeasonPremiere Milestone = "season_premiere"
	MilestoneSeasonFinale   Milestone = "season_finale"
)

func Milestones() []Milestone {
	return []Milestone{MilestoneSeriesPremiere, MilestoneSeasonPremiere, MilestoneSeasonFinale}
}

// CalendarFilter is which titles a calendar holds.
type CalendarFilter string

const (
	CalendarAll CalendarFilter = "all"
	// CalendarMine is the films and shows a profile has begun, put on its watchlist or made a
	// favourite, as Silo's following.
	CalendarMine       CalendarFilter = "mine"
	CalendarWatchlist  CalendarFilter = "watchlist"
	CalendarFavourites CalendarFilter = "favourites"
)

func CalendarFilters() []CalendarFilter {
	return []CalendarFilter{CalendarAll, CalendarMine, CalendarWatchlist, CalendarFavourites}
}
