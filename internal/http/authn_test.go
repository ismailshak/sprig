package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	testTTL   = 30 * 24 * time.Hour
	testToken = "a-token-the-test-issued"
)

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// testSessions is a Sessions with no database. The middleware only asks it for
// the cookie's name and attributes, which need no query.
func testSessions() *auth.Sessions {
	return auth.NewSessions(nil, testTTL, auth.CookieSettings{Name: "__Host-sprig_session", Secure: true})
}

// testPasskeys is a Passkeys with no database behind it. The route table and
// the middleware only need it to exist. A test that runs a ceremony builds its
// own against a real Postgres.
func testPasskeys() *auth.Passkeys {
	passkeys, err := auth.NewPasskeys(nil, "localhost", "sprig", "http://localhost:8080", auth.CookieSettings{Name: "__Host-sprig_session", Secure: true})
	if err != nil {
		panic(err)
	}
	return passkeys
}

func signedIn(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{Name: "__Host-sprig_session", Value: testToken})
	return r
}

var rejectEveryToken = ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrNoSession
})

func acceptEveryToken(principal auth.Principal) Resolver {
	return ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
		return principal, nil
	})
}

func failEveryToken(err error) Resolver {
	return ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
		return auth.Principal{}, err
	})
}

func sitterPrincipal() auth.Principal {
	return auth.Principal{
		User:         store.AppUser{ID: uuid.MustParse("00000000-0000-7000-8000-000000000002"), DisplayName: "Ellie", Handle: "ellie", Timezone: "Pacific/Auckland"},
		Garden:       store.Garden{ID: uuid.MustParse("00000000-0000-7000-8000-000000000001"), Name: "Rosewood"},
		Membership:   store.Membership{Role: "sitter"},
		Capabilities: auth.Capabilities{auth.CareLog: true},
	}
}

func protected(t *testing.T, resolver Resolver) (http.Handler, *auth.Principal) {
	t.Helper()

	var seen auth.Principal
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /plants", func(w http.ResponseWriter, r *http.Request) {
		seen = PrincipalFrom(r)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /plants", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })

	isPublic := func(r *http.Request) bool {
		_, pattern := mux.Handler(r)
		return pattern == "GET /healthz"
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return Authenticate(logger, testSessions(), resolver, isPublic)(mux), &seen
}

func cookieNamed(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestAuthenticate_APublicRouteIsServedWithoutASession(t *testing.T) {
	called := false
	handler, _ := protected(t, ResolverFunc(func(context.Context, time.Time, string) (auth.Principal, error) {
		called = true
		return auth.Principal{}, auth.ErrNoSession
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if called {
		t.Error("a public route cost a session lookup")
	}
}

func TestAuthenticate_ARequestWithNoCookieIsRedirectedToSignIn(t *testing.T) {
	handler, _ := protected(t, acceptEveryToken(sitterPrincipal()))

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, "/plants", nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("%s: status = %d, want %d", method, rec.Code, http.StatusSeeOther)
		}
		if got := rec.Header().Get("Location"); got != signInPath {
			t.Errorf("%s: Location = %q, want %q", method, got, signInPath)
		}
	}
}

func TestAuthenticate_AnUnknownTokenClearsTheCookieAndRedirectsToSignIn(t *testing.T) {
	handler, _ := protected(t, rejectEveryToken)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants", nil)))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signInPath {
		t.Errorf("status = %d to %q, want %d to %q", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, signInPath)
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_session")
	if cookie == nil || cookie.MaxAge >= 0 {
		t.Errorf("Set-Cookie = %v, want the session cookie cleared", cookie)
	}
}

func TestAuthenticate_AValidSessionReachesTheHandlerAndExtendsTheCookie(t *testing.T) {
	handler, seen := protected(t, acceptEveryToken(sitterPrincipal()))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if seen.Garden.Name != "Rosewood" || seen.User.Handle != "ellie" {
		t.Errorf("the handler saw %s on %s, want ellie on Rosewood", seen.User.Handle, seen.Garden.Name)
	}
	if !seen.Can(auth.CareLog) || seen.Can(auth.PlantCreate) {
		t.Errorf("the handler's principal can log care = %v and create plants = %v, want true and false", seen.Can(auth.CareLog), seen.Can(auth.PlantCreate))
	}

	cookie := cookieNamed(t, rec, "__Host-sprig_session")
	if cookie == nil || cookie.Value != testToken || cookie.MaxAge != int(testTTL/time.Second) {
		t.Errorf("Set-Cookie = %v, want the same token reissued with Max-Age %d", cookie, int(testTTL/time.Second))
	}
}

// noGardenPrincipal is the principal Resolve returns for an account with no
// live membership. Only Session and User are set.
func noGardenPrincipal() auth.Principal {
	p := sitterPrincipal()
	return auth.Principal{Session: store.Session{UserID: p.User.ID}, User: p.User}
}

// gated returns a handler under requireGarden and a flag the wrapped route
// sets when it runs. Every request is made with principal on its context.
func gated(t *testing.T, principal auth.Principal, signupEnabled bool) (http.Handler, *bool) {
	t.Helper()
	ran := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ran = true
		w.WriteHeader(http.StatusNoContent)
	})
	h := requireGarden(testTemplates(), signupEnabled, inner)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal)))
	}), &ran
}

