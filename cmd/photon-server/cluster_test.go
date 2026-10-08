package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

type leading bool

func (l leading) Leads() bool { return bool(l) }

func (leading) Statuses(context.Context) ([]task.Status, error) {
	return []task.Status{
		{Key: domain.TaskScanLibraries, State: domain.TaskState{Finished: time.Unix(1_700_000_000, 0), Result: domain.TaskSucceeded}},
		{Key: domain.TaskBackupDatabase},
	}, nil
}

type shared struct{}

func (shared) JobLoads(context.Context) ([]store.JobLoad, error) {
	return []store.JobLoad{
		{Kind: domain.JobIdentify, State: domain.JobQueued, Count: 40, Due: time.Now().Add(-time.Hour)},
		{Kind: domain.JobIdentify, State: domain.JobRunning, Count: 16},
	}, nil
}

func (shared) ItemCounts(context.Context, []domain.ItemKind) (map[domain.ItemKind]int, error) {
	return map[domain.ItemKind]int{domain.ItemMovie: 812}, nil
}

func (shared) Nodes(context.Context) ([]domain.Node, error) {
	return []domain.Node{{Availability: domain.NodeActive}, {Availability: domain.NodeActive}, {Availability: domain.NodeDraining}}, nil
}

// What the nodes share is said by the node holding the scheduler lease alone, so that summed or
// taken across the nodes each is counted once.
func TestTheClustersMetricsAreSaidByTheLeaderAlone(t *testing.T) {
	if n := testutil.CollectAndCount(cluster{lead: leading(false), st: shared{}, nodes: shared{}}); n != 0 {
		t.Errorf("a node without the lease said %d metrics, want none", n)
	}
	leader := cluster{lead: leading(true), st: shared{}, nodes: shared{}}
	want := `# HELP photon_jobs The cluster's jobs, by kind and state.
# TYPE photon_jobs gauge
photon_jobs{kind="identify",state="queued"} 40
photon_jobs{kind="identify",state="running"} 16
# HELP photon_library_items The films and episodes in the libraries.
# TYPE photon_library_items gauge
photon_library_items{kind="episode"} 0
photon_library_items{kind="movie"} 812
# HELP photon_nodes The nodes telling the others of themselves, by whether each takes new work.
# TYPE photon_nodes gauge
photon_nodes{state="active"} 2
photon_nodes{state="draining"} 1
# HELP photon_task_last_finished_timestamp_seconds When each scheduled task's last run ended, and how.
# TYPE photon_task_last_finished_timestamp_seconds gauge
photon_task_last_finished_timestamp_seconds{result="succeeded",task="scan_libraries"} 1.7e+09
`
	if err := testutil.CollectAndCompare(leader, strings.NewReader(want),
		"photon_jobs", "photon_library_items", "photon_nodes", "photon_task_last_finished_timestamp_seconds"); err != nil {
		t.Error(err)
	}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(leader)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "photon_jobs_oldest_due_seconds" {
			if m := f.GetMetric(); len(m) != 1 || m[0].GetGauge().GetValue() < 3599 || m[0].GetGauge().GetValue() > 3700 {
				t.Errorf("oldest due = %v, want the identify queue an hour waiting", m)
			}
			return
		}
	}
	t.Error("no photon_jobs_oldest_due_seconds")
}
