package playback

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A node's metrics count the playbacks it serves, by how each plays, and none another node serves:
// summed across the nodes, each is counted once.
func TestANodesMetricsCountThePlaybacksItServes(t *testing.T) {
	live := memory{}
	s := NewSessions(live, positions{}, served{}, func(context.Context, domain.Event) {}, thisNode)
	elsewhere := uuid.NewV7()
	for _, start := range []struct {
		method domain.PlayMethod
		node   uuid.UUID
	}{
		{domain.PlayTranscode, thisNode},
		{domain.PlayTranscode, thisNode},
		{domain.PlayDirect, thisNode},
		{domain.PlayTranscode, elsewhere},
		{domain.PlayRemux, elsewhere},
	} {
		if _, err := s.Start(t.Context(), uuid.NewV7(), start.method, card(uuid.NewV7(), uuid.NewV7()), start.node); err != nil {
			t.Fatal(err)
		}
	}
	want := `# HELP photon_playbacks The playbacks this node serves now, by how each plays.
# TYPE photon_playbacks gauge
photon_playbacks{method="direct"} 1
photon_playbacks{method="remux"} 0
photon_playbacks{method="transcode"} 2
`
	if err := testutil.CollectAndCompare(s, strings.NewReader(want), "photon_playbacks"); err != nil {
		t.Error(err)
	}
}
