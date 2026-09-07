package http

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/photo"
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

// testPhotos returns a photo store over a directory that is removed when the
// test ends.
func testPhotos(t *testing.T) *photo.Store {
	t.Helper()

	photos, err := photo.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return photos
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
