// Package pgtest creates and drops the Postgres databases the tests run
// against. The rule about which server a test may write to lives here rather
// than in every package that needs a database.
//
// Each test binary passes in the function that builds its schema, because the
// package that owns the migrations imports pgtest from its tests, so pgtest
// cannot import it back.
package pgtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApplySchema builds the schema in the template database that Fresh and
// Shared copy from. It runs once per test binary.
//
// CREATE DATABASE refuses a template another session is connected to, so an
// ApplySchema must close every connection it opened before returning.
type ApplySchema func(ctx context.Context, databaseURL string) error

// ServerURL returns the Postgres server every test database is created on,
// from SPRIG_DATABASE_URL. The test is skipped when the variable is unset.
func ServerURL(t *testing.T) *url.URL {
	t.Helper()

	base := os.Getenv("SPRIG_DATABASE_URL")
	if base == "" {
		t.Skip("SPRIG_DATABASE_URL is unset")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("SPRIG_DATABASE_URL is not a URL: %v", err)
	}
	// The tests create and drop databases, so they are only allowed to talk
	// to a server on this machine or in the compose stack.
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1", "db":
	default:
		t.Fatalf("refusing to run against %q: tests only talk to loopback or the compose database", parsed.Hostname())
	}
	return parsed
}

// Empty creates a database with no schema and drops it when the test ends.
func Empty(t *testing.T) string {
	t.Helper()
	return createForTest(t, "")
}

// Fresh creates a database with the schema applied and drops it when the test
// ends. It copies a template rather than running schema again, so each call
// costs one CREATE DATABASE instead of a full migration run.
func Fresh(t *testing.T, schema ApplySchema) string {
	t.Helper()
	return createForTest(t, templateDatabase(t, schema))
}

// Shared returns one database with the schema applied, created on the first
// call and reused by every later caller in the binary. Main drops it, because
// no single test owns it. A test that commits should use Fresh instead.
func Shared(t *testing.T, schema ApplySchema) string {
	t.Helper()

	base := ServerURL(t)
	template := templateDatabase(t, schema)
	sharedOnce.Do(func() {
		sharedName, sharedErr = create(context.Background(), base, "sprig_test_shared_", template)
	})
	if sharedErr != nil {
		t.Fatalf("preparing the shared database: %v", sharedErr)
	}
	return databaseURL(base, sharedName)
}

// SharedPool returns a pool on the Shared database. Main closes it before
// dropping the database.
func SharedPool(t *testing.T, schema ApplySchema) *pgxpool.Pool {
	t.Helper()

	databaseURL := Shared(t, schema)
	poolOnce.Do(func() { sharedPool, poolErr = open(context.Background(), databaseURL) })
	if poolErr != nil {
		t.Fatalf("opening the shared database: %v", poolErr)
	}
	return sharedPool
}

// Tx begins a transaction on SharedPool and rolls it back when the test ends,
// so tests in a binary share one database without seeing each other's rows.
func Tx(t *testing.T, schema ApplySchema) pgx.Tx {
	t.Helper()

	tx, err := SharedPool(t, schema).Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the transaction: %v", err)
	}
	// The test's context is already cancelled when cleanup runs.
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	return tx
}

// Main runs the tests, then closes the shared pool and drops the template and
// shared databases, which no single test owns.
func Main(m *testing.M) {
	code := m.Run()
	if sharedPool != nil {
		sharedPool.Close()
	}
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

func cleanup() error {
	base := os.Getenv("SPRIG_DATABASE_URL")
	if base == "" {
		return nil
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("SPRIG_DATABASE_URL is not a URL: %w", err)
	}

	ctx := context.Background()
	for _, name := range []string{sharedName, templateName} {
		if name == "" {
			continue
		}
		if err := drop(ctx, parsed, name); err != nil {
			return err
		}
	}
	return nil
}

var (
	templateOnce sync.Once
	templateName string
	templateErr  error

	sharedOnce sync.Once
	sharedName string
	sharedErr  error

	poolOnce   sync.Once
	sharedPool *pgxpool.Pool
	poolErr    error
)

func open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func templateDatabase(t *testing.T, schema ApplySchema) string {
	t.Helper()

	base := ServerURL(t)
	templateOnce.Do(func() { templateErr = prepareTemplate(base, schema) })
	if templateErr != nil {
		t.Fatalf("preparing the template database: %v", templateErr)
	}
	return templateName
}

func prepareTemplate(base *url.URL, schema ApplySchema) error {
	ctx := context.Background()

	name, err := create(ctx, base, "sprig_test_tmpl_", "")
	if err != nil {
		return err
	}
	// Recorded before the schema is built, so cleanup still drops the database
	// if schema fails.
	templateName = name

	if err := schema(ctx, databaseURL(base, name)); err != nil {
		return fmt.Errorf("build the schema in %s: %w", name, err)
	}
	return nil
}

func createForTest(t *testing.T, template string) string {
	t.Helper()

	base := ServerURL(t)
	name, err := create(t.Context(), base, "sprig_test_", template)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// The test's context is already cancelled when cleanup runs.
		if err := drop(context.WithoutCancel(t.Context()), base, name); err != nil {
			t.Error(err)
		}
	})
	return databaseURL(base, name)
}

func create(ctx context.Context, base *url.URL, prefix, template string) (string, error) {
	name := prefix + strings.ToLower(rand.Text()[:12])

	sql := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if template != "" {
		sql += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	}

	server, err := pgx.Connect(ctx, base.String())
	if err != nil {
		return "", fmt.Errorf("connecting to %s: %w", base.Redacted(), err)
	}
	defer func() { _ = server.Close(ctx) }()

	if _, err := server.Exec(ctx, sql); err != nil {
		return "", fmt.Errorf("creating %s: %w", name, err)
	}
	return name, nil
}

func drop(ctx context.Context, base *url.URL, name string) error {
	server, err := pgx.Connect(ctx, base.String())
	if err != nil {
		return fmt.Errorf("connecting to drop %s: %w", name, err)
	}
	defer func() { _ = server.Close(ctx) }()

	// FORCE disconnects any pool that failed mid-test and still holds a connection.
	if _, err := server.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("dropping %s: %w", name, err)
	}
	return nil
}

func databaseURL(base *url.URL, name string) string {
	dbURL := *base
	dbURL.Path = "/" + name
	return dbURL.String()
}
