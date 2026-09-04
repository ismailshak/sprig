package store

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/pgtest"
)

// migrateSchema builds the template every test database in this package is
// copied from.
func migrateSchema(ctx context.Context, databaseURL string) error {
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	// CREATE DATABASE refuses a template another session is connected to.
	defer pool.Close()

	return Migrate(ctx, pool, db.Migrations, slog.New(slog.DiscardHandler))
}

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

func TestMigrate_FreshDatabaseAppliesEveryMigrationOnce(t *testing.T) {
	ctx := t.Context()
	pool := openPool(t, pgtest.Empty(t))

	logger, applied := recordingLogger()
	if err := Migrate(ctx, pool, os.DirFS("testdata/migrations"), logger); err != nil {
		t.Fatalf("Migrate on a fresh database returned an error: %v", err)
	}
	if got := applied(); got != 2 {
		t.Errorf("logged %d applied migrations, want 2", got)
	}
	if got := schemaVersion(t, pool); got != 2 {
		t.Errorf("schema version = %d, want 2", got)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO widget (id, name) VALUES (1, 'pot')"); err != nil {
		t.Errorf("the migrated schema did not accept a row: %v", err)
	}

	logger, applied = recordingLogger()
	if err := Migrate(ctx, pool, os.DirFS("testdata/migrations"), logger); err != nil {
		t.Fatalf("Migrate on a current database returned an error: %v", err)
	}
	if got := applied(); got != 0 {
		t.Errorf("a second run applied %d migrations, want 0", got)
	}
}

func TestMigrate_ConcurrentStartsDoNotRunTheSameMigrationTwice(t *testing.T) {
	ctx := t.Context()
	databaseURL := pgtest.Empty(t)
	pools := make([]*pgxpool.Pool, 4)
	for i := range pools {
		pools[i] = openPool(t, databaseURL)
	}

	// Apply the first migration on its own, so the race is over the second one
	// rather than over the version table goose creates first.
	logger, _ := recordingLogger()
	if err := Migrate(ctx, pools[0], firstMigrationOnly(t), logger); err != nil {
		t.Fatalf("applying the first migration: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, len(pools))
	counts := make([]int, len(pools))
	for i, pool := range pools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger, applied := recordingLogger()
			<-start
			errs[i] = Migrate(ctx, pool, os.DirFS("testdata/migrations"), logger)
			counts[i] = applied()
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("process %d returned an error: %v", i, err)
		}
	}
	total := 0
	for _, count := range counts {
		total += count
	}
	if total != 1 {
		t.Errorf("the processes applied the pending migration %d times, want once", total)
	}

	// Version 0 is goose's own row, written when it creates the version table.
	var rows int
	if err := pools[0].QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&rows); err != nil {
		t.Fatalf("counting the version rows: %v", err)
	}
	if rows != 2 {
		t.Errorf("goose_db_version holds %d migration rows, want one each", rows)
	}
}

func TestMigrate_FailedMigrationLeavesTheRestUnapplied(t *testing.T) {
	ctx := t.Context()
	pool := openPool(t, pgtest.Empty(t))

	logger, applied := recordingLogger()
	err := Migrate(ctx, pool, os.DirFS("testdata/broken"), logger)
	if err == nil {
		t.Fatal("expected an error from a migration Postgres cannot parse, got nil")
	}
	if got := applied(); got != 1 {
		t.Errorf("logged %d applied migrations, want the one that ran before the failure", got)
	}
	if got := schemaVersion(t, pool); got != 1 {
		t.Errorf("schema version = %d, want 1", got)
	}

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('gadget') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("looking for the failed migration's table: %v", err)
	}
	if exists {
		t.Error("the failed migration left its table behind")
	}
}

func TestMigrate_EmptySetIsNotAFailure(t *testing.T) {
	pool := openPool(t, pgtest.Empty(t))

	logger, applied := recordingLogger()
	if err := Migrate(t.Context(), pool, os.DirFS(t.TempDir()), logger); err != nil {
		t.Fatalf("Migrate with no migrations returned an error: %v", err)
	}
	if got := applied(); got != 0 {
		t.Errorf("logged %d applied migrations, want 0", got)
	}
}

// firstMigrationOnly is testdata/migrations with the second migration held back.
func firstMigrationOnly(t *testing.T) fs.FS {
	t.Helper()

	const name = "00001_create_widget.sql"
	data, err := os.ReadFile(filepath.Join("testdata", "migrations", name))
	if err != nil {
		t.Fatalf("reading the first migration: %v", err)
	}
	return fstest.MapFS{name: &fstest.MapFile{Data: data}}
}

func schemaVersion(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var version int64
	if err := pool.QueryRow(t.Context(), "SELECT max(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	return version
}

// recordingLogger returns a logger and a count of the migrations it has been
// told were applied.
func recordingLogger() (*slog.Logger, func() int) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	return logger, func() int {
		return strings.Count(buf.String(), `"msg":"migration applied"`)
	}
}

func openPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()

	pool, err := Open(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestMigrate_MigrationNumberedBehindAnAppliedOneIsRefused(t *testing.T) {
	ctx := t.Context()
	pool := openPool(t, pgtest.Empty(t))

	logger, _ := recordingLogger()
	if err := Migrate(ctx, pool, numberedMigrations("00001_first", "00003_third"), logger); err != nil {
		t.Fatalf("applying the first set: %v", err)
	}

	// 00002 lands from another branch after 00003 has already run.
	err := Migrate(ctx, pool, numberedMigrations("00001_first", "00002_second", "00003_third"), logger)
	if err == nil {
		t.Fatal("expected an error for a migration numbered behind an applied one, got nil")
	}
	if got := schemaVersion(t, pool); got != 3 {
		t.Errorf("schema version = %d, want the version the refusal left in place", got)
	}
}

// numberedMigrations builds a migration set where each one creates a table named
// after its own file.
func numberedMigrations(names ...string) fs.FS {
	fsys := fstest.MapFS{}
	for _, name := range names {
		body := fmt.Sprintf("-- +goose Up\nCREATE TABLE %q (id integer PRIMARY KEY);\n", name)
		fsys[name+".sql"] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}
