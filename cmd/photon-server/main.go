package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/olivertgwalton/photon-server/internal/httpapi"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	defaultListen = ":8640"
	shutdownGrace = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("photon-server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := os.Getenv("PHOTON_DATABASE_URL")
	if databaseURL == "" {
		return errors.New("PHOTON_DATABASE_URL is not set")
	}
	switch {
	case len(args) == 0:
		return serve(ctx, logger, databaseURL)
	case len(args) == 1 && args[0] == "migrate":
		return store.Migrate(ctx, databaseURL, logger)
	}
	return fmt.Errorf("usage: photon-server [migrate], got %q", args)
}

func serve(ctx context.Context, logger *slog.Logger, databaseURL string) error {
	st, err := store.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer st.Close()
	id, err := st.ServerID(ctx)
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	info := httpapi.Info{
		ID:      id.String(),
		Name:    cmp.Or(os.Getenv("PHOTON_NAME"), hostname),
		Version: version(),
	}
	srv := &http.Server{
		Addr:              cmp.Or(os.Getenv("PHOTON_LISTEN"), defaultListen),
		Handler:           httpapi.New(logger, info),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	logger.InfoContext(ctx, "serving", slog.String("addr", srv.Addr), slog.String("version", info.Version))

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
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

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return "(devel)"
}
