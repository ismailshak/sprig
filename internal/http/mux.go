package http

import (
	"log/slog"
	"net/http"
)

// New builds sprig's handler. Request id runs outermost so it is set before
// anything logs, logging wraps recovery so a recovered panic's 500 still
// produces one request line, and the cross-origin check sits inside both so a
// refused request is logged like any other.
func New(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)

	var handler http.Handler = mux
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
