package main

import (
	"bytes"
	"context"
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

func TestRun_ShutsDownGracefullyOnContextCancel(t *testing.T) {
	env := map[string]string{
		"SPRIG_ADDR":         "127.0.0.1:0",
		"SPRIG_DATABASE_URL": "postgres://example/db",
	}
	getenv := func(k string) string { return env[k] }

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done: exercises shutdown without depending on goroutine timing

	var stdout bytes.Buffer
	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, getenv, &stdout) }()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned an error on graceful shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return within 2s of an already-cancelled context")
	}
}
