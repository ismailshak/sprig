package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/push"
)

func TestRun_MissingRequiredConfigReturnsError(t *testing.T) {
	getenv := func(string) string { return "" }
	var stdout bytes.Buffer

	err := run(context.Background(), getenv, &stdout)
	if err == nil {
		t.Fatal("expected an error for a missing SPRIG_DATABASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "SPRIG_DATABASE_URL") {
		t.Errorf("error %q did not name the missing variable", err.Error())
	}
}

func TestRun_UnreachableDatabaseStopsBeforeListening(t *testing.T) {
	ctx := t.Context()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the port: %v", err)
	}

	env := map[string]string{
		"SPRIG_ADDR": addr,
		// Port 1 on loopback has nothing listening.
		"SPRIG_DATABASE_URL": "postgres://sprig@127.0.0.1:1/sprig",
	}
	getenv := func(k string) string { return env[k] }

	var stdout bytes.Buffer
	if err := run(ctx, getenv, &stdout); err == nil {
		t.Fatal("expected an error for a database nothing is listening on, got nil")
	}

	// The process must not have started listening.
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err == nil {
		_ = conn.Close()
		t.Errorf("something is listening on %s", addr)
	}
}

func TestServe_ShutsDownGracefullyOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}

	cancel() // already done: exercises shutdown without depending on goroutine timing

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, logger, listener, http.NotFoundHandler()) }()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serve returned an error on graceful shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return within 2s of an already-cancelled context")
	}
}

func TestSubcommand_VapidPrintsAPairAsTheTwoVariables(t *testing.T) {
	var stdout bytes.Buffer

	if err := subcommand(t.Context(), []string{"vapid"}, noEnv, &stdout); err != nil {
		t.Fatalf("vapid: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "SPRIG_VAPID_PUBLIC_KEY=") || !strings.HasPrefix(lines[1], "SPRIG_VAPID_PRIVATE_KEY=") {
		t.Fatalf("vapid printed:\n%s\nwant the two variables, one a line", stdout.String())
	}
	keys := push.Keys{
		Public:  strings.TrimPrefix(lines[0], "SPRIG_VAPID_PUBLIC_KEY="),
		Private: strings.TrimPrefix(lines[1], "SPRIG_VAPID_PRIVATE_KEY="),
		Subject: "mailto:sprig@example.com",
	}
	if err := keys.Validate(); err != nil {
		t.Errorf("the printed pair does not validate: %v", err)
	}
}

func TestSubcommand_AnUnknownCommandIsRefused(t *testing.T) {
	var stdout bytes.Buffer

	if err := subcommand(t.Context(), []string{"serve"}, noEnv, &stdout); err == nil {
		t.Error("an unknown command ran without an error")
	}
}

func TestSubcommand_SweepWithoutADatabaseURLNamesTheMissingVariable(t *testing.T) {
	var stdout bytes.Buffer

	err := subcommand(t.Context(), []string{"sweep"}, noEnv, &stdout)

	if err == nil || !strings.Contains(err.Error(), "SPRIG_DATABASE_URL") {
		t.Errorf("err = %v, want one naming SPRIG_DATABASE_URL", err)
	}
}

func TestSubcommand_SweepWithAMissingPhotoDirectoryNamesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "photos")
	env := map[string]string{
		"SPRIG_DATABASE_URL": "postgres://example/db",
		"SPRIG_BASE_URL":     "https://sprig.example.com",
		"SPRIG_PHOTO_DIR":    dir,
	}
	var stdout bytes.Buffer

	err := subcommand(t.Context(), []string{"sweep"}, func(k string) string { return env[k] }, &stdout)

	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("err = %v, want one naming %s", err, dir)
	}
}

func TestSubcommand_SweepDeletesAnExpiredSessionAndAnOldPhotoFileNoRowPointsAt(t *testing.T) {
	d := adminFixture(t)
	ctx := t.Context()

	// The default session lifetime is 30 days.
	if _, err := d.pool.Exec(ctx, `INSERT INTO session (token_hash, user_id, last_seen_at)
		VALUES ('expired', $1, now() - interval '31 days'), ('live', $1, now())`, adminUserID); err != nil {
		t.Fatalf("seeding the sessions: %v", err)
	}

	// The sweep deletes a file no row points at only when it is under a garden
	// and plant directory and over an hour old.
	dir := t.TempDir()
	orphan := filepath.Join(dir, adminGardenID.String(), orphanPlantID.String(), "photo.jpg")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	written := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphan, written, written); err != nil {
		t.Fatal(err)
	}
	d.env["SPRIG_PHOTO_DIR"] = dir

	var stdout bytes.Buffer
	if err := subcommand(ctx, []string{"sweep"}, d.getenv, &stdout); err != nil {
		t.Fatalf("sweep: %v\n%s", err, stdout.String())
	}

	rows, err := d.pool.Query(ctx, "SELECT token_hash FROM session")
	if err != nil {
		t.Fatalf("reading the sessions: %v", err)
	}
	tokens, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading the sessions: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "live" {
		t.Errorf("the sessions left are %v, want only the live one", tokens)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat of the orphan file = %v, want it deleted", err)
	}
}

// orphanPlantID names the plant directory of a photo file no row points at.
var orphanPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000403")

func noEnv(string) string { return "" }
