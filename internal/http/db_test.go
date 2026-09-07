package http

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

func migrateSchema(ctx context.Context, databaseURL string) error {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	// CREATE DATABASE refuses a template another session is connected to.
	defer pool.Close()

	return store.Migrate(ctx, pool, db.Migrations, slog.New(slog.DiscardHandler))
}

// countRows returns the number of rows in table. It counts across every
// account and garden, so a test can check that a request wrote nothing outside
// the rows it names.
func countRows(t *testing.T, tx pgx.Tx, table string) int {
	t.Helper()

	var n int
	if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}
