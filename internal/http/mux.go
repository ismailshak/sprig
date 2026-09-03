package http

import (
	"log/slog"
	"net/http"
)

// New builds sprig's handler. Every route lives on one ServeMux, wrapped by
// the middleware chain composed here. Request id runs outermost so it's set
// before anything logs. Logging wraps recovery so a recovered panic's 500
// still produces one request line.
func New(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)

	var handler http.Handler = mux
	handler = Recover(logger)(handler)
	handler = Logging(logger, mux)(handler)
	handler = RequestID(handler)
	return handler
}
