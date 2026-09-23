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

// credential is what a route authenticates a request with. A route takes one
// kind and no other.
type credential int

const (
	// sessionCookie is the cookie a browser holds after signing in. It is
	// the zero value, so a route takes it unless it is listed as public or
	// flagged bearer.
	sessionCookie credential = iota
	// noCredential marks a public route. Authenticate reads no cookie for it.
	noCredential
	// apiToken is a token issued on the Tokens page and sent in the
	// Authorization header. A route taking it does not accept the session
	// cookie.
	apiToken
)

// Authenticate requires a principal on every request whose route takes the
// session cookie, and puts it on the context for PrincipalFrom. credentials is
// keyed by the route pattern MatchPattern puts on the context. A pattern the
// map does not hold takes the session cookie. A request with no cookie, or with
// one that resolves to no session, is redirected to sign in. The cookie is
// reissued when resolving the session moved its deadline.
//
// A public route and a route taking an API token both pass through here.
// requireToken resolves the bearer token further in, inside the mux.
func Authenticate(logger *slog.Logger, templates *Templates, sessions *auth.Sessions, resolver Resolver, credentials map[string]credential) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch credentials[patternFrom(r)] {
			case noCredential, apiToken:
				next.ServeHTTP(w, r)
				return
			}

			token := sessions.TokenFromRequest(r)
			if token == "" {
				logger.LogAttrs(r.Context(), slog.LevelDebug, "session refused", slog.String("reason", "the request has no session cookie"))
				http.Redirect(w, r, signInPath, http.StatusSeeOther)
				return
			}

			principal, err := resolver.Resolve(r.Context(), time.Now(), token)
			switch {
			case errors.Is(err, auth.ErrNoSession):
				logger.LogAttrs(r.Context(), slog.LevelDebug, "session refused", slog.String("reason", err.Error()))
				http.SetCookie(w, sessions.ClearedCookie())
				http.Redirect(w, r, signInPath, http.StatusSeeOther)
				return
			case err != nil:
				templates.serverError(logger, w, r, "resolve the session", err)
				return
			}

			if principal.SessionTouched {
				http.SetCookie(w, sessions.Cookie(token))
			}
			ctx := context.WithValue(r.Context(), principalKey, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireToken resolves the bearer token in the Authorization header and puts
// the principal on the context for PrincipalFrom. A request with no token, or
// one that matches no live row, gets a 401.
func requireToken(logger *slog.Logger, templates *Templates, tokens Resolver, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			unauthorized(w)
			return
		}
		principal, err := tokens.Resolve(r.Context(), time.Now(), token)
		switch {
		case errors.Is(err, auth.ErrNoAPIToken):
			unauthorized(w)
			return
		case err != nil:
			templates.serverError(logger, w, r, "resolve the token", err)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal)))
	})
}

// unauthorized sends a 401 naming the Bearer scheme in WWW-Authenticate. The
// caller is a device with no sign-in page to open, so it gets a refusal and
// not a redirect.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Send an API token from the Tokens page as a Bearer token in the Authorization header.", http.StatusUnauthorized)
}

// hasSession reports whether the request's session cookie resolves to a
// signed-in account. The account need not be in a garden. A request with no
// cookie costs no lookup, so a public route may call this. It sets the session
// cookie again on w when resolving the session moved its deadline, because
// otherwise the cookie would expire before the row.
func hasSession(w http.ResponseWriter, r *http.Request, sessions *auth.Sessions, resolver Resolver, now time.Time) bool {
	token := sessions.TokenFromRequest(r)
	if token == "" {
		return false
	}
	principal, err := resolver.Resolve(r.Context(), now, token)
	if err != nil {
		return false
	}
	if principal.SessionTouched {
		http.SetCookie(w, sessions.Cookie(token))
	}
	return true
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
	// Close is the URL of the Close account page. It is linked here because
	// an account in no garden reaches nothing under More.
	Close string
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
		page := noGardenPage{Name: principal.User.DisplayName, SignOut: signOutPath, Close: closeAccountPath}
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

// PrincipalFrom returns the principal Authenticate or requireToken attached
// to r. It panics on a request carrying none, since every route outside the
// public allowlist has one.
func PrincipalFrom(r *http.Request) auth.Principal {
	principal, ok := r.Context().Value(principalKey).(auth.Principal)
	if !ok {
		panic("PrincipalFrom on a request that passed through neither Authenticate nor requireToken")
	}
	return principal
}

// require wraps h so a principal whose role lacks capability gets the 404 an
// unknown path gets. A 403 would confirm the page exists.
func require(capability auth.Capability, templates *Templates, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !PrincipalFrom(r).Can(capability) {
			templates.notFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}
