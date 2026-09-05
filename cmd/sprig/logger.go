package main

import (
	"io"
	"log/slog"

	sprighttp "github.com/ismailshak/sprig/internal/http"
)

// newLogger builds the process-wide logger, in JSON unless cfg asks for text.
// The context handler adds the request id to every line logged during a
// request, so callers do not pass it themselves.
func newLogger(cfg config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.logLevel}

	var handler slog.Handler
	if cfg.logFormat == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(sprighttp.NewContextHandler(handler))
}
