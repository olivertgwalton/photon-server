//go:build integration

package scan

import (
	"context"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// raised is a hub keeping the events a scan job raises.
type raised []domain.Event

func (*raised) Scanning(context.Context) func(domain.ScanProgress) {
	return func(domain.ScanProgress) {}
}
func (*raised) Changed(context.Context, uuid.UUID, store.Changed) {}
func (*raised) Scanned(context.Context, uuid.UUID)                {}
func (r *raised) Raise(_ context.Context, e domain.Event)         { *r = append(*r, e) }

func TestAScanThatAddsTitlesTellsProfilesAndAsksForSubtitles(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	ctx := t.Context()
	if err := f.st.SetLibrary(ctx, f.lib.ID, store.LibraryChange{SubtitleLanguages: []language.Tag{language.English}}); err != nil {
		t.Fatal(err)
	}
	fan, err := f.st.AddProfile(ctx, "Fan", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	prefs := domain.DefaultPreferences()
	prefs.Notify = domain.NotifyAll
	if _, err := f.st.SetPreferences(ctx, fan.ID, prefs); err != nil {
		t.Fatal(err)
	}
	// A task that has run before is asked for again only by name.
	if err := f.st.TaskStarted(ctx, domain.TaskFetchSubtitles, time.Now()); err != nil {
		t.Fatal(err)
	}
	// A library's first titles fill it; only those added after tell anyone.
	f.put("Alien (1979)/Alien (1979).mkv", "alien")
	f.scan()
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	if err := f.st.ScanLibrary(ctx, f.lib.ID, 0); err != nil {
		t.Fatal(err)
	}
	var hub raised
	if err := f.scanner.Job(&hub)(ctx, f.lib.ID); err != nil {
		t.Fatal(err)
	}
	told := false
	for _, e := range hub {
		told = told || e.Kind == domain.EventNotificationsChanged && e.Profile == fan.ID
	}
	if !told {
		t.Errorf("raised %+v, want the profile told of its new notification", hub)
	}
	states, err := f.st.TaskStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states[domain.TaskFetchSubtitles].Requested.IsZero() {
		t.Error("missing subtitles were not asked for")
	}
}
