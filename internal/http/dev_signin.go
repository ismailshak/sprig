//go:build dev

package http

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// The development sign-in starts a session as any user in the database, with
// the handle as the whole credential. It exists so the screens can be built
// before passkeys are, and only the proof of identity is faked. The session
// row, the cookie and the middleware behind them are the production ones. The
// build tag keeps it out of the binary the image is built from, and a test in
// cmd/sprig builds that binary and asserts the route is absent from it. The
// deployed instance stays off the public tunnel while this file exists.

const devSignInPath = "/dev/signin"

func init() {
	// The allowlist entries live here rather than beside the other public
	// routes, so a production build does not name a route it does not serve.
	publicRoutes["GET "+devSignInPath] = true
	publicRoutes["POST "+devSignInPath] = true
}

// devRoutes returns the development sign-in routes. The page is rendered from
// its own template rather than the template tree.
func devRoutes(sessions *auth.Sessions, resolver *auth.Resolver, queries *store.Queries, _ *Templates) []route {
	d := &devSignIn{sessions: sessions, queries: queries, resolver: resolver}
	return []route{
		{pattern: "GET " + devSignInPath, handler: http.HandlerFunc(d.show)},
		{pattern: "POST " + devSignInPath, handler: http.HandlerFunc(d.start)},
	}
}

type devSignIn struct {
	sessions *auth.Sessions
	queries  *store.Queries
	resolver *auth.Resolver
}

// An error on these routes goes into the response rather than the log,
// because the developer who caused it is the only reader of either.

func (d *devSignIn) show(w http.ResponseWriter, r *http.Request) {
	users, err := d.queries.ListUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A failed write means the browser hung up, which nothing here can act on.
	_ = devSignInPage.Execute(w, users)
}

// start creates a session for the user whose handle the form names, on their
// oldest live membership. A user with no live membership gets a 409, since the
// screen for that case belongs to the passkey sign-in.
func (d *devSignIn) start(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handle := r.FormValue("handle")

	user, err := d.queries.GetUserByHandle(ctx, handle)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, fmt.Sprintf("no user has the handle %q", handle), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	membership, err := d.resolver.StartingMembership(ctx, now, user.ID)
	if errors.Is(err, auth.ErrNoLiveMembership) {
		http.Error(w, fmt.Sprintf("%s is in no garden, so there is nothing to start a session on", handle), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Switching to another person replaces the session rather than leaving the
	// previous row live until its TTL.
	if err := d.sessions.DeleteFromRequest(ctx, r); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	token, _, err := d.sessions.Create(ctx, now, user.ID, membership.GardenID, nil, r.UserAgent())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, d.sessions.Cookie(token))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// devSignInPage is inline rather than in the template tree because it is
// deleted once passkeys exist.
var devSignInPage = template.Must(template.New("dev-signin").Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Development sign-in</title>
<h1>Development sign-in</h1>
<p>Start a session as one of these users. This page exists only in a development build.</p>
{{range .}}<form method="post" action="/dev/signin">
<button name="handle" value="{{.Handle}}">{{.DisplayName}} ({{.Handle}})</button>
</form>
{{else}}<p>There are no users. Run <code>mise run seed</code> first.</p>
{{end}}`))
