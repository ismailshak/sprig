package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Dockerfile and mise run build pass no tag, so a plain go build is the
// binary that ships. Reading the artifact is the check that survives whatever
// the route table does under a tag.
func TestBuild_ProductionBinaryHasNoDevelopmentSignIn(t *testing.T) {
	const route = "/dev/signin"

	production := buildBinary(t)
	if bytes.Contains(production, []byte(route)) {
		t.Errorf("the production binary contains %s", route)
	}

	// The development build is the control, so a renamed route fails here
	// rather than making the assertion above vacuous.
	development := buildBinary(t, "-tags", "dev")
	if !bytes.Contains(development, []byte(route)) {
		t.Fatalf("the development binary does not contain %s, so the route this test looks for is stale", route)
	}
}

func buildBinary(t *testing.T, flags ...string) []byte {
	t.Helper()

	out := filepath.Join(t.TempDir(), "sprig")
	args := append([]string{"build", "-o", out}, flags...)
	args = append(args, ".")
	cmd := exec.CommandContext(t.Context(), "go", args...) //nolint:gosec // G204 sees a variable, and it holds the literals above
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, output)
	}

	binary, err := os.ReadFile(out) //nolint:gosec // G304 sees a variable, and it is a path under t.TempDir()
	if err != nil {
		t.Fatalf("reading the built binary: %v", err)
	}
	return binary
}
