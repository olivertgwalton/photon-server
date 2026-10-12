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

// deliveries are the playbacks this node delivers.
type deliveries interface {
	Delivering() int
	SetStreamLimit(limit int)
}

// egress is how fast this node sends its players media.
type egress interface {
	Kbps() int
}

// Parts are what this node is made of that what an admin sets of it changes.
type Parts struct {
	Transcodes transcodes
	Deliveries deliveries
	Egress     egress
	// Retool finds what this node encodes video on as choice says, and has it encode on that from
	// then: what it encodes with, and the limit on transcodes at once that keeps up with it.
	Retool func(ctx context.Context, choice domain.EncoderChoice) (domain.Encoder, int, error)
}

// Self is this node: what it is, and what an admin sets of it.
type Self struct {
	id       uuid.UUID
	name     string
	settings settings
	parts    Parts
	log      *slog.Logger
	// retooling is held while it finds what it encodes on, so one choice is tried at a time.
	retooling sync.Mutex

	mu  sync.Mutex
	set domain.NodeSettings
	// encoder is what it encodes with, found for choice, and automatic the limit it keeps up with.
	encoder   domain.Encoder
	choice    domain.EncoderChoice
	automatic int
	// stopping is whether the process is stopping, which drains it whatever is set.
	stopping bool
	// changed is told, once for any number of changes, as what is set of it changes.
	changed chan struct{}
}

// Join keeps this node among the server's nodes, as name, finds what it encodes on, and takes up
// what an admin has set of it.
func Join(ctx context.Context, s settings, id uuid.UUID, name string, p Parts, log *slog.Logger) (*Self, error) {
	n, err := s.JoinNode(ctx, id, name)
	if err != nil {
		return nil, err
	}
	encoder, automatic, err := p.Retool(ctx, n.Encoding)
	if err != nil {
		return nil, err
	}
	self := &Self{
		id: id, name: name, settings: s, parts: p, log: log, changed: make(chan struct{}, 1),
		encoder: encoder, choice: n.Encoding, automatic: automatic,
	}
	self.apply(ctx, n.NodeSettings)
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
	s.apply(ctx, n.NodeSettings)
}

// apply takes up set, finding what this node encodes on again, meanwhile, where its choice changed.
func (s *Self) apply(ctx context.Context, set domain.NodeSettings) {
	s.mu.Lock()
	was := s.set
	s.set = set
	retool := set.Encoding != s.choice
	s.choice = set.Encoding
	s.mu.Unlock()
	if set != was {
		s.tell()
	}
	s.limit()
	s.parts.Deliveries.SetStreamLimit(set.StreamLimit)
	if retool {
		go s.retool(context.WithoutCancel(ctx), set.Encoding)
	}
}

// limit holds this node to as many transcodes at once as is set, or as its encoder keeps up with.
func (s *Self) limit() {
	s.mu.Lock()
	limit := s.automatic
	switch s.set.LimitSource {
	case domain.LimitSet:
		limit = s.set.Limit
	case domain.LimitAutomatic:
	}
	s.mu.Unlock()
	s.parts.Transcodes.SetLimit(limit)
}

// retool finds what this node encodes on as choice says, unless an admin has chosen again since.
func (s *Self) retool(ctx context.Context, choice domain.EncoderChoice) {
	s.retooling.Lock()
	defer s.retooling.Unlock()
	s.mu.Lock()
	latest := s.choice
	s.mu.Unlock()
	if choice != latest {
		return
	}
	encoder, automatic, err := s.parts.Retool(ctx, choice)
	if err != nil {
		s.log.WarnContext(ctx, "what the node encodes on not found again", slog.Any("err", err))
		return
	}
	s.mu.Lock()
	s.encoder, s.automatic = encoder, automatic
	s.mu.Unlock()
	s.limit()
	s.tell()
}

// tell tells Changes, once for any number of changes since it was read.
func (s *Self) tell() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Node is this node as it tells the others of itself, now.
func (s *Self) Node() domain.Node {
	s.mu.Lock()
	set, stopping, encoder := s.set, s.stopping, s.encoder
	s.mu.Unlock()
	if stopping {
		set.Availability = domain.NodeDraining
	}
	n := domain.Node{
		ID: s.id, Address: set.Address, Name: s.name, Group: set.Group, PublicURL: set.PublicURL, LocalURL: set.LocalURL,
		StreamLimit: set.StreamLimit, EgressLimitMbps: set.EgressLimitMbps, Role: set.Role, Availability: set.Availability,
		Encoder: encoder, LimitSource: set.LimitSource,
	}
	n.Transcodes, n.Conversions, n.Limit = s.parts.Transcodes.Transcodes()
	n.Streams, n.EgressKbps = s.parts.Deliveries.Delivering(), s.parts.Egress.Kbps()
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
	s.tell()
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
