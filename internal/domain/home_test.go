package domain

import (
	"slices"
	"testing"
)

func TestAHomeIsEveryRowInTheProfilesOrder(t *testing.T) {
	got := ArrangeHome([]HomeSection{
		{RowFavourites, RowHidden}, {RowNextUp, RowShown}, {RowFavourites, RowShown},
	})
	want := []HomeSection{
		{RowFavourites, RowHidden},
		{RowNextUp, RowShown},
		{RowContinueWatching, RowShown},
		{RowWatchlist, RowShown},
		{RowRecentFilms, RowShown},
		{RowRecentShows, RowShown},
		{RowRecentlyReleased, RowShown},
		{RowTopRatedUnwatched, RowShown},
		{RowCollection, RowShown},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ArrangeHome = %v, want %v", got, want)
	}
}
