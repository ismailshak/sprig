package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// goose polls every five seconds by default, which is five seconds a restart
// spends waiting on a lock that has already been released.
const (
	lockPollSeconds  = 1
	lockPollAttempts = 300
)

// Migrate applies every outstanding migration in migrations. It holds a Postgres
// advisory lock for the run, so two processes starting together cannot apply the
// same migration twice.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS, logger *slog.Logger) error {
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(lockPollSeconds, lockPollAttempts))
	if err != nil {
		return fmt.Errorf("build the migration locker: %w", err)
	}

	// goose speaks database/sql, so the run borrows the pool through pgx's adapter.
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close() //nolint:errcheck // the pool outlives it and owns the connections

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		if errors.Is(err, goose.ErrNoMigrations) {
			logger.WarnContext(ctx, "no migrations to apply")
			return nil
		}
		return fmt.Errorf("read the migrations: %w", err)
	}

	results, err := provider.Up(ctx)
	var partial *goose.PartialError
	if errors.As(err, &partial) {
		results = partial.Applied
	}
	for _, result := range results {
		logger.InfoContext(ctx, "migration applied",
			"version", result.Source.Version,
			"file", result.Source.Path,
			"duration", result.Duration,
		)
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read the schema version: %w", err)
	}
	logger.InfoContext(ctx, "schema is current", "version", version, "applied", len(results))

	return nil
}
