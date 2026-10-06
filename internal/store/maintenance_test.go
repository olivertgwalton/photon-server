//go:build integration

package store

import (
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A new server keeps Plex's window, 02:00 to 05:00, makes previews only in it and finds intros and
// credits as parts are added too; what an admin sets in their place is kept.
func TestTheMaintenanceWindowStartsAsPlexsAndKeepsWhatIsSet(t *testing.T) {
	s := migrated(t)
	got, err := s.Maintenance(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Maintenance{StartHour: 2, EndHour: 5, Zone: time.UTC, Previews: domain.TimingWindow, Markers: domain.TimingWindowAndAdded}
	if got.StartHour != want.StartHour || got.EndHour != want.EndHour || got.Zone.String() != "UTC" ||
		got.Previews != want.Previews || got.Markers != want.Markers {
		t.Errorf("a new server's window is %+v, want %+v", got, want)
	}
	london, err := domain.ParseZone("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	set := domain.Maintenance{StartHour: 23, EndHour: 6, Zone: london, Previews: domain.TimingWindowAndAdded, Markers: domain.TimingWindow}
	if err := s.SetMaintenance(t.Context(), set); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Maintenance(t.Context()); err != nil || got.StartHour != 23 || got.EndHour != 6 ||
		got.Zone.String() != "Europe/London" || got.Previews != set.Previews || got.Markers != set.Markers {
		t.Errorf("after setting %+v: %+v, %v", set, got, err)
	}
}
