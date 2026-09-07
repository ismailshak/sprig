package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

const signInPath = "/signin"

// Resolver turns the token a request presented into the principal behind it.
type Resolver interface {
	Resolve(ctx context.Context, now time.Time, token string) (auth.Principal, error)
}

// ResolverFunc is a Resolver made from a function.
type ResolverFunc func(ctx context.Context, now time.Time, token string) (auth.Principal, error)

// Resolve calls f.
func (f ResolverFunc) Resolve(ctx context.Context, now time.Time, token string) (auth.Principal, error) {
	return f(ctx, now, token)
}

// Authenticate requires a principal on every request isPublic does not exempt,
// and puts it on the context for PrincipalFrom. A request with no session, or
// with a token that resolves to none, is redirected to sign in. A resolved
// session has its cookie reissued, so the cookie's expiry extends along with
// the session row's.
func Authenticate(logger *slog.Logger, sessions *auth.Sessions, resolver Resolver, isPublic func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublic(r) {
				next.ServeHTTP(w, r)
				return
			}

			token := sessions.TokenFromRequest(r)
			if token == "" {
				http.Redirect(w, r, signInPath, http.StatusSeeOther)
				return
			}

			principal, err := resolver.Resolve(r.Context(), time.Now(), token)
			switch {
			case errors.Is(err, auth.ErrNoSession):
				http.SetCookie(w, sessions.ClearedCookie())
				http.Redirect(w, r, signInPath, http.StatusSeeOther)
				return
			case err != nil:
				logger.ErrorContext(r.Context(), "resolve the session", slog.Any("error", err))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}

			http.SetCookie(w, sessions.Cookie(token))
			ctx := context.WithValue(r.Context(), principalKey, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// hasSession reports whether the request's session cookie resolves to a
// signed-in account. The account need not be in a garden. A request with no
// cookie costs no lookup, so a public route may call this.
func hasSession(r *http.Request, sessions *auth.Sessions, resolver *auth.Resolver, now time.Time) bool {
	token := sessions.TokenFromRequest(r)
	if token == "" {
		return false
	}
	_, err := resolver.Resolve(r.Context(), now, token)
	return err == nil
}

// noGardenPage is the data for the "You're in no garden" page. Every route
// that needs a garden renders it for an account in no garden.
type noGardenPage struct {
	// Name is the display name of the account signed in.
	Name string
	// SetUp is the URL of the page that sets up a garden for the account. It
	// is empty when sign-up is off.
	SetUp string
	// SignOut is the URL the Sign out button posts to.
	SignOut string
}

// requireGarden wraps a route that needs a garden. For an account in no
// garden it renders the "You're in no garden" page in place of the route,
// with a 200, because the request named no object to refuse. signupEnabled is
// SPRIG_SIGNUP_ENABLED. It decides whether the page offers to set up a
// garden.
func requireGarden(templates *Templates, signupEnabled bool, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := PrincipalFrom(r)
		if principal.InGarden() {
			h.ServeHTTP(w, r)
			return
		}
		// This page is a whole document, and htmx would swap the response
		// into the element the request targeted. HX-Redirect loads Today
		// instead, where the same check renders the page.
		if isHTMX(r) {
			w.Header().Set("HX-Redirect", todayPath)
			return
		}
		page := noGardenPage{Name: principal.User.DisplayName, SignOut: signOutPath}
		if signupEnabled {
			page.SetUp = setupSignedInPath
		}
		templates.render(w, r, view{page: "no-garden"}, page)
	})
}

// locationFor returns the timezone a user's dates are shown in. The account
// form restricts the column to names the zone database knows, so an unknown
// name means the row was written some other way. It gets UTC rather than an
// error.
func locationFor(user store.AppUser) *time.Location {
	location, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return time.UTC
	}
	return location
}

// PrincipalFrom returns the principal Authenticate attached to r. It panics
// on a request carrying none, since every route outside the public allowlist
// has one.
func PrincipalFrom(r *http.Request) auth.Principal {
	principal, ok := r.Context().Value(principalKey).(auth.Principal)
	if !ok {
		panic("PrincipalFrom on a request that did not pass through Authenticate")
	}
	return principal
}

// require wraps h so a principal whose role lacks capability gets the 404 an
// unknown path gets. A 403 would confirm the page exists.
func require(capability auth.Capability, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !PrincipalFrom(r).Can(capability) {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
