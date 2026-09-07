package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func noEnv(string) string { return "" }
