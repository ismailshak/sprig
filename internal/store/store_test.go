package store

import (
	"strings"
	"testing"
)

func TestOpen_AnUnreachableDatabaseFailsWithoutThePasswordInTheError(t *testing.T) {
	// Port 1 on loopback has nothing listening.
	pool, err := Open(t.Context(), "postgres://sprig:hunter2@127.0.0.1:1/sprig")
	if err == nil {
		pool.Close()
		t.Fatal("expected an error for a database nothing is listening on, got nil")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error carries the password: %v", err)
	}
}
