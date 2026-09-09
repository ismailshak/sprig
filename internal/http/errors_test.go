package http

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// browsing sets the Accept header a browser sends when it loads a page.
func browsing(r *http.Request) *http.Request {
	r.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	return r
}

func TestNotFound_ABrowserOnAnUnknownPathGetsThePageNotFoundPage(t *testing.T) {
	app := New(testLogger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(everyCapability())), noLiveToken, nil,
		testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, browsing(signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	body := rec.Body.String()
	for _, want := range []string{"<title>" + notFoundTitle + " · sprig</title>", notFoundTitle, notFoundLine, `href="` + todayPath + `">Back to Today`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
		}
	}
}

func TestNotFound_ARequestThatIsNotABrowserNavigationReadsOneLineOfText(t *testing.T) {
	app := New(testLogger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(everyCapability())), noLiveToken, nil,
		testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)
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

func TestBadRequest_ABrowserPostingAFormThatCannotBeParsedReadsTheErrorPage(t *testing.T) {
	app := New(testLogger, testSessions(), testPasskeys(), acceptEveryToken(memberWith(everyCapability())), noLiveToken, nil,
		testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)
	// %zz is not a percent-encoding, so ParseForm refuses the body.
	req := browsing(signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodPost, accountPath, strings.NewReader("display_name=%zz"))))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	body := rec.Body.String()
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
	for _, want := range []string{"<title>" + serverErrorTitle + " · sprig</title>", serverErrorLine, "Back to Today"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q:\n%s", want, body)
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
