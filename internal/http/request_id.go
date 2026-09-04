package http

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
)

// RequestIDHeader is the response header a request's id is echoed under.
const RequestIDHeader = "X-Request-Id"

type contextKey int

const (
	requestIDKey contextKey = iota
	principalKey
)

// RequestID generates an id for every request, attaches it to the request's
// context and echoes it on the response, so a client and a log line can be
// matched to the same request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := rand.Text()
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the id RequestID attached to ctx, or "" if
// none was.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// NewContextHandler wraps h so every record it handles carries the request
// id of whichever request produced it. Callers log through a *slog.Logger's
// *Context methods and never pass the id by hand.
func NewContextHandler(h slog.Handler) slog.Handler {
	return contextHandler{h}
}

type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
