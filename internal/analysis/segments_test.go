//go:build integration

package analysis

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// timer is a plugin that has timed every episode's intro, and counts what it is asked.
type timer struct{ asked []domain.SegmentQuery }

func (t *timer) Info() provider.Info { return provider.Info{ID: "plugin:intros"} }
func (t *timer) Segments(_ context.Context, q domain.SegmentQuery) ([]domain.Marker, error) {
	t.asked = append(t.asked, q)
	return []domain.Marker{{Kind: domain.MarkerIntro, StartMS: 60_000, EndMS: 120_000}}, nil
}

// A plugin's timing of an episode is offered in a library on chapters, below the chapters', and
// each part is asked about once; the server's own reading of the season is still to do.
func TestAPluginTimesAnEpisodesIntro(t *testing.T) {
	st, lib := lost(t, domain.MarkersChapters)
	ctx := t.Context()
	if n, err := st.QueueSegments(ctx, domain.JobDueWindow); err != nil || n != 3 {
		t.Fatalf("queued %d (%v), want the three episodes", n, err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, seasonOf(t, st, lib))
	if err != nil || len(page.Episodes) != 3 {
		t.Fatal(page.Episodes, err)
	}
	first := page.Episodes[0].ID
	plugin := &timer{}
	segments := Segments(st, provider.NewRegistry(nil, plugin))
	if err := segments(ctx, first); err != nil {
		t.Fatal(err)
	}
	if len(plugin.asked) != 1 || plugin.asked[0].Season != 1 || plugin.asked[0].Episode != 1 || plugin.asked[0].Duration != 44*time.Minute {
		t.Fatalf("asked %+v, want episode 1 of season 1, 44 minutes long", plugin.asked)
	}
	ep, err := st.Title(ctx, uuid.UUID{}, first)
	if err != nil {
		t.Fatal(err)
	}
	intro := store.MarkerRef{Kind: domain.MarkerIntro, StartMS: 60_000, EndMS: 120_000, Source: domain.MarkerByProvider}
	if got := ep.Versions[0].Markers; len(got) != 1 || got[0] != intro {
		t.Errorf("markers = %+v, want the plugin's intro", got)
	}
	if err := segments(ctx, first); err != nil || len(plugin.asked) != 1 {
		t.Errorf("asked again: %d asks (%v), want the part asked about once", len(plugin.asked), err)
	}

	if err := st.SetLibrary(ctx, lib, store.LibraryChange{Markers: domain.MarkersAll}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.QueueSeasonMarkers(ctx, domain.JobDueWindow); err != nil || n != 1 {
		t.Errorf("set to compare sound: %d queued (%v), want the season still to compare", n, err)
	}
}
