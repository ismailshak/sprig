//go:build dev

package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

func init() {
	routeAccess["GET "+devSignInPath] = access{public: true}
	routeAccess["POST "+devSignInPath] = access{public: true}
}

var (
	homeID     = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	upstairsID = uuid.MustParse("00000000-0000-7000-8000-000000000002")
	ellieID    = uuid.MustParse("00000000-0000-7000-8000-000000000003")
	samID      = uuid.MustParse("00000000-0000-7000-8000-000000000004")
	robinID    = uuid.MustParse("00000000-0000-7000-8000-000000000005")
)

// devStack builds the handler with New over a transaction, and returns the
// resolver so a test can check what a cookie the handler set resolves to.
func devStack(t *testing.T) (http.Handler, *auth.Resolver) {
	t.Helper()

	ctx := t.Context()
	tx := pgtest.Tx(t, migrateSchema)
	seed := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO garden (id, name) VALUES ($1, 'Home'), ($2, 'Upstairs')", []any{homeID, upstairsID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone, created_at) VALUES ($1, 'Ellie', 'ellie', 'Europe/London', now() - interval '3 days')", []any{ellieID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone, created_at) VALUES ($1, 'Sam', 'sam', 'Europe/London', now() - interval '2 days')", []any{samID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone, created_at) VALUES ($1, 'Robin', 'robin', 'Europe/Lisbon', now() - interval '1 day')", []any{robinID}},
		{"INSERT INTO membership (garden_id, user_id, role, created_at, digest_hour) VALUES ($1, $2, 'owner', now() - interval '3 days', 8)", []any{homeID, ellieID}},
		{"INSERT INTO membership (garden_id, user_id, role, created_at, digest_hour) VALUES ($1, $2, 'member', now() - interval '2 days', 8)", []any{homeID, samID}},
		{"INSERT INTO membership (garden_id, user_id, role, created_at, expires_at, digest_hour) VALUES ($1, $2, 'sitter', now() - interval '1 day', now() - interval '1 hour', 8)", []any{upstairsID, samID}},
		{"INSERT INTO membership (garden_id, user_id, role, created_at, expires_at, digest_hour) VALUES ($1, $2, 'sitter', now() - interval '1 day', now() - interval '7 days', 8)", []any{upstairsID, robinID}},
	}
	for _, row := range seed {
		if _, err := tx.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}

	queries := store.New(tx)
	sessions := auth.NewSessions(queries, testTTL, auth.CookieSettings{Name: "__Host-sprig_session", Secure: true})
	resolver := auth.NewResolver(sessions, queries)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	passkeys, err := auth.NewPasskeys(queries, "localhost", "sprig", "http://localhost:8080", auth.CookieSettings{Name: "__Host-sprig_session", Secure: true})
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	return New(logger, sessions, passkeys, resolver, queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil), resolver
}

func postHandle(t *testing.T, handler http.Handler, handle string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"handle": {handle}}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, devSignInPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestDevSignIn_ThePageListsEveryUser(t *testing.T) {
	handler, _ := devStack(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, devSignInPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	for _, handle := range []string{"ellie", "sam", "robin"} {
		if !strings.Contains(rec.Body.String(), `value="`+handle+`"`) {
			t.Errorf("the page has no button for %s", handle)
		}
	}
}

func TestDevSignIn_AHandleStartsARealSession(t *testing.T) {
	handler, resolver := devStack(t)

	rec := postHandle(t, handler, "ellie", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("status = %d to %q, want %d to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_session")
	if cookie == nil || cookie.Value == "" {
		t.Fatal("no session cookie was set")
	}

	principal, err := resolver.Resolve(t.Context(), time.Now(), cookie.Value)
	if err != nil {
		t.Fatalf("the cookie's token did not resolve: %v", err)
	}
	if principal.User.Handle != "ellie" || principal.Garden.ID != homeID {
		t.Errorf("resolved to %s on %s, want ellie on Home", principal.User.Handle, principal.Garden.Name)
	}
	if !principal.Can(auth.MemberInvite) {
		t.Error("an owner's session came without the owner's capabilities")
	}
}

// Sam's live membership is Home. The Upstairs one is more recent but has
// ended, so it is not a garden the first request can pick.
func TestDevSignIn_TheSessionStartsOnTheOldestLiveMembership(t *testing.T) {
	handler, resolver := devStack(t)

	rec := postHandle(t, handler, "sam", nil)
	cookie := cookieNamed(t, rec, "__Host-sprig_session")
	if cookie == nil {
		t.Fatalf("status = %d and no cookie was set", rec.Code)
	}
	principal, err := resolver.Resolve(t.Context(), time.Now(), cookie.Value)
	if err != nil {
		t.Fatalf("the cookie's token did not resolve: %v", err)
	}
	if principal.Garden.ID != homeID {
		t.Errorf("sam's session is on %s, want Home", principal.Garden.Name)
	}
}

func TestDevSignIn_SwitchingUsersDeletesThePreviousSession(t *testing.T) {
	handler, resolver := devStack(t)

	first := cookieNamed(t, postHandle(t, handler, "ellie", nil), "__Host-sprig_session")
	if first == nil {
		t.Fatal("no session cookie was set for ellie")
	}
	second := cookieNamed(t, postHandle(t, handler, "sam", first), "__Host-sprig_session")
	if second == nil {
		t.Fatal("no session cookie was set for sam")
	}
	if first.Value == second.Value {
		t.Fatal("the second sign-in reused the first token")
	}

	if _, err := resolver.Resolve(t.Context(), time.Now(), first.Value); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("ellie's token still resolves: err = %v, want %v", err, auth.ErrNoSession)
	}
	principal, err := resolver.Resolve(t.Context(), time.Now(), second.Value)
	if err != nil {
		t.Fatalf("sam's token did not resolve: %v", err)
	}
	if principal.User.Handle != "sam" {
		t.Errorf("resolved to %s, want sam", principal.User.Handle)
	}
}

func TestDevSignIn_AUserWithNoGardenGetsASessionOnNoGarden(t *testing.T) {
	handler, resolver := devStack(t)

	rec := postHandle(t, handler, "robin", nil)

	cookie := cookieNamed(t, rec, "__Host-sprig_session")
	if rec.Code != http.StatusSeeOther || cookie == nil {
		t.Fatalf("status = %d and cookie = %v, want %d with a session cookie", rec.Code, cookie, http.StatusSeeOther)
	}
	principal, err := resolver.Resolve(t.Context(), time.Now(), cookie.Value)
	if err != nil {
		t.Fatalf("the cookie's token did not resolve: %v", err)
	}
	if principal.InGarden() || principal.User.Handle != "robin" {
		t.Errorf("resolved to %s on %q, want robin in no garden", principal.User.Handle, principal.Garden.Name)
	}
}

func TestDevSignIn_AnUnknownHandleIs404(t *testing.T) {
	handler, _ := devStack(t)

	cases := []struct {
		name   string
		handle string
		want   int
	}{
		{"a handle nobody has", "nobody", http.StatusNotFound},
		{"an empty handle", "", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := postHandle(t, handler, c.handle, nil)
			if rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
			if cookie := cookieNamed(t, rec, "__Host-sprig_session"); cookie != nil {
				t.Error("a refused sign-in set a cookie")
			}
		})
	}
}
