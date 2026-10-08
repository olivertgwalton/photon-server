package jobs

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// outcome is how a run of a job ended; one postponed has not.
type outcome string

const (
	outcomeDone   outcome = "done"
	outcomeFailed outcome = "failed"
)

// Finished counts the runs of jobs that ended on a node, by kind and outcome, across its workers.
type Finished struct{ runs *prometheus.CounterVec }

func NewFinished() *Finished {
	return &Finished{prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "photon_jobs_finished_total",
		Help: "The runs of jobs this node ended, by kind and outcome: a failed run may be tried again.",
	}, []string{"kind", "outcome"})}
}

// start gives a kind its counts from zero, before any run of it ends.
func (f *Finished) start(kind domain.JobKind) {
	for _, o := range []outcome{outcomeDone, outcomeFailed} {
		f.runs.WithLabelValues(string(kind), string(o))
	}
}

func (f *Finished) Describe(ch chan<- *prometheus.Desc) { f.runs.Describe(ch) }

func (f *Finished) Collect(ch chan<- prometheus.Metric) { f.runs.Collect(ch) }
