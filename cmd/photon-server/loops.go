package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/discovery"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/reach"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// drain stops this node taking new work and plays its streams to their end, for limit at most,
// before it stops serving: another node takes new streams meanwhile. A signal on again stops it
// at once.
func drain(ctx context.Context, self interface{ Stop() }, streams interface{ Playbacks() []uuid.UUID }, again <-chan os.Signal, limit time.Duration, logger *slog.Logger) {
	self.Stop()
	t := time.NewTicker(drainPoll)
	defer t.Stop()
	until := time.After(limit)
	said := -1
	for {
		left := len(streams.Playbacks())
		if left == 0 {
			return
		}
		if left != said {
			logger.InfoContext(ctx, "draining: playing streams to their end; stop again to stop at once", slog.Int("streams", left))
			said = left
		}
		select {
		case <-t.C:
		case <-until:
			return
		case <-again:
			return
		}
	}
}

// listenUntilDone serves until ctx ends and drain returns, then gives open requests shutdownGrace
// to finish.
func listenUntilDone(ctx context.Context, srv *http.Server, either func(net.Listener) net.Listener, drain func()) error {
	served := make(chan error, 1)
	go func() {
		l, err := new(net.ListenConfig).Listen(ctx, "tcp", srv.Addr)
		if err != nil {
			served <- err
			return
		}
		served <- srv.Serve(either(l))
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	drain()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ready reports what keeps this node from taking new clients: a backend it cannot reach, or its
// stopping, so a balancer sends new clients to the others while it drains.
func ready(st *store.Store, cache *kv.KV, self interface{ Stopping() bool }) func(context.Context) error {
	return func(ctx context.Context) error {
		if self.Stopping() {
			return domain.ErrStopping
		}
		var errs []error
		if err := st.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("postgres: %w", err))
		}
		if err := cache.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("valkey: %w", err))
		}
		return errors.Join(errs...)
	}
}

// answerDiscovery answers clients looking for the server on UDP at the HTTP listener's port, while
// an admin has it on. Clients can still be given the address, so a port it cannot have is only a
// warning.
func answerDiscovery(ctx context.Context, addr string, r *reach.Reach, scheme func() string, info domain.Info, logger *slog.Logger) {
	conn, err := new(net.ListenConfig).ListenPacket(ctx, "udp", addr)
	if err == nil {
		on := func() bool { return r.Discovery() == domain.DiscoveryBroadcast }
		err = discovery.Serve(ctx, conn, info, on, scheme, logger)
	}
	if err != nil {
		logger.WarnContext(ctx, "clients must be given the server's address", slog.Any("err", err))
	}
}

// advertiseEvery is how often a node says where its peers reach it; it is forgotten after three
// times that, quiet.
const advertiseEvery = 15 * time.Second

// advertise tells the others where this node's peers reach it, as an admin sets it, so a request
// for HLS one of its playbacks makes is handed to it whichever node it lands on, and how many
// videos it encodes, said again as soon as either changes. A node with no address says nothing,
// and is forgotten by the others.
func advertise(ctx context.Context, cache *kv.KV, self func() domain.Node, slots, settings <-chan struct{}, logger *slog.Logger) {
	t := time.NewTicker(advertiseEvery)
	defer t.Stop()
	for {
		if n := self(); n.Address != "" {
			if err := cache.SetNode(ctx, n, 3*advertiseEvery); err != nil && ctx.Err() == nil {
				logger.WarnContext(ctx, "node not advertised", slog.Any("err", err))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-slots:
		case <-settings:
		}
	}
}

// sweepEvery is how often a node ends the playbacks of players that went away without stopping,
// closes its streams of playbacks that have ended, and forgets subtitles no one has read lately.
const sweepEvery = 30 * time.Second

func sweepPlaybacks(ctx context.Context, s *playback.Sessions, r *hls.Remuxer, logger *slog.Logger) {
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r.SweepSubtitles()
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "playbacks not swept", slog.Any("err", err))
		}
	}
}

// nodeIDs answers the ids of the nodes that say where their peers reach them.
func nodeIDs(cache *kv.KV) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		adverts, err := cache.Nodes(ctx)
		ids := make([]uuid.UUID, len(adverts))
		for i, n := range adverts {
			ids[i] = n.ID
		}
		return ids, err
	}
}

// pruneEvery is how often a node removes the converted files no download needs any more.
const pruneEvery = 10 * time.Minute

// pruneConversions prunes at start, which clears what a node stopped mid-conversion left, and
// every pruneEvery after.
func pruneConversions(ctx context.Context, c *playback.Conversions, logger *slog.Logger) {
	t := time.NewTicker(pruneEvery)
	defer t.Stop()
	for {
		if err := c.Prune(ctx); err != nil && ctx.Err() == nil {
			logger.WarnContext(ctx, "converted files not pruned", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
