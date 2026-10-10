package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	// The image has no zoneinfo, and the maintenance window is kept in a zone named by an admin.
	_ "time/tzdata"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/storage"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// version is stamped by the image's build: -ldflags "-X main.version=…".
var version = "(devel)"

const (
	// listen is where every node answers: a container maps it to any port the host likes.
	listen        = ":8640"
	shutdownGrace = 10 * time.Second
	// drainFor is the longest a node stopping plays its streams to their end: a film's length.
	drainFor = 2 * time.Hour
	// drainPoll is how often a node draining looks whether its streams have ended.
	drainPoll = 5 * time.Second
)

const (
	// scanSlots is how many libraries a node scans at once, each scan being one library's: a
	// films library and a shows library together, which are usually on the same disk or mount,
	// so a third would only share its reads.
	scanSlots = 2
	// identifySlots is how many titles a node matches at once. A match spends its time waiting
	// on providers, a request at a time, about two seconds of it a title (measured against TMDB),
	// so sixteen at once reach TMDB's rate limit, which every node shares; past it a match waits
	// on the limit, not the network.
	identifySlots = 16
	// webhookSlots is how many deliveries a node makes at once, so one receiver that does not
	// answer holds up no other.
	webhookSlots = 2
	// importSlots is how many history imports a node runs at once, each a source's whole library
	// read a page at a time.
	importSlots = 1
	// mediaSlots is how many jobs of each kind that reads media a node runs at once: one, as
	// Jellyfin's chapter images and trickplay go through files one by one and Plex's butler file by
	// file. A still is a seek into the whole file, and on a network mount ten at once took every
	// byte a stream needed (measured: every chapter timed out, and 64 MiB took minutes to read).
	mediaSlots = 1
)

// exitRestart is the exit code of a node stopped for a restore, which its restart policy starts
// again: EX_TEMPFAIL, a failure, so a policy that restarts on failure alone does too.
const exitRestart = 75

// errRestart is a node stopped for a restore, to be started again.
var errRestart = errors.New("stopped for a restore; to be started again")

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	stops := make(chan os.Signal, 2)
	signal.Notify(stops, os.Interrupt, syscall.SIGTERM)
	err := run(logger, os.Args[1:], stops)
	switch {
	case errors.Is(err, errRestart):
		logger.Info("photon-server stopped for a restore; its restart policy starts it again", slog.Int("exit", exitRestart))
		os.Exit(exitRestart)
	case err != nil:
		logger.Error("photon-server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

// run runs what args ask for until it is done or told to stop on stops; a node draining its
// streams stops at once when told again.
func run(logger *slog.Logger, args []string, stops <-chan os.Signal) error {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		select {
		case <-stops:
			stop()
		case <-ctx.Done():
		}
	}()

	if len(args) == 1 && args[0] == "openapi" {
		return writeDescription(os.Stdout)
	}
	databaseURL, err := requiredEnv("PHOTON_DATABASE_URL")
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		return serve(ctx, logger, databaseURL, stops)
	case len(args) == 1 && args[0] == "migrate":
		return store.Migrate(ctx, databaseURL, logger)
	case len(args) == 2 && args[0] == "restore":
		valkeyURL, err := requiredEnv("PHOTON_VALKEY_URL")
		if err != nil {
			return err
		}
		r, err := restorer(databaseURL, valkeyURL, logger)
		if err != nil {
			return err
		}
		return r.Restore(ctx, args[1], os.Stdout)
	}
	return fmt.Errorf("usage: photon-server [migrate | restore <dump> | openapi], got %q", args)
}

// writeDescription writes the API's OpenAPI description, so a client's types are generated
// without a server or database.
func writeDescription(w io.Writer) error {
	doc, err := httpapi.Describe(domain.Info{Version: version})
	if err != nil {
		return err
	}
	_, err = w.Write(append(doc, '\n'))
	return err
}

func requiredEnv(name string) (string, error) {
	if v := os.Getenv(name); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is not set", name)
}

// serve runs the node until it is told to stop; stopped for a restore, it restores the dump
// where it keeps it, and answers errRestart, for the restart policy to start it again.
func serve(ctx context.Context, logger *slog.Logger, databaseURL string, stops <-chan os.Signal) error {
	valkeyURL, err := requiredEnv("PHOTON_VALKEY_URL")
	if err != nil {
		return err
	}
	err = serveNode(ctx, logger, databaseURL, valkeyURL, stops)
	stopped, ok := errors.AsType[*stoppedForRestore](err)
	if !ok {
		return err
	}
	if stopped.leads {
		r, err := restorer(databaseURL, valkeyURL, logger)
		if err != nil {
			return err
		}
		if err := lead(ctx, logger, r, stopped.file, stopped.server, stopped.restore); err != nil {
			return err
		}
	}
	return errRestart
}

// serveNode opens what the node keeps its state in, wires the node together on it, and serves
// until it is told to stop.
func serveNode(ctx context.Context, logger *slog.Logger, databaseURL, valkeyURL string, stops <-chan os.Signal) error {
	started := time.Now()
	tools, err := mediaTools(ctx)
	if err != nil {
		return err
	}
	logTools(ctx, logger, tools)
	underway := func(ctx context.Context) (bool, error) { return restoreUnderway(ctx, databaseURL, valkeyURL) }
	if err := awaitRestore(ctx, listen, underway, logger); err != nil {
		return err
	}
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	id, err := st.ServerID(ctx)
	if err != nil {
		return err
	}
	cache, err := kv.Open(valkeyURL, id)
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	defer cache.Close()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cacheRoot := filepath.Join(cacheDir, "photon-server")
	stores, err := storage.Open(ctx, st, cacheRoot, logger)
	if err != nil {
		return err
	}
	defer stores.Close()
	n := &node{
		logger: logger, st: st, cache: cache, stores: stores, tools: tools, server: id,
		started: started, listen: listen, cacheRoot: cacheRoot, stops: stops,
	}
	if err := n.join(ctx, databaseURL, valkeyURL); err != nil {
		return err
	}
	// A restore under way stops the node as a signal does, but for its streams, which it ends;
	// the watch outlives what it stops, to keep the restore while this node goes on to restore it.
	ctx, stopFor := context.WithCancelCause(ctx)
	defer stopFor(nil)
	watched := make(chan struct{})
	defer close(watched)
	go watchRestore(context.WithoutCancel(ctx), cache, id, n.id, n.dumper.Dir, stopFor, watched, logger)
	if err := n.wire(ctx); err != nil {
		return err
	}
	return n.serve(ctx)
}
