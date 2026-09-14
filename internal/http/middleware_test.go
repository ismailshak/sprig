package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_SetsHeaderAndContext(t *testing.T) {
	var gotID string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	RequestID(next).ServeHTTP(rec, req)

	header := rec.Header().Get(RequestIDHeader)
	if header == "" {
		t.Fatal("response carried no request id header")
	}
	if gotID != header {
		t.Fatalf("context id %q did not match header id %q", gotID, header)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	handler := RequestID(next)

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	id1 := rec1.Header().Get(RequestIDHeader)
	id2 := rec2.Header().Get(RequestIDHeader)
	if id1 == id2 {
		t.Fatalf("two requests got the same id: %q", id1)
	}
}

func TestLogging_OneLineWithPatternAndStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /plants/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler := MatchPattern(mux)(Logging(logger)(mux))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants/42", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line was not JSON: %v", err)
	}
	if entry["pattern"] != "GET /plants/{id}" {
		t.Errorf("pattern = %v, want %q", entry["pattern"], "GET /plants/{id}")
	}
	if entry["method"] != http.MethodGet {
		t.Errorf("method = %v, want %q", entry["method"], http.MethodGet)
	}
	if entry["status"] != float64(http.StatusTeapot) {
		t.Errorf("status = %v, want %d", entry["status"], http.StatusTeapot)
	}
	if _, ok := entry["duration"]; !ok {
		t.Error("log line carried no duration")
	}
}

func TestLogging_AHealthCheckRequestIsLoggedAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mux := http.NewServeMux()
	mux.HandleFunc(healthzPattern, handleHealthz)

	handler := MatchPattern(mux)(Logging(logger)(mux))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log line was not JSON: %v", err)
	}
	if entry["level"] != "DEBUG" {
		t.Errorf("level = %v, want DEBUG", entry["level"])
	}
}

func TestRecover_ConvertsPanicToInternalServerError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	Recover(logger, testTemplates())(next).ServeHTTP(rec, browsing(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(text(rec.Body.String()), serverErrorTitle) {
		t.Errorf("the browser read %q, want the %q page", rec.Body.String(), serverErrorTitle)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("panic log line was not JSON: %v", err)
	}
	if entry["stack"] == "" || entry["stack"] == nil {
		t.Error("panic log line carried no stack")
	}
	if entry["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entry["level"])
	}
}

func TestNewContextHandler_AttachesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))

	ctx := context.WithValue(context.Background(), requestIDKey, "abc123")
	logger.InfoContext(ctx, "hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log line was not JSON: %v", err)
	}
	if entry["request_id"] != "abc123" {
		t.Errorf("request_id = %v, want %q", entry["request_id"], "abc123")
	}
}

func TestRecover_APanicAfterTheResponseStartedKeepsWhatTheHandlerWrote(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<p>half a page"))
		panic("boom")
	})

	rec := httptest.NewRecorder()
	Recover(slog.New(slog.DiscardHandler), testTemplates())(next).ServeHTTP(rec, browsing(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the %d the handler wrote", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "<p>half a page" {
		t.Errorf("body = %q, want only what the handler wrote", got)
	}
}
