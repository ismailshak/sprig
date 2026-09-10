package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"uuid"
)

func (f *setupFixture) suggestHandle(t *testing.T, name string) (int, string) {
	t.Helper()

	h := &handleSuggestions{queries: f.handler.queries, templates: f.handler.templates, logger: testLogger}
	rec := f.request(t, h.suggest, handlePath+"?name="+url.QueryEscape(name), nil)
	if rec.Code != http.StatusOK {
		return rec.Code, ""
	}
	var body struct{ Handle string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not JSON: %v:\n%s", err, rec.Body.String())
	}
	return rec.Code, body.Handle
}

func TestHandle_ADisplayNameNoAccountHoldsIsSuggestedAsItsSlug(t *testing.T) {
	f := setupOn(t, true)

	code, handle := f.suggestHandle(t, "Robin+Hood")

	if code != http.StatusOK || handle != "robin_hood" {
		t.Errorf("status %d, handle %q, want 200 and robin_hood", code, handle)
	}
}

func TestHandle_ADisplayNameAnotherAccountHoldsIsSuggestedWithASuffix(t *testing.T) {
	f := setupOn(t, true)
	if _, err := f.queries.CreateAccount(t.Context(), uuid.NewV7(), "Robin", "Europe/London"); err != nil {
		t.Fatalf("seeding an account: %v", err)
	}

	code, handle := f.suggestHandle(t, "Robin")

	if code != http.StatusOK || !regexp.MustCompile(`^robin_[a-z2-7]{4}$`).MatchString(handle) {
		t.Errorf("status %d, handle %q, want 200 and robin with a four character suffix", code, handle)
	}
}

func TestHandle_ADisplayNameOfSpacesGetsNoSuggestion(t *testing.T) {
	f := setupOn(t, true)

	code, _ := f.suggestHandle(t, "   ")

	if code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", code, http.StatusUnprocessableEntity)
	}
}

// The post makes the same handle for the same name when the field is left
// blank, so the suggestion shows what would be stored either way.
func TestHandle_ADisplayNameWithNoLetterOrDigitIsSuggestedAsGardener(t *testing.T) {
	f := setupOn(t, true)

	code, handle := f.suggestHandle(t, "+++")

	if code != http.StatusOK || handle != "gardener" {
		t.Errorf("status %d, handle %q, want 200 and gardener", code, handle)
	}
}

// handleMux serves the suggestion through the whole mux, so the route's rate
// limiters are in front of it.
func handleMux(t *testing.T, f *setupFixture) http.Handler {
	t.Helper()
	return New(testLogger, f.handler.sessions, f.handler.passkeys, rejectEveryToken, noLiveToken, f.queries, testPhotos(t), testTemplates(), testAssets(), "", true, testPushKey, nil, nil, nil, nil)
}

func TestHandle_TheThirtyFirstSuggestionInAMinuteFromOneAddressIsRefused(t *testing.T) {
	f := setupOn(t, true)
	handler := handleMux(t, f)
	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, handlePath+"?name=Robin", nil)
		req.RemoteAddr = "203.0.113.7:40000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for i := range 30 {
		if rec := get(); rec.Code != http.StatusOK {
			t.Fatalf("suggestion %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
	rec := get()

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the thirty-first suggestion: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("the body is %q, want none, because the page's script ignores anything but a 200", rec.Body.String())
	}
}
