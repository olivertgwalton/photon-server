package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// scrapeHandler answers what g gathers. A source that cannot be read, Valkey out of reach say,
// leaves its own metrics out and is logged; the rest are answered.
func scrapeHandler(g prometheus.Gatherer, logger *slog.Logger) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn), ErrorHandling: promhttp.ContinueOnError,
	})
}

func (a *API) metrics(w http.ResponseWriter, r *http.Request) { a.scrape.ServeHTTP(w, r) }

// metricsJSON is what the nodes' metrics say now, each node's own, and what they share once, from
// the node holding the scheduler lease; cluster is absent while none holds it.
type metricsJSON struct {
	Nodes   []nodeMetricsJSON   `json:"nodes"`
	Cluster *clusterMetricsJSON `json:"cluster,omitempty"`
}

// nodeMetricsJSON is a node running, as it tells the others of itself, and what its metrics say;
// one unreachable says why instead.
type nodeMetricsJSON struct {
	ID           uuid.UUID               `json:"id"`
	Name         string                  `json:"name"`
	Role         domain.NodeRole         `json:"role"`
	Availability domain.NodeAvailability `json:"availability"`
	Reach        nodeReach               `json:"reach"`
	Error        string                  `json:"error,omitzero"`
	Metrics      *ownMetricsJSON         `json:"metrics,omitempty"`
}

// nodeReach is whether a node answered when asked for its metrics.
type nodeReach string

const (
	reachAnswered nodeReach = "answered"
	// reachUnreachable is a node that did not answer within gatherWithin, or answered an error.
	reachUnreachable nodeReach = "unreachable"
)

func nodeReaches() []nodeReach { return []nodeReach{reachAnswered, reachUnreachable} }

// gatherWithin is how long a node has to answer its metrics before it is shown unreachable: a
// dashboard asks every few seconds, and waits on the slowest node.
const gatherWithin = 2 * time.Second

// metricsPath is where a node is asked for its metrics by another.
const metricsPath = "/api/v1/internal/metrics"

// ownMetricsJSON is what a node's metrics say of itself, gathered at at. The counters, starts,
// refusals, sent bytes and CPU seconds, are totals since it started, for rates between two
// answers; transcode_slots is absent with no limit.
type ownMetricsJSON struct {
	At                  time.Time                     `json:"at"`
	Playbacks           map[domain.PlayMethod]int     `json:"playbacks"`
	Transcodes          map[string]int                `json:"transcodes"`
	TranscodeSlots      int                           `json:"transcode_slots,omitzero"`
	PlaybackStarts      map[domain.PlayMethod]float64 `json:"playback_starts"`
	TranscodeRefusals   map[string]float64            `json:"transcode_refusals"`
	SentBytes           map[string]float64            `json:"sent_bytes"`
	CPUSeconds          float64                       `json:"cpu_seconds"`
	ResidentMemoryBytes float64                       `json:"resident_memory_bytes"`
	SegmentWait         histogramJSON                 `json:"segment_wait"`
}

// histogramJSON is a histogram's buckets, each counting the observations at most le, as
// Prometheus' do, beside the count and sum of every one.
type histogramJSON struct {
	Buckets []histogramBucketJSON `json:"buckets"`
	Count   uint64                `json:"count"`
	Sum     float64               `json:"sum"`
}

type histogramBucketJSON struct {
	LE    float64 `json:"le"`
	Count uint64  `json:"count"`
}

// clusterMetricsJSON is what the nodes share, as node, holding the scheduler lease, said it:
// oldest_due_seconds is how long the job of each kind due now and waiting longest has waited.
type clusterMetricsJSON struct {
	Node         uuid.UUID                       `json:"node"`
	Jobs         []jobCountJSON                  `json:"jobs"`
	OldestDue    map[domain.JobKind]float64      `json:"oldest_due_seconds"`
	Tasks        []taskFinishedJSON              `json:"tasks"`
	Nodes        map[domain.NodeAvailability]int `json:"nodes"`
	LibraryItems map[domain.ItemKind]int         `json:"library_items"`
}

type taskFinishedJSON struct {
	Task       domain.TaskKey    `json:"task"`
	Result     domain.TaskResult `json:"result"`
	FinishedAt time.Time         `json:"finished_at"`
}

// gatheredJSON is a node's metrics as it gathered them: its own, and what the nodes share where it
// holds the scheduler lease.
type gatheredJSON struct {
	Own     ownMetricsJSON      `json:"own"`
	Cluster *clusterMetricsJSON `json:"cluster,omitempty"`
}

// gather is what this node's metrics say now. A source that cannot be read is left out, and
// logged, as a scrape leaves it.
func (a *API) gather(ctx context.Context) gatheredJSON {
	families, err := a.svc.Metrics.Gather()
	if err != nil {
		a.logger.WarnContext(ctx, "metrics left out", slog.Any("err", err))
	}
	return shapeMetrics(families, time.Now().UTC())
}