func TestRequireGarden_ASessionInAGardenReachesTheRoute(t *testing.T) {
	handler, ran := gated(t, sitterPrincipal(), false)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants", nil))

	if !*ran || rec.Code != http.StatusNoContent {
		t.Errorf("the route ran = %v with status %d, want it to run", *ran, rec.Code)
	}
}

// onNoGardenPage reports whether body is the "You're in no garden" page.
func onNoGardenPage(body string) bool {
	return strings.Contains(body, "You&rsquo;re in no garden")
}

func TestRequireGarden_ASessionOnNoGardenIsToldSoAndIsOfferedSetUpOnlyWhenSignUpIsOn(t *testing.T) {
	for _, signupEnabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "sign-up on", false: "sign-up off"}[signupEnabled], func(t *testing.T) {
			handler, ran := gated(t, noGardenPrincipal(), signupEnabled)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants", nil))

			if *ran {
				t.Error("the route ran for a session on no garden")
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			page := rec.Body.String()
			if !onNoGardenPage(page) {
				t.Errorf("the page does not say the account is in no garden:\n%s", page)
			}
			for _, want := range []string{"You are signed in as Ellie", `<form method="post" action="` + signOutPath + `">`} {
				if !strings.Contains(page, want) {
					t.Errorf("the page lacks %s:\n%s", want, page)
				}
			}
			buttonNamed(t, page, "Sign out")
			for _, tab := range []string{plantsPath, morePath} {
				if strings.Contains(page, `href="`+tab+`"`) {
					t.Errorf("the page links to %s, and there is no garden to open", tab)
				}
			}
			if linkTo(page, setupSignedInPath, "Set up a garden of your own") != signupEnabled {
				t.Errorf("the page offers to set up a garden = %v, want %v", !signupEnabled, signupEnabled)
			}
		})
	}
}

func TestRequireGarden_AnHtmxRequestOnNoGardenLoadsTodayInsteadOfSwapping(t *testing.T) {
	handler, ran := gated(t, noGardenPrincipal(), true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, logPath(rosewoodPlantID), nil)
	req.Header.Set("HX-Request", "true")
	handler.ServeHTTP(rec, req)

	if *ran {
		t.Error("the route ran for a session on no garden")
	}
	if got := rec.Header().Get("HX-Redirect"); got != todayPath {
		t.Errorf("HX-Redirect = %q, want %s", got, todayPath)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("the response has a body, and htmx would swap it into the element the request targeted:\n%s", rec.Body.String())
	}
}

func TestAuthenticate_AFailedLookupIsA500LoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plants", func(http.ResponseWriter, *http.Request) { t.Error("the handler ran") })
	handler := Authenticate(logger, testSessions(), failEveryToken(errors.New("read the session: connection refused")), func(*http.Request) bool { return false })(mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/plants", nil)))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line was not JSON: %v", err)
	}
	if entry["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entry["level"])
	}
	if strings.Contains(buf.String(), testToken) {
		t.Error("the log line carries the session token")
	}
}

func TestRequire_AMissingCapabilityIsTheSame404AsAnUnknownPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /plants", require(auth.PlantCreate, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))
	handler := Authenticate(slog.New(slog.DiscardHandler), testSessions(), acceptEveryToken(sitterPrincipal()), func(*http.Request) bool { return false })(mux)

	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/nope", nil)))

	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/plants", nil)))
	if refused.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", refused.Code, http.StatusNotFound)
	}
	if refused.Body.String() != unknown.Body.String() {
		t.Errorf("body = %q, want the unknown path's %q", refused.Body.String(), unknown.Body.String())
	}

	owner := sitterPrincipal()
	owner.Capabilities = auth.Capabilities{auth.PlantCreate: true}
	handler = Authenticate(slog.New(slog.DiscardHandler), testSessions(), acceptEveryToken(owner), func(*http.Request) bool { return false })(mux)
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, signedIn(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/plants", nil)))
	if allowed.Code != http.StatusCreated {
		t.Errorf("status with the capability = %d, want %d", allowed.Code, http.StatusCreated)
	}
}

func TestPrincipalFrom_PanicsOnARequestThatSkippedAuthenticate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("PrincipalFrom returned on a request with no principal")
		}
	}()
	PrincipalFrom(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}
