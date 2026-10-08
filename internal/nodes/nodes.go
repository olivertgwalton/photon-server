// Package nodes keeps this server node doing what an admin sets of it, taking up a change at once.
package nodes

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// rereadEvery is how often a node reads what is set of it, besides as an admin changes it: a
// change whose event was lost with Valkey's connection is taken up within it.
const rereadEvery = time.Minute

type settings interface {
	JoinNode(ctx context.Context, id uuid.UUID, name string) (domain.NodeRecord, error)
	SeeNode(ctx context.Context, id uuid.UUID) (domain.NodeRecord, error)
}

// transcodes are this node's transcode slots, whose number may change while in use.
type transcodes interface {
	Transcodes() (active, conversions, limit int)
	SetLimit(limit int)
}

// Self is this node: what it is, and what an admin sets of it.
type Self struct {
	id      uuid.UUID
	name    string
	address string
	encoder domain.Encoder
	// automatic is the limit its encoder keeps up with.
	automatic  int
	settings   settings
	transcodes transcodes
	log        *slog.Logger

	mu  sync.Mutex
	set domain.NodeSettings
	// stopping is whether the process is stopping, which drains it whatever is set.
	stopping bool
	// changed is told, once for any number of changes, as what is set of it changes.
	changed chan struct{}
}

// Join keeps this node among the server's nodes, as name, reached by the others at address, and
// takes up what an admin has set of it.
func Join(ctx context.Context, s settings, id uuid.UUID, name, address string, encoder domain.Encoder, automatic int, t transcodes, log *slog.Logger) (*Self, error) {
	n, err := s.JoinNode(ctx, id, name)
	if err != nil {
		return nil, err
	}
	self := &Self{
		id: id, name: name, address: address, encoder: encoder, automatic: automatic,
		settings: s, transcodes: t, log: log, changed: make(chan struct{}, 1),
	}
	self.apply(n.NodeSettings)
	return self, nil
}

// Run takes up what an admin sets of this node until ctx ends, as it changes and every
// rereadEvery.
func (s *Self) Run(ctx context.Context, subscribe func() (<-chan domain.Event, func())) {
	follow.Events(ctx, subscribe, rereadEvery, s.reread, domain.EventNodesChanged)
}

// reread keeps that this node is up, and takes up what is set of it. One an admin forgot while it
// ran joins again, as it would starting.
func (s *Self) reread(ctx context.Context) {
	n, err := s.settings.SeeNode(ctx, s.id)
	if errors.Is(err, store.ErrNotFound) {
		n, err = s.settings.JoinNode(ctx, s.id, s.name)
	}
	if err != nil {
		if ctx.Err() == nil {
			s.log.WarnContext(ctx, "node settings not read", slog.Any("err", err))
		}
		return
	}
	s.apply(n.NodeSettings)
}

func (s *Self) apply(set domain.NodeSettings) {
	s.mu.Lock()
	was := s.set
	s.set = set
	s.mu.Unlock()
	if set != was {
		select {
		case s.changed <- struct{}{}:
		default:
		}
	}
	limit := s.automatic
	switch set.LimitSource {
	case domain.LimitSet:
		limit = set.Limit
	case domain.LimitAutomatic:
	}
	s.transcodes.SetLimit(limit)
}

// Node is this node as it tells the others of itself, now.
func (s *Self) Node() domain.Node {
	s.mu.Lock()
	set, stopping := s.set, s.stopping
	s.mu.Unlock()
	if stopping {
		set.Availability = domain.NodeDraining
	}
	n := domain.Node{
		ID: s.id, Address: s.address, Name: s.name, Role: set.Role, Availability: set.Availability,
		Encoder: s.encoder, LimitSource: set.LimitSource,
	}
	n.Transcodes, n.Conversions, n.Limit = s.transcodes.Transcodes()
	return n
}

// TakesTranscodes reports whether this node takes new video to encode now: its role encodes, and
// it is not drained.
func (s *Self) TakesTranscodes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.set.Role.Encodes() && s.set.Availability.Takes() && !s.stopping
}

// Stopping reports whether this node's process is stopping.
func (s *Self) Stopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

// Stop drains this node as its process stops: it takes nothing new, and tells the others at once.
func (s *Self) Stop() {
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Changes is told as what is set of this node changes, once for any number since it was read.
func (s *Self) Changes() <-chan struct{} { return s.changed }

var nodeInfo = prometheus.NewDesc("photon_node_info", "This node: its name, its role, and whether it takes new work.",
	[]string{"node", "role", "state"}, nil)

func (s *Self) Describe(ch chan<- *prometheus.Desc) { ch <- nodeInfo }

func (s *Self) Collect(ch chan<- prometheus.Metric) {
	n := s.Node()
	ch <- prometheus.MustNewConstMetric(nodeInfo, prometheus.GaugeValue, 1, n.Name, string(n.Role), string(n.Availability))
}
