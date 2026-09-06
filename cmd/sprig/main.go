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

	// Embeds the timezone database, so the binary resolves users' timezones
	// in a base image that has none.
	_ "time/tzdata"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/auth"
	sprighttp "github.com/ismailshak/sprig/internal/http"
	"github.com/ismailshak/sprig/internal/store"
	"github.com/ismailshak/sprig/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// shutdownGrace is how long in-flight requests get to finish after the process
// is told to stop.
const shutdownGrace = 10 * time.Second

// run loads config, connects to the database, migrates, and serves until ctx
// is cancelled or the server fails.
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

	// Migrate before listening, so no request is served against a schema older
	// than the binary.
	if err := store.Migrate(ctx, pool, db.Migrations, logger); err != nil {
		return err
	}

	assets, err := sprighttp.NewAssets(web.Static)
	if err != nil {
		return err
	}

	// Parse before listening too, so a template that does not parse stops the
	// process instead of returning a 500 on the first request for its page.
	templates, err := sprighttp.ParseTemplates(logger, cfg.templateDir, assets)
	if err != nil {
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
	passkeys, err := auth.NewPasskeys(queries, cfg.rpID, "sprig", cfg.baseURL.String(), cfg.cookie)
	if err != nil {
		return err
	}

	return serve(ctx, logger, listener, sprighttp.New(logger, sessions, passkeys, resolver, queries, templates, assets, cfg.trustedIPHeader, cfg.signupEnabled))
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
