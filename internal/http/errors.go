package http

import (
	"log/slog"
	"net/http"
)

// serverError answers with a 500 and logs err, so a handler that calls it does
// not log the error again. what names the step that failed.
func serverError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, what string, err error) {
	logger.ErrorContext(r.Context(), what, slog.Any("error", err))
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