func shapeMetrics(families []*dto.MetricFamily, at time.Time) gatheredJSON {
	own := ownMetricsJSON{
		At: at, Playbacks: map[domain.PlayMethod]int{}, Transcodes: map[string]int{}, PlaybackStarts: map[domain.PlayMethod]float64{},
		TranscodeRefusals: map[string]float64{}, SentBytes: map[string]float64{}, SegmentWait: histogramJSON{Buckets: []histogramBucketJSON{}},
	}
	var cluster *clusterMetricsJSON
	shared := func() *clusterMetricsJSON {
		if cluster == nil {
			cluster = &clusterMetricsJSON{
				Jobs: []jobCountJSON{}, OldestDue: map[domain.JobKind]float64{}, Tasks: []taskFinishedJSON{},
				Nodes: map[domain.NodeAvailability]int{}, LibraryItems: map[domain.ItemKind]int{},
			}
		}
		return cluster
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			// A metric is a gauge or a counter; the other is nil, read as zero.
			v := m.GetGauge().GetValue() + m.GetCounter().GetValue()
			switch f.GetName() {
			case "photon_playbacks":
				own.Playbacks[domain.PlayMethod(label(m, "method"))] = int(v)
			case "photon_transcodes":
				own.Transcodes[label(m, "kind")] = int(v)
			case "photon_transcode_slots":
				own.TranscodeSlots = int(v)
			case "photon_playback_starts_total":
				own.PlaybackStarts[domain.PlayMethod(label(m, "method"))] = v
			case "photon_transcode_refusals_total":
				own.TranscodeRefusals[label(m, "reason")] = v
			case "photon_sent_bytes_total":
				own.SentBytes[label(m, "delivery")] = v
			case "process_cpu_seconds_total":
				own.CPUSeconds = v
			case "process_resident_memory_bytes":
				own.ResidentMemoryBytes = v
			case "photon_hls_segment_wait_seconds":
				h := m.GetHistogram()
				own.SegmentWait.Count, own.SegmentWait.Sum = h.GetSampleCount(), h.GetSampleSum()
				for _, b := range h.GetBucket() {
					own.SegmentWait.Buckets = append(own.SegmentWait.Buckets, histogramBucketJSON{LE: b.GetUpperBound(), Count: b.GetCumulativeCount()})
				}
			case "photon_jobs":
				c := shared()
				c.Jobs = append(c.Jobs, jobCountJSON{Kind: domain.JobKind(label(m, "kind")), State: domain.JobState(label(m, "state")), Count: int(v)})
			case "photon_jobs_oldest_due_seconds":
				shared().OldestDue[domain.JobKind(label(m, "kind"))] = v
			case "photon_task_last_finished_timestamp_seconds":
				c := shared()
				c.Tasks = append(c.Tasks, taskFinishedJSON{
					Task: domain.TaskKey(label(m, "task")), Result: domain.TaskResult(label(m, "result")), FinishedAt: time.Unix(int64(v), 0).UTC(),
				})
			case "photon_nodes":
				shared().Nodes[domain.NodeAvailability(label(m, "state"))] = int(v)
			case "photon_library_items":
				shared().LibraryItems[domain.ItemKind(label(m, "kind"))] = int(v)
			}
		}
	}
	return gatheredJSON{Own: own, Cluster: cluster}
}

func label(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

// nodeMetrics answers another node what this one's metrics say.
func (a *API) nodeMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.logger, "application/json", http.StatusOK, a.gather(r.Context()))
}

// adminMetrics answers what the metrics of every node running say now, asking the others at once,
// and what the nodes share, from the node holding the scheduler lease. A node that does not answer
// is listed unreachable.
func (a *API) adminMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	online, err := a.online(ctx)
	if a.answered(w, r, err) {
		return
	}
	nodes := slices.SortedFunc(maps.Values(online), func(x, y domain.Node) int {
		return cmp.Or(cmp.Compare(x.Name, y.Name), x.ID.Compare(y.ID))
	})
	self := a.svc.Placer.Self().ID
	gathered := make([]gatheredJSON, len(nodes))
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Go(func() {
			if n.ID == self {
				gathered[i] = a.gather(ctx)
				return
			}
			gathered[i], errs[i] = a.askMetrics(ctx, n)
		})
	}
	wg.Wait()
	out := metricsJSON{Nodes: make([]nodeMetricsJSON, len(nodes))}
	for i, n := range nodes {
		out.Nodes[i] = nodeMetricsJSON{ID: n.ID, Name: n.Name, Role: n.Role, Availability: n.Availability, Reach: reachAnswered}
		if errs[i] != nil {
			out.Nodes[i].Reach, out.Nodes[i].Error = reachUnreachable, errs[i].Error()
			continue
		}
		out.Nodes[i].Metrics = &gathered[i].Own
		// While the lease passes, two nodes may say what is shared, or neither.
		if c := gathered[i].Cluster; c != nil && out.Cluster == nil {
			c.Node = n.ID
			out.Cluster = c
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// askMetrics asks n, another node, what its metrics say.
func (a *API) askMetrics(ctx context.Context, n domain.Node) (gatheredJSON, error) {
	ctx, cancel := context.WithTimeout(ctx, gatherWithin)
	defer cancel()
	var g gatheredJSON
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.Address+metricsPath, nil)
	if err != nil {
		return g, err
	}
	a.svc.NodeKey.Sign(req, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return g, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return g, fmt.Errorf("%s answered %s", n.Name, resp.Status)
	}
	return g, json.NewDecoder(resp.Body).Decode(&g)
}
