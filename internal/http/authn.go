package http

import (
	"context"
	"errors"
	"fmt"
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
			var ended *auth.MembershipEndedError
			switch {
			case errors.Is(err, auth.ErrNoSession):
				http.SetCookie(w, sessions.ClearedCookie())
				http.Redirect(w, r, signInPath, http.StatusSeeOther)
				return
			case errors.As(err, &ended):
				http.SetCookie(w, sessions.ClearedCookie())
				writeAccessEnded(w, ended)
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

// writeAccessEnded responds to a signed-in user whose membership has ended.
// The status is 403 rather than 404 because the 404 rule is for an object a
// request named, and this request named none.
func writeAccessEnded(w http.ResponseWriter, ended *auth.MembershipEndedError) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	// A failed write means the browser hung up, which nothing here can act on.
	_, _ = fmt.Fprintf(w, "Your access to %s ended on %s.\n", ended.Garden.Name, ended.EndedAt.In(locationFor(ended.User)).Format("2 January 2006"))
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
