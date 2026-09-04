package http

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

var epoch = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

func TestLimiter_ARefusedAttemptSpendsNothing(t *testing.T) {
	l := NewLimiter(rate.Every(time.Minute), 2)

	for i := range 2 {
		if ok, _ := l.Allow("k", epoch); !ok {
			t.Fatalf("attempt %d refused inside the burst", i+1)
		}
	}
	ok, retryAfter := l.Allow("k", epoch)
	if ok {
		t.Fatal("third attempt allowed with a burst of two")
	}
	if retryAfter != time.Minute {
		t.Errorf("retry after %s, want the minute one token takes", retryAfter)
	}

	// A refusal left reserved rather than cancelled would have claimed the
	// token that arrives at the minute.
	if ok, _ := l.Allow("k", epoch.Add(time.Minute)); !ok {
		t.Error("the token that refilled at the minute was not there to spend")
	}
	if ok, _ := l.Allow("k", epoch.Add(time.Minute)); ok {
		t.Error("a second token was allowed when only one had refilled")
	}
}

func TestLimiter_SweepsBucketsNobodyHasUsedForARefill(t *testing.T) {
	l := NewLimiter(rate.Every(time.Second), 10)

	for i := range 1000 {
		l.Allow("stranger-"+strconv.Itoa(i), epoch)
	}
	if got := len(l.buckets); got != 1000 {
		t.Fatalf("held %d buckets after 1000 distinct keys", got)
	}

	// The call at 10 seconds triggers the sweep. The call at 9 keeps the
	// display's bucket younger than a refill.
	l.Allow("display", epoch.Add(9*time.Second))
	l.Allow("display", epoch.Add(10*time.Second))
	if got := len(l.buckets); got != 1 {
		t.Errorf("held %d buckets after the sweep, want only the busy one", got)
	}
}

func TestLimit_OverTheLimitIsA429WithRetryAfter(t *testing.T) {
	l := NewLimiter(rate.Every(time.Minute), 1)
	handler := Limit(l, AnySource)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first request got %d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request got %d, want 429", rec.Code)
	}
	seconds, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("Retry-After %q is not a number of seconds", rec.Header().Get("Retry-After"))
	}
	if seconds < 1 || seconds > 60 {
		t.Errorf("Retry-After = %d, want between 1 and 60 for a one-a-minute bucket", seconds)
	}
}

func TestLimit_ByTokenGivesTwoDisplaysOnOneAddressTheirOwnBuckets(t *testing.T) {
	l := NewLimiter(rate.Every(time.Minute), 1)
	handler := Limit(l, ByToken)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	poll := func(token string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/chores", nil)
		req.RemoteAddr = "203.0.113.7:4444"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := poll("kitchen"); got != http.StatusNoContent {
		t.Fatalf("the kitchen display's first poll got %d", got)
	}
	if got := poll("kitchen"); got != http.StatusTooManyRequests {
		t.Fatalf("the kitchen display's second poll got %d, want 429", got)
	}
	if got := poll("hallway"); got != http.StatusNoContent {
		t.Errorf("the hallway display got %d from the same address, so it shared the kitchen's bucket", got)
	}
}

func TestLimit_ASharedBudgetIsRefusedWhateverTheAddress(t *testing.T) {
	perAddress := NewLimiter(rate.Every(time.Minute), 100)
	shared := NewLimiter(rate.Every(time.Minute), 3)
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler = Limit(shared, AnySource)(handler)
	handler = Limit(perAddress, ClientAddress(""))(handler)

	attempt := func(addr string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/recover", nil)
		req.RemoteAddr = addr + ":1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for i, addr := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		if got := attempt(addr); got != http.StatusNoContent {
			t.Fatalf("attempt %d from %s got %d inside the shared budget", i+1, addr, got)
		}
	}
	if got := attempt("198.51.100.4"); got != http.StatusTooManyRequests {
		t.Errorf("a fourth address got %d once the shared budget was spent, want 429", got)
	}
}

func TestClientAddress_UnsetIgnoresTheHeader(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:5555"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")

	if got := ClientAddress("")(req); got != "10.0.0.5" {
		t.Errorf("with no trusted header the key was %q, want the RemoteAddr host", got)
	}
}

func TestClientAddress_SetTrustsTheHeader(t *testing.T) {
	key := ClientAddress("CF-Connecting-IP")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:5555"
	req.Header.Set("CF-Connecting-IP", " 203.0.113.9 ")
	if got := key(req); got != "203.0.113.9" {
		t.Errorf("key = %q, want the header's value", got)
	}

	// A request arriving without the header did not come through the proxy,
	// so RemoteAddr is the only address it has.
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:5555"
	if got := key(req); got != "10.0.0.5" {
		t.Errorf("key without the header = %q, want the RemoteAddr host", got)
	}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{"Bearer sprg_7c1f", "sprg_7c1f"},
		{"bearer sprg_7c1f", "sprg_7c1f"},
		{"Basic c3ByaWc=", ""},
		{"Bearer", ""},
		{"", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		if got := bearerToken(req); got != c.want {
			t.Errorf("bearerToken(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}
