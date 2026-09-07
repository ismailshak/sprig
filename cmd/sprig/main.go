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
	"strings"
	"syscall"
	"time"

	// Embeds the timezone database, so the binary resolves users' timezones
	// in a base image that has none.
	_ "time/tzdata"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/auth"
	sprighttp "github.com/ismailshak/sprig/internal/http"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/store"
	"github.com/ismailshak/sprig/web"
)

func main() {
	// A subcommand needs no configuration and no database, so it runs before
	// either is read.
	if len(os.Args) > 1 {
		if err := subcommand(os.Args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// subcommand runs the command named by args. The only command is vapid: it
// prints a new VAPID key pair as the two environment variables that hold it.
// SPRIG_VAPID_SUBJECT is not printed, because that is a contact address the
// operator chooses.
func subcommand(args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "vapid" {
		return fmt.Errorf("unknown command %q: vapid is the only command, and the server runs with none", strings.Join(args, " "))
	}
	keys, err := push.GenerateKeys()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "SPRIG_VAPID_PUBLIC_KEY=%s\nSPRIG_VAPID_PRIVATE_KEY=%s\n", keys.Public, keys.Private)
	return err
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

	photos, err := photo.NewStore(cfg.photoDir)
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

	// The Notifications page gives pushKey to the browser to subscribe with.
	// With push off there is no key and no digest job.
	pushKey := ""
	var digest *push.Digest
	var wake func()
	if cfg.pushEnabled {
		pushKey = cfg.push.Public
		digest = push.NewDigest(logger, queries, push.NewSender(cfg.push, nil), cfg.baseURL.String())
		wake = digest.Wake
	}
	handler := sprighttp.New(logger, sessions, passkeys, resolver, queries, photos, templates, assets, cfg.trustedIPHeader, cfg.signupEnabled, pushKey, wake)
	if digest == nil {
		return serve(ctx, logger, listener, handler)
	}

	// The job is stopped and waited for before run returns, because the deferred
	// pool.Close would otherwise run while the job was still querying.
	jobCtx, stopJob := context.WithCancel(ctx)
	defer stopJob()
	done := make(chan struct{})
	go func() {
		defer close(done)
		digest.Run(jobCtx)
	}()
	err = serve(ctx, logger, listener, handler)
	stopJob()
	<-done
	return err
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
