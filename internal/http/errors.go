package http

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ismailshak/sprig/internal/photo"
)

// The heading and the sentence on each error page.
const (
	notFoundTitle    = "Page not found"
	notFoundLine     = "There’s no page at this address."
	serverErrorTitle = "Something went wrong"
	serverErrorLine  = "Try again."
	// The 400 says to go back rather than what was wrong, because only a
	// stale or edited page reaches it.
	badRequestTitle = "This request couldn’t be processed"
	badRequestLine  = "Go back and try again."
	// The 413 names the photo, because nothing else on a form is big enough
	// to reach the size cap.
	tooLargeTitle = "The photo is too large"
)

// The 404 and 500 bodies for a request that is not a browser navigation.
var (
	notFoundText    = plainText(notFoundTitle, notFoundLine)
	serverErrorText = plainText(serverErrorTitle, serverErrorLine)
)

var tooLargeLine = "Choose a photo of " + storageFigure(photo.MaxBytes) + " or less."

func plainText(title, line string) string {
	return title + ". " + line
}

// errorPage is the data for the error page.
type errorPage struct {
	Title string
	Line  string
	// Back is the URL the Back to Today link points at. A signed-out person
	// is sent from there to sign in.
	Back string
}

// refuse responds with status. A browser navigation gets the error page.
// Every other request gets the heading and the sentence as one line of plain
// text, because htmx does not swap a response with an error status and a
// script or an API caller has no page to put it on.
func (t *Templates) refuse(w http.ResponseWriter, r *http.Request, status int, title, line string) {
	text := plainText(title, line)
	if !wantsPage(r) {
		http.Error(w, text, status)
		return
	}
	var buf bytes.Buffer
	if err := t.execute(&buf, r, view{page: "error"}, errorPage{Title: title, Line: line, Back: todayPath}); err != nil {
		t.logger.ErrorContext(r.Context(), "render the error page", slog.Any("error", err))
		http.Error(w, text, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// wantsPage reports whether the request lists text/html in Accept. A browser
// does for a navigation.
func wantsPage(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// serverError responds with a 500 and logs err to logger, so a handler that
// calls it does not log the error again. what names the step that failed.
func (t *Templates) serverError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, what string, err error) {
	logger.ErrorContext(r.Context(), what, slog.Any("error", err))
	t.refuse(w, r, http.StatusInternalServerError, serverErrorTitle, serverErrorLine)
}

// notFound responds with a 404. A plant in another garden, a route the person
// may not use and a path that matches nothing all get this same response, so
// none of them can be told from the others.
func (t *Templates) notFound(w http.ResponseWriter, r *http.Request) {
	t.refuse(w, r, http.StatusNotFound, notFoundTitle, notFoundLine)
}

// badRequest responds with a 400 to a post that did not parse, or to one
// holding a value no form offered.
func (t *Templates) badRequest(w http.ResponseWriter, r *http.Request) {
	t.refuse(w, r, http.StatusBadRequest, badRequestTitle, badRequestLine)
}

// tooLarge responds with a 413 to a form post over the size cap.
func (t *Templates) tooLarge(w http.ResponseWriter, r *http.Request) {
	t.refuse(w, r, http.StatusRequestEntityTooLarge, tooLargeTitle, tooLargeLine)
}
