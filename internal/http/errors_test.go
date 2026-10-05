package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// browsing sets the Accept header a browser sends when it loads a page.
func browsing(r *http.Request) *http.Request {
	r.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	return r
}

func TestNotFound_ABrowserOnAnUnknownPathGetsThePageNotFoundPage(t *testing.T) {
	deps := testDependencies(t)
	deps.Resolver = acceptEveryToken(memberWith(everyCapability()))
	app := New(deps)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, browsing(signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	page := readHTML(rec.Body.String())
	if got, want := page.first(isTag("title")).text(), notFoundTitle+" · sprig"; got != want {
		t.Errorf("the title is %q, want %q", got, want)
	}
	for _, want := range []string{notFoundTitle, notFoundLine} {
		if !strings.Contains(page.first(isTag("body")).text(), want) {
			t.Errorf("the page lacks %q:\n%s", want, page.text())
		}
	}
	if page.first(isTag("a"), attrIs("href", todayPath), textIs("Back to Today")) == nil {
		t.Errorf("the page has no Back to Today link to %s:\n%s", todayPath, rec.Body.String())
	}
}

// errorPageText is the text of the page body, without the title in the head.
func errorPageText(body string) string {
	return readHTML(body).first(isTag("body")).text()
}

func TestNotFound_ARequestThatIsNotABrowserNavigationReadsOneLineOfText(t *testing.T) {
	deps := testDependencies(t)
	deps.Resolver = acceptEveryToken(memberWith(everyCapability()))
	app := New(deps)
	for name, prepare := range map[string]func(*http.Request){
		"no Accept header": func(*http.Request) {},
		"htmx": func(r *http.Request) {
			r.Header.Set("Accept", "*/*")
			r.Header.Set("HX-Request", "true")
		},
	} {
		req := signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))
		prepare(req)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusNotFound)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != notFoundText {
			t.Errorf("%s: body = %q, want %q", name, got, notFoundText)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("%s: Content-Type = %q, want text/plain", name, ct)
		}
	}
}

func TestNotFound_ThePageAndTheLineOfTextBothVaryOnAccept(t *testing.T) {
	deps := testDependencies(t)
	deps.Resolver = acceptEveryToken(memberWith(everyCapability()))
	app := New(deps)
	for name, prepare := range map[string]func(*http.Request){
		"page": func(r *http.Request) { browsing(r) },
		"text": func(r *http.Request) { r.Header.Set("Accept", "*/*") },
	} {
		req := signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))
		prepare(req)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		if got := rec.Header().Values("Vary"); !slices.Contains(got, "Accept") {
			t.Errorf("the %s's Vary is %q, want Accept", name, got)
		}
	}
}

func TestBadRequest_ABrowserPostingAFormThatCannotBeParsedReadsTheErrorPage(t *testing.T) {
	deps := testDependencies(t)
	deps.Resolver = acceptEveryToken(memberWith(everyCapability()))
	app := New(deps)
	// %zz is not a percent-encoding, so ParseForm refuses the body.
	req := browsing(signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodPost, accountPath, strings.NewReader("display_name=%zz"))))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	body := errorPageText(rec.Body.String())
	for _, want := range []string{badRequestTitle, badRequestLine, "Back to Today"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
}

func TestServerError_ABrowserReadsTheSomethingWentWrongPageAndTheErrorIsLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rec := httptest.NewRecorder()
	req := browsing(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants", nil))
	testTemplates().serverError(logger, rec, req, "list the plants", http.ErrHandlerTimeout)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	body := rec.Body.String()
	if got, want := readHTML(body).first(isTag("title")).text(), serverErrorTitle+" · sprig"; got != want {
		t.Errorf("the title is %q, want %q", got, want)
	}
	for _, want := range []string{serverErrorLine, "Back to Today"} {
		if !strings.Contains(errorPageText(body), want) {
			t.Errorf("the page lacks %q:\n%s", want, errorPageText(body))
		}
	}
	if strings.Contains(body, http.ErrHandlerTimeout.Error()) {
		t.Errorf("the page shows the error's text:\n%s", body)
	}
	if lines := strings.Split(strings.TrimSpace(buf.String()), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "list the plants") {
		t.Errorf("logged %d lines, want one naming the step: %v", len(lines), lines)
	}
}

func TestServerError_AnHTMXRequestGetsOneLineOfText(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/plants/x/log", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Accept", "*/*")
	testTemplates().serverError(testLogger, rec, req, "log the care", http.ErrHandlerTimeout)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != serverErrorText {
		t.Errorf("body = %q, want %q", got, serverErrorText)
	}
}

func TestServerError_ACancelledRequestWritesNothingAndLogsTheErrorAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := browsing(httptest.NewRequestWithContext(ctx, http.MethodGet, "/plants", nil))
	rec := httptest.NewRecorder()
	// The error does not wrap context.Canceled. A serverError that checks err
	// instead of the request's context fails this test.
	testTemplates().serverError(logger, rec, req, "resolve the session", errors.New("conn closed"))

	if rec.Code == http.StatusInternalServerError || rec.Body.Len() > 0 || len(rec.Header()) > 0 {
		t.Errorf("wrote status %d, headers %v and body %q, want nothing written", rec.Code, rec.Header(), rec.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("logged %d lines, want 1: %v", len(lines), lines)
	}
	var entry struct{ Level, Msg string }
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line %q is not JSON: %v", lines[0], err)
	}
	if entry.Level != slog.LevelInfo.String() || entry.Msg != "resolve the session" {
		t.Errorf("logged %s %q, want INFO \"resolve the session\"", entry.Level, entry.Msg)
	}
}
