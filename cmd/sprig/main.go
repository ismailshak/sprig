// Command sprig serves the plant tracker.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/auth"
	sprighttp "github.com/ismailshak/sprig/internal/http"
	"github.com/ismailshak/sprig/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// shutdownGrace is how long an in-flight request has to finish once the
// process has been asked to stop.
const shutdownGrace = 10 * time.Second

// run wires config, logging, the database and the server together, and blocks
// until ctx is cancelled or the server fails.
func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}

	logger := newLogger(cfg, stdout)

	pool, err := store.Open(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Before the listener, so nothing is served against a schema that is behind
	// the binary.
	if err := store.Migrate(ctx, pool, db.Migrations, logger); err != nil {
		return err
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	queries := store.New(pool)
	sessions := auth.NewSessions(queries, cfg.sessionTTL, cfg.cookie)
	resolver := auth.NewResolver(sessions, queries)

	return serve(ctx, logger, listener, sprighttp.New(logger, sessions, resolver, queries))
}

// serve runs the server on listener until ctx is cancelled, then gives
// in-flight requests shutdownGrace to finish.
func serve(ctx context.Context, logger *slog.Logger, listener net.Listener, handler http.Handler) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", listener.Addr().String())
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down", "grace", shutdownGrace)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := <-errCh; err != nil {
			return err
		}

		logger.Info("stopped")
		return nil
	case err := <-errCh:
		return err
	}
}
