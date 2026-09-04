package http

import (
	"log/slog"
	"net/http"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// route is one pattern the server answers. An empty capability admits every
// member, and New wraps a route carrying one in require, so a handler never
// checks its own.
type route struct {
	pattern    string
	capability auth.Capability
	handler    http.Handler
}

// routes is every route the server has. New registers from this slice and the
// enforcement test walks it, because http.ServeMux does not list its patterns
// and a route registered directly on the mux would be one the test cannot see.
// devRoutes is what a development build adds, and empty otherwise.
func routes(sessions *auth.Sessions, queries *store.Queries, templates *Templates) []route {
	base := []route{
		{pattern: "GET /healthz", handler: http.HandlerFunc(handleHealthz)},
	}
	return append(base, devRoutes(sessions, queries, templates)...)
}

// publicRoutes is every route that answers without a session. Authenticate
// covers the rest, so a route in routes is protected until it is listed here.
var publicRoutes = map[string]bool{
	"GET /healthz": true,
}

// New builds sprig's handler. Request id runs outermost so it is set before
// anything logs, logging wraps recovery so a recovered panic's 500 still
// produces one request line, the cross-origin check sits inside both so a
// refused request is logged like any other, and authentication sits inside
// that so a cross-site post is refused before it costs a session lookup.
func New(logger *slog.Logger, sessions *auth.Sessions, resolver Resolver, queries *store.Queries, templates *Templates) http.Handler {
	mux := http.NewServeMux()
	for _, r := range routes(sessions, queries, templates) {
		h := r.handler
		if r.capability != "" {
			h = require(r.capability, h)
		}
		mux.Handle(r.pattern, h)
	}

	isPublic := func(r *http.Request) bool {
		_, pattern := mux.Handler(r)
		return publicRoutes[pattern]
	}

	var handler http.Handler = mux
	handler = Authenticate(logger, sessions, resolver, isPublic)(handler)
	handler = crossOrigin().Handler(handler)
	handler = Recover(logger)(handler)
	handler = Logging(logger, mux)(handler)
	handler = RequestID(handler)
	return handler
}

// crossOrigin is the second half of the CSRF defence after SameSite=Lax on the
// session cookie. It refuses an unsafe method whose Sec-Fetch-Site says
// cross-site or whose Origin names a host other than the one the request
// arrived at. The expected origin is the Host the request came in on, so
// nothing is configured and the check holds behind a proxy as well as on a
// laptop.
func crossOrigin() *http.CrossOriginProtection {
	return http.NewCrossOriginProtection()
}
