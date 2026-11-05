package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Open returns a pool for databaseURL. It pings once so that an unreachable
// database stops the process at startup rather than on the first request.
//
// Every query run inside a span gets a span of its own from provider, named by
// spanName. A nil provider records nothing. The span holds the statement's
// text and never its arguments, because the arguments include token hashes,
// push endpoints and people's names.
func Open(ctx context.Context, databaseURL string, provider trace.TracerProvider) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("configure the postgres pool: %w", err)
	}
	if provider == nil {
		provider = noop.NewTracerProvider()
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithTracerProvider(provider),
		otelpgx.WithSpanNameCtxFunc(spanName),
		otelpgx.WithDisableAcquireTracer(),
	)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("configure the postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("reach postgres: %w", err)
	}
	return pool, nil
}

// spanName returns the name of the sqlc query sql came from, read from the
// "-- name: GetPlant :one" line sqlc puts first in every generated statement.
// A statement without that line, such as one from a migration, is named by
// its first SQL keyword.
func spanName(ctx context.Context, sql string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(sql), "\n")
	if rest, ok := strings.CutPrefix(line, "-- name:"); ok {
		if fields := strings.Fields(rest); len(fields) > 0 {
			return fields[0]
		}
	}
	return otelpgx.SQLOperationName(ctx, sql)
}
