package http

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_HealthzOK(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body healthzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body was not JSON: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want %q", body.Status, "ok")
	}
	if body.GoVersion == "" {
		t.Error("body carried no go_version")
	}
	if rec.Header().Get(RequestIDHeader) == "" {
		t.Error("response carried no request id header")
	}
}

// The id reaches the request line only because New orders RequestID
// outermost and the caller wrapped its handler with NewContextHandler.
// Neither is visible from the other's package, so nothing else fails if one
// of them is undone.
func TestNew_RequestLineCarriesTheHeaderID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	handler := New(logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line was not JSON: %v", err)
	}

	header := rec.Header().Get(RequestIDHeader)
	if header == "" {
		t.Fatal("response carried no request id header")
	}
	if entry["request_id"] != header {
		t.Errorf("logged request_id = %v, want the header's %q", entry["request_id"], header)
	}
	if entry["pattern"] != "GET /healthz" {
		t.Errorf("pattern = %v, want %q", entry["pattern"], "GET /healthz")
	}
}

func TestNew_UnknownRouteNotFound(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(logger)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
