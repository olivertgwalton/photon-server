package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/olivertgwalton/photon-server/internal/httpapi"
)

const (
	defaultListen = ":8640"
	shutdownGrace = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("photon-server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	info := httpapi.Info{
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
	logger.Info("serving", slog.String("addr", srv.Addr), slog.String("version", info.Version))

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
