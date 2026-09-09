package http

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// patternResolver is satisfied by *http.ServeMux. Its Handler method reports
// which pattern matched a request without dispatching it, so Logging can
// record the route instead of the literal path.
type patternResolver interface {
	Handler(r *http.Request) (http.Handler, string)
}

// Logging logs one line per request, carrying the method, the route
// pattern, the status and the duration. The request id comes from the
// logger's handler rather than from an explicit attribute here.
func Logging(logger *slog.Logger, patterns patternResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, pattern := patterns.Handler(r)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("method", r.Method),
				slog.String("pattern", pattern),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// statusRecorder captures the status code a handler wrote, defaulting to
// 200 for a handler that never calls WriteHeader.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer, so a handler
// that flushes or sets a deadline still works through this wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Recover turns a panic in a handler into a logged stack and a 500, rather
// than a crashed process. The 500 is the Something went wrong page. A handler
// that had already started its response keeps what it wrote.
func Recover(logger *slog.Logger, templates *Templates) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				if err := recover(); err != nil {
					logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
						slog.Any("error", err),
						slog.String("stack", string(debug.Stack())),
					)
					if !rec.wroteHeader {
						templates.refuse(rec, r, http.StatusInternalServerError, serverErrorTitle, serverErrorLine)
					}
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
