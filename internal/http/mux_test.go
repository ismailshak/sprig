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
	handler := New(logger, testSessions(), testPasskeys(), rejectEveryToken, noLiveToken, nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)

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

// The id reaches the log line only because New puts RequestID outermost and the
// caller wrapped its handler with NewContextHandler. Neither is visible from
// the other's package, so nothing else fails if one of them is undone.
func TestNew_TheRequestLogLineIncludesTheRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewContextHandler(slog.NewJSONHandler(&buf, nil)))
	handler := New(logger, testSessions(), testPasskeys(), rejectEveryToken, noLiveToken, nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)

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

// A stranger cannot tell a path that exists from one that does not.
func TestNew_UnknownRouteIsSignInForAStrangerAndNotFoundForAMember(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(logger, testSessions(), testPasskeys(), acceptEveryToken(sitterPrincipal()), noLiveToken, nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signInPath {
		t.Errorf("without a cookie: status = %d to %q, want %d to %q", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, signInPath)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("with a session: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// GET is not checked because no GET route changes state.
func TestNew_RefusesAnUnsafeMethodFromAnotherOrigin(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := New(logger, testSessions(), testPasskeys(), rejectEveryToken, noLiveToken, nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)

	// The requests have no cookie, so an accepted one reaches the session
	// check and is redirected to sign in. A refused one is a 403 before that.
	cases := []struct {
		name   string
		method string
		host   string
		origin string
		site   string
		want   int
	}{
		{"a form post from elsewhere", http.MethodPost, "sprig.test", "https://evil.example", "", http.StatusForbidden},
		{"a browser that says cross-site", http.MethodPost, "sprig.test", "", "cross-site", http.StatusForbidden},
		{"a form post from this origin", http.MethodPost, "sprig.test", "http://sprig.test", "", http.StatusSeeOther},
		{"a browser that says same-origin", http.MethodPost, "sprig.test", "", "same-origin", http.StatusSeeOther},
		{"a post over plain http on a laptop", http.MethodPost, "localhost:8080", "http://localhost:8080", "same-origin", http.StatusSeeOther},
		{"a client that is not a browser", http.MethodDelete, "sprig.test", "", "", http.StatusSeeOther},
		{"a read from elsewhere", http.MethodGet, "sprig.test", "https://evil.example", "cross-site", http.StatusSeeOther},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), c.method, "/plants", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.site != "" {
				req.Header.Set("Sec-Fetch-Site", c.site)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

// A route flagged bearer that credentialsFor left on the session cookie would
// be an endpoint no device could reach.
func TestCredentialsFor_ABearerRouteTakesTheAPITokenAndEveryOtherRouteTheSessionCookie(t *testing.T) {
	table := []route{
		{pattern: "GET /healthz"},
		{pattern: "GET /api/chores", bearer: true},
		{pattern: "GET /plants"},
	}

	credentials := credentialsFor(table)

	if got := credentials["GET /healthz"]; got != noCredential {
		t.Errorf("the route in publicRoutes takes credential %d, want noCredential", got)
	}
	if got := credentials["GET /api/chores"]; got != apiToken {
		t.Errorf("the route flagged bearer takes credential %d, want apiToken", got)
	}
	if got := credentials["GET /plants"]; got != sessionCookie {
		t.Errorf("the route flagged neither takes credential %d, want the session cookie", got)
	}
}
