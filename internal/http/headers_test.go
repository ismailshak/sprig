package http

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The headers come from middleware, so the route table is walked for the range
// of responses under it: pages, redirects to sign in, 404s and refused posts.
func TestSecurityHeaders_EveryRouteSetsAllFourHeaders(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	queries := routeQueries(t)
	stranger := New(logger, testSessions(), testPasskeys(), rejectEveryToken, noLiveToken, queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)
	owner := New(logger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(everyCapability())), noLiveToken, queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)

	for _, r := range routes(testLogger, testSessions(), testPasskeys(), nil, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil) {
		method, path := splitPattern(r.pattern)
		if a := routeAccess[r.pattern]; a.path != "" {
			path = a.path
		}
		if method == "" {
			method = http.MethodGet
		}
		t.Run(r.pattern, func(t *testing.T) {
			rec := httptest.NewRecorder()
			stranger.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
			checkSecurityHeaders(t, rec)

			// Only reads run as the owner. A post would change the seeded rows
			// the routes after it request, and the stranger's walk already
			// covers a refused post.
			if method == http.MethodGet {
				rec = httptest.NewRecorder()
				owner.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), method, path, nil)))
				checkSecurityHeaders(t, rec)
			}
		})
	}

	t.Run("a path no route matches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		owner.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		checkSecurityHeaders(t, rec)
	})
}

func TestSecurityHeaders_TheFiveHundredAfterAPanicHasTheHeaders(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("a handler that fell over") })
	rec := httptest.NewRecorder()
	SecurityHeaders(Recover(testLogger)(panicking)).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	checkSecurityHeaders(t, rec)
}

func TestContentSecurityPolicy_ScriptSrcIsSelfWithNoInlineAllowance(t *testing.T) {
	if got := policyDirectives(t)["script-src"]; got != "'self'" {
		t.Errorf("script-src = %q, want 'self' alone", got)
	}
}

func TestContentSecurityPolicy_FrameAncestorsIsNone(t *testing.T) {
	if got := policyDirectives(t)["frame-ancestors"]; got != "'none'" {
		t.Errorf("frame-ancestors = %q, want 'none'", got)
	}
}

func checkSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": contentSecurityPolicy,
		"Referrer-Policy":         "same-origin",
		"X-Content-Type-Options":  "nosniff",
		"Permissions-Policy":      permissionsPolicy,
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q on a %d, want %q", name, got, rec.Code, value)
		}
	}
}

// policyDirectives returns the Content-Security-Policy from a response through
// SecurityHeaders, as a map of directive name to value. It reads the header
// rather than the constant, so a widened directive fails the test that calls
// it.
func policyDirectives(t *testing.T) map[string]string {
	t.Helper()
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	directives := map[string]string{}
	for _, directive := range strings.Split(rec.Header().Get("Content-Security-Policy"), ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), " ")
		directives[name] = value
	}
	return directives
}
