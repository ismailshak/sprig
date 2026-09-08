package http

import (
	"log/slog"
	"net/http"
)

// serverError responds with a 500 and logs err, so a handler that calls it does
// not log the error again. what names the step that failed.
func serverError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, what string, err error) {
	logger.ErrorContext(r.Context(), what, slog.Any("error", err))
	http.Error(w, serverErrorText, http.StatusInternalServerError)
}

const serverErrorText = "Something went wrong. Try again."

// notFoundText is the body of every 404 that is not a rendered page.
const notFoundText = "Page not found."

// notFound responds with a 404. A plant in another garden, a route the person
// may not use and a page that does not exist all get this same body.
func notFound(w http.ResponseWriter) {
	http.Error(w, notFoundText, http.StatusNotFound)
}

// notFoundHandler is the handler on the route that matches every path no other
// route matches.
var notFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { notFound(w) })

// badRequest responds with a 400 to a post the app's own pages could not have
// made, such as a value no select offered. The body says to go back rather than
// what was wrong, because only a stale or edited page reaches it.
func badRequest(w http.ResponseWriter) {
	http.Error(w, "This request couldn’t be processed. Go back and try again.", http.StatusBadRequest)
}
