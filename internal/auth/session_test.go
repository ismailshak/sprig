package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSessionToken_Is256BitsOfBase64urlAndTwoCallsDiffer(t *testing.T) {
	first := NewSessionToken()
	second := NewSessionToken()

	if first == second {
		t.Fatalf("two tokens came out the same: %q", first)
	}
	// 32 bytes in base64url without padding.
	if len(first) != 43 {
		t.Errorf("token is %d characters, want 43", len(first))
	}
	if err := (&http.Cookie{Name: "s", Value: first}).Valid(); err != nil {
		t.Errorf("token is not a valid cookie value: %v", err)
	}
	if HashToken(first) == first || len(HashToken(first)) != 64 {
		t.Errorf("HashToken(%q) = %q, want 64 hex characters that are not the token", first, HashToken(first))
	}
}

func TestSessionExpired_CountsFromLastSeenAndTheDeadlineInstantIsExpired(t *testing.T) {
	const ttl = 30 * 24 * time.Hour
	lastSeen := time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"the same instant", lastSeen, false},
		{"a fortnight on", lastSeen.AddDate(0, 0, 14), false},
		{"a second before the deadline", lastSeen.Add(ttl - time.Second), false},
		{"the deadline itself", lastSeen.Add(ttl), true},
		{"a day after", lastSeen.Add(ttl + 24*time.Hour), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SessionExpired(lastSeen, c.now, ttl); got != c.want {
				t.Errorf("SessionExpired(lastSeen, %s, ttl) = %v, want %v", c.now, got, c.want)
			}
		})
	}
}

func TestCookie_HasThePathHttpOnlySameSiteAndMaxAgeAttributes(t *testing.T) {
	sessions := NewSessions(nil, 720*time.Hour, CookieSettings{Name: "__Host-sprig_session", Secure: true})

	cookie := sessions.Cookie("tok")
	if cookie.Name != "__Host-sprig_session" || cookie.Value != "tok" {
		t.Errorf("cookie = %s=%s, want __Host-sprig_session=tok", cookie.Name, cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Error("cookie is readable from script")
	}
	if !cookie.Secure {
		t.Error("cookie is not Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
	if cookie.Domain != "" {
		t.Errorf("Domain = %q, and __Host- forbids one", cookie.Domain)
	}
	if err := cookie.Valid(); err != nil {
		t.Errorf("the cookie would not be sent: %v", err)
	}
}

func TestCookie_MaxAgeMatchesTheSessionTTL(t *testing.T) {
	// Max-Age is whole seconds, so the fraction must be dropped.
	ttl := 720*time.Hour + 750*time.Millisecond
	sessions := NewSessions(nil, ttl, CookieSettings{Name: "__Host-sprig_session", Secure: true})
	lastSeen := time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC)

	cookie := sessions.Cookie("tok")
	browserDrops := lastSeen.Add(time.Duration(cookie.MaxAge) * time.Second)
	if !SessionExpired(lastSeen, browserDrops, sessions.ttl) {
		t.Errorf("the browser drops the cookie at %s and the server still accepts the row", browserDrops)
	}
	if SessionExpired(lastSeen, browserDrops.Add(-time.Second), sessions.ttl) {
		t.Errorf("the server refuses the row a second before the browser drops the cookie")
	}
}

func TestCookie_SecureFollowsTheSetting(t *testing.T) {
	sessions := NewSessions(nil, time.Hour, CookieSettings{Name: "sprig_session", Secure: false})
	if sessions.Cookie("tok").Secure {
		t.Error("cookie is Secure with the setting off, so a browser on plain http would drop it")
	}
}

func TestClearedCookie_HasTheSameNamePathAndFlagsAsTheSessionCookie(t *testing.T) {
	sessions := NewSessions(nil, time.Hour, CookieSettings{Name: "__Host-sprig_session", Secure: true})

	issued := sessions.Cookie("tok")
	cleared := sessions.ClearedCookie()
	if cleared.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative so the browser deletes it", cleared.MaxAge)
	}
	if cleared.Value != "" {
		t.Errorf("a cleared cookie still carries %q", cleared.Value)
	}
	if cleared.Name != issued.Name || cleared.Path != issued.Path || cleared.Secure != issued.Secure {
		t.Errorf("cleared cookie %s does not match the issued %s", cleared, issued)
	}
}

func TestTokenFromRequest_ReadsOnlyTheNamedCookie(t *testing.T) {
	sessions := NewSessions(nil, time.Hour, CookieSettings{Name: "__Host-sprig_session", Secure: true})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if got := sessions.TokenFromRequest(req); got != "" {
		t.Errorf("a request with no cookie yielded %q", got)
	}

	req.AddCookie(&http.Cookie{Name: "other", Value: "nope"})
	req.AddCookie(&http.Cookie{Name: "__Host-sprig_session", Value: "tok"})
	if got := sessions.TokenFromRequest(req); got != "tok" {
		t.Errorf("TokenFromRequest = %q, want tok", got)
	}
}

func TestCookieSettings_Validate(t *testing.T) {
	cases := []struct {
		name     string
		settings CookieSettings
		wantErr  string
	}{
		{"the default", CookieSettings{Name: "__Host-sprig_session", Secure: true}, ""},
		{"a plain name over http", CookieSettings{Name: "sprig_session", Secure: false}, ""},
		{"__Host- over http", CookieSettings{Name: "__Host-sprig_session", Secure: false}, "Secure"},
		{"__Secure- over http", CookieSettings{Name: "__Secure-sprig", Secure: false}, "Secure"},
		{"no name", CookieSettings{Name: "", Secure: true}, "empty"},
		{"a name a browser cannot parse", CookieSettings{Name: "sprig session", Secure: true}, "invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.settings.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("Validate() = %v, want an error mentioning %q", err, c.wantErr)
			}
		})
	}
}
