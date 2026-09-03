package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
		// Port 1 on loopback, where nothing is listening and nothing ever will be.
		"SPRIG_DATABASE_URL": "postgres://sprig@127.0.0.1:1/sprig",
	}
	getenv := func(k string) string { return env[k] }

	var stdout bytes.Buffer
	if err := run(ctx, getenv, &stdout); err == nil {
		t.Fatal("expected an error for a database nothing is listening on, got nil")
	}

	// Nothing was served against a database the process never reached.
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
