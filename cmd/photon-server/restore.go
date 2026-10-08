package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// restorePoll is how often a node looks for a restore under way, and the node restoring keeps
	// it and looks whether the others have let go of the database.
	restorePoll = 2 * time.Second
	// restoreWait is the longest the node restoring waits for the others to let go of the database
	// before it gives the restore up and they start again.
	restoreWait = 2 * time.Minute
)

// stoppedForRestore is a node stopped for a restore, which it restores from file where it leads.
type stoppedForRestore struct {
	restore domain.Restore
	server  uuid.UUID
	leads   bool
	file    string
}

func (s *stoppedForRestore) Error() string { return "stopped for the restore of " + s.restore.Dump }

// watchRestore stops the node, by stop, once a restore is under way, keeping it while this node
// is the one to restore it, until done closes.
func watchRestore(ctx context.Context, cache *kv.KV, server, node uuid.UUID, dir string, stop context.CancelCauseFunc, done <-chan struct{}, logger *slog.Logger) {
	t := time.NewTicker(restorePoll)
	defer t.Stop()
	told := false
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		r, ok, err := cache.Restoring(ctx)
		if err != nil {
			logger.WarnContext(ctx, "could not look for a restore under way", slog.Any("err", err))
			continue
		}
		if !ok {
			continue
		}
		leads := r.Node == node
		if leads {
			if err := cache.KeepRestore(ctx, backup.RestoreKept); err != nil {
				logger.WarnContext(ctx, "could not keep the restore under way", slog.Any("err", err))
			}
		}
		if !told {
			logger.InfoContext(ctx, "stopping for a restore", slog.String("dump", r.Dump), slog.Bool("restores", leads))
			stop(&stoppedForRestore{restore: r, server: server, leads: leads, file: filepath.Join(dir, r.Dump)})
			told = true
		}
	}
}

// endStreams stops the node taking work and ends its streams: a restore leaves no playback to
// go on with.
func endStreams(self interface{ Stop() }, streams interface {
	Playbacks() []uuid.UUID
	Close(playback uuid.UUID)
},
) {
	self.Stop()
	for _, p := range streams.Playbacks() {
		streams.Close(p)
	}
}

// restoreUnderway reports whether a restore is under way, asked before the node opens the
// database: one holds it, or Valkey keeps one.
func restoreUnderway(ctx context.Context, databaseURL, valkeyURL string) (bool, error) {
	id, err := store.WaitingServerID(ctx, databaseURL)
	if errors.Is(err, store.ErrHeld) {
		return true, nil
	}
	if err != nil || id == (uuid.UUID{}) {
		return false, err
	}
	cache, err := kv.Open(valkeyURL, id)
	if err != nil {
		return false, fmt.Errorf("valkey: %w", err)
	}
	defer cache.Close()
	_, ok, err := cache.Restoring(ctx)
	return ok, err
}

// awaitRestore holds the node back from opening the database while a restore is under way,
// answering at listen that it is restoring, until it ends or lapses.
func awaitRestore(ctx context.Context, listen string, underway func(context.Context) (bool, error), logger *slog.Logger) error {
	ok, err := underway(ctx)
	if err != nil || !ok {
		return err
	}
	l, err := new(net.ListenConfig).Listen(ctx, "tcp", listen)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "waiting for a restore to finish before starting")
	return answerRestoring(ctx, l, underway, logger)
}

// answerRestoring serves l, every request told the server is restoring, until underway says the
// restore has ended.
func answerRestoring(ctx context.Context, l net.Listener, underway func(context.Context) (bool, error), logger *slog.Logger) error {
	srv := &http.Server{Handler: httpapi.Restoring(logger), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(l) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	t := time.NewTicker(restorePoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		ok, err := underway(ctx)
		if err != nil {
			logger.WarnContext(ctx, "could not look for a restore under way", slog.Any("err", err))
			continue
		}
		if !ok {
			return nil
		}
	}
}

// lead restores the dump once the other nodes have let go of the database, keeping the restore
// under way meanwhile, and records how it ended. A restore given up ends, so the others start.
func lead(ctx context.Context, logger *slog.Logger, r backup.Restorer, file string, server uuid.UUID, restore domain.Restore) error {
	cache, err := kv.Open(r.ValkeyURL, server)
	if err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	defer cache.Close()
	keeping, stopKeeping := context.WithCancel(ctx)
	kept := make(chan struct{})
	go func() {
		defer close(kept)
		t := time.NewTicker(restorePoll)
		defer t.Stop()
		for {
			if err := cache.KeepRestore(keeping, backup.RestoreKept); err != nil && keeping.Err() == nil {
				logger.WarnContext(ctx, "could not keep the restore under way", slog.Any("err", err))
			}
			select {
			case <-keeping.Done():
				return
			case <-t.C:
			}
		}
	}()
	stop := func() {
		stopKeeping()
		<-kept
	}
	err = restoreAlone(ctx, r, file, cache, restore, logger)
	stop()
	if err == nil {
		logger.InfoContext(ctx, "restored; starting again", slog.String("dump", restore.Dump))
		return nil
	}
	end := context.WithoutCancel(ctx)
	if endErr := cache.EndRestore(end); endErr != nil {
		logger.WarnContext(ctx, "could not end the restore", slog.Any("err", endErr))
	}
	outcome := domain.RestoreOutcome{Dump: restore.Dump, At: time.Now().UTC(), Result: domain.RestoreFailed, Reason: err.Error()}
	if saveErr := cache.SaveRestoreOutcome(end, outcome); saveErr != nil {
		logger.WarnContext(ctx, "could not record how the restore ended", slog.Any("err", saveErr))
	}
	return fmt.Errorf("restoring %s: %w", restore.Dump, err)
}

// restoreAlone waits, up to restoreWait, for every other connection to the database to close,
// then restores file.
func restoreAlone(ctx context.Context, r backup.Restorer, file string, cache *kv.KV, restore domain.Restore, logger *slog.Logger) error {
	until := time.Now().Add(restoreWait)
	for {
		others, err := store.Connections(ctx, r.DatabaseURL)
		if err != nil {
			return err
		}
		if len(others) == 0 {
			break
		}
		if time.Now().After(until) {
			return fmt.Errorf("still connected after %s: %s", restoreWait, strings.Join(others, "; "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(restorePoll):
		}
	}
	restore.Phase = domain.RestoreRestoring
	if err := cache.SaveRestore(ctx, restore, backup.RestoreKept); err != nil {
		return err
	}
	var said strings.Builder
	err := r.Restore(ctx, file, &said)
	logger.InfoContext(ctx, "restore", slog.String("said", said.String()))
	return err
}
