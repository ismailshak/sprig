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
	"sync"
	"syscall"
	"time"
	"uuid"

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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	if len(os.Args) > 1 {
		err = subcommand(ctx, os.Args[1:], os.Getenv, os.Stdout)
	} else {
		err = run(ctx, os.Getenv, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func subcommand(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	switch {
	case len(args) == 1 && args[0] == "vapid":
		return vapid(stdout)
	case len(args) == 1 && args[0] == "sweep":
		return sweep(ctx, getenv, stdout)
	case len(args) == 1 && args[0] == "health":
		return health(ctx, getenv)
	case len(args) > 0 && args[0] == "admin":
		return admin(ctx, args[1:], getenv, stdout)
	default:
		return fmt.Errorf("unknown command %q: the commands are vapid, sweep, health and admin, and the server runs with none", strings.Join(args, " "))
	}
}

// vapid prints a new VAPID key pair as the two environment variables that
// hold it. SPRIG_VAPID_SUBJECT is not printed, because that is a contact
// address the operator chooses.
func vapid(stdout io.Writer) error {
	keys, err := push.GenerateKeys()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "SPRIG_VAPID_PUBLIC_KEY=%s\nSPRIG_VAPID_PRIVATE_KEY=%s\n", keys.Public, keys.Private)
	return err
}

// sweep deletes the sessions, invites and recovery codes that no longer mean
// anything, then passes over the photo directory and the photo rows, printing
// what it removed. It does not run the migrations, because the server that
// wrote the rows has already applied them. A photo row with no file makes it
// return an error, so the exit status is non-zero.
func sweep(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	// photo.NewStore creates the directory when it is missing. The sweep
	// stats it first, because a mount that did not come up would otherwise
	// look like a directory whose files have all been deleted.
	if _, err := os.Stat(cfg.photoDir); err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := store.New(pool)
	rows, err := auth.Sweep(ctx, queries, time.Now(), cfg.sessionTTL)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "deleted %d expired sessions, %d redeemed invites and expired sign-in links, %d replaced recovery codes\n", rows.Sessions, rows.Invites, rows.RecoveryCodes); err != nil {
		return err
	}

	photos, err := photo.NewStore(cfg.photoDir, cfg.photoQuota)
	if err != nil {
		return err
	}
	report, err := photos.Sweep(ctx, queries, stdout)
	if err != nil {
		return err
	}
	if report.Missing > 0 {
		return fmt.Errorf("%d photo rows have no file under %s", report.Missing, cfg.photoDir)
	}
	return nil
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

	photos, err := photo.NewStore(cfg.photoDir, cfg.photoQuota)
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
	tokens := auth.NewAPITokens(queries)
	passkeys, err := auth.NewPasskeys(queries, cfg.rpID, "sprig", cfg.baseURL.String(), cfg.cookie)
	if err != nil {
		return err
	}

	// The Notifications page gives pushKey to the browser to subscribe with.
	// With push off there is no key, no digest job and no activity
	// notification.
	pushKey := ""
	var digest *push.Digest
	var activity *push.Activity
	var wake func()
	var notify func(context.Context, uuid.UUID, uuid.UUID, push.Notification)
	var test func(context.Context, store.PushSubscription, push.Notification) error
	if cfg.pushEnabled {
		pushKey = cfg.push.Public
		sender := push.NewSender(cfg.push, nil)
		digest = push.NewDigest(logger, queries, sender, cfg.baseURL.String())
		activity = push.NewActivity(logger, queries, sender, cfg.baseURL.String())
		wake = digest.Wake
		notify = activity.Send
		test = push.NewTestMessage(queries, sender, cfg.baseURL.String()).Send
	}
	handler := sprighttp.New(logger, sessions, passkeys, resolver, tokens, queries, photos, templates, assets, cfg.trustedIPHeader, cfg.signupEnabled, pushKey, wake, notify, test)

	// run does not return until the sweep and the digest job have stopped and
	// every activity notification has finished sending. The deferred
	// pool.Close would otherwise close the pool under one of them.
	jobCtx, stopJobs := context.WithCancel(ctx)
	defer stopJobs()
	var jobs sync.WaitGroup
	jobs.Go(func() { auth.NewSweeper(logger, queries, cfg.sessionTTL).Run(jobCtx) })
	if cfg.pushEnabled {
		jobs.Go(func() { digest.Run(jobCtx) })
	}
	err = serve(ctx, logger, listener, handler)
	stopJobs()
	jobs.Wait()
	if activity != nil {
		activity.Wait()
	}
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
