package main

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

// scrapeWithin bounds reading what a scrape reports from Postgres and Valkey.
const scrapeWithin = 5 * time.Second

// libraryKinds are the titles counted in photon_library_items: what is played, not what holds it.
var libraryKinds = []domain.ItemKind{domain.ItemMovie, domain.ItemEpisode}

var (
	jobsDesc = prometheus.NewDesc("photon_jobs", "The cluster's jobs, by kind and state.", []string{"kind", "state"}, nil)
	dueDesc  = prometheus.NewDesc("photon_jobs_oldest_due_seconds",
		"How long the job of a kind due now and waiting longest has been due.", []string{"kind"}, nil)
	taskDesc = prometheus.NewDesc("photon_task_last_finished_timestamp_seconds",
		"When each scheduled task's last run ended, and how.", []string{"task", "result"}, nil)
	nodesDesc = prometheus.NewDesc("photon_nodes",
		"The nodes telling the others of themselves, by whether each takes new work.", []string{"state"}, nil)
	itemsDesc = prometheus.NewDesc("photon_library_items", "The films and episodes in the libraries.", []string{"kind"}, nil)
)

type leader interface {
	Leads() bool
	Statuses(ctx context.Context) ([]task.Status, error)
}

type clusterStore interface {
	JobLoads(ctx context.Context) ([]store.JobLoad, error)
	ItemCounts(ctx context.Context, kinds []domain.ItemKind) (map[domain.ItemKind]int, error)
}

type adverts interface {
	Nodes(ctx context.Context) ([]domain.Node, error)
}

// cluster is what the nodes share, in Postgres and Valkey, said only by the node holding the
// scheduler lease, so that each is said once across the cluster.
type cluster struct {
	lead  leader
	st    clusterStore
	nodes adverts
}

func (c cluster) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{jobsDesc, dueDesc, taskDesc, nodesDesc, itemsDesc} {
		ch <- d
	}
}

func (c cluster) Collect(ch chan<- prometheus.Metric) {
	if !c.lead.Leads() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), scrapeWithin)
	defer cancel()
	if loads, err := c.st.JobLoads(ctx); err != nil {
		ch <- prometheus.NewInvalidMetric(jobsDesc, err)
	} else {
		for _, l := range loads {
			ch <- prometheus.MustNewConstMetric(jobsDesc, prometheus.GaugeValue, float64(l.Count), string(l.Kind), string(l.State))
			if !l.Due.IsZero() {
				ch <- prometheus.MustNewConstMetric(dueDesc, prometheus.GaugeValue, time.Since(l.Due).Seconds(), string(l.Kind))
			}
		}
	}
	if statuses, err := c.lead.Statuses(ctx); err != nil {
		ch <- prometheus.NewInvalidMetric(taskDesc, err)
	} else {
		for _, s := range statuses {
			if !s.State.Finished.IsZero() {
				ch <- prometheus.MustNewConstMetric(taskDesc, prometheus.GaugeValue, float64(s.State.Finished.Unix()), string(s.Key), string(s.State.Result))
			}
		}
	}
	if nodes, err := c.nodes.Nodes(ctx); err != nil {
		ch <- prometheus.NewInvalidMetric(nodesDesc, err)
	} else {
		by := map[domain.NodeAvailability]int{}
		for _, n := range nodes {
			by[n.Availability]++
		}
		for _, a := range domain.NodeAvailabilities() {
			ch <- prometheus.MustNewConstMetric(nodesDesc, prometheus.GaugeValue, float64(by[a]), string(a))
		}
	}
	if counts, err := c.st.ItemCounts(ctx, libraryKinds); err != nil {
		ch <- prometheus.NewInvalidMetric(itemsDesc, err)
	} else {
		for _, k := range libraryKinds {
			ch <- prometheus.MustNewConstMetric(itemsDesc, prometheus.GaugeValue, float64(counts[k]), string(k))
		}
	}
}
