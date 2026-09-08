package http

import (
	"net/http"
	"strings"
)

// nextField is the name of the query parameter on the sign-in page's URL and
// of the hidden input on its form. Both hold the path to redirect to after
// signing in. The invite page sets it, so somebody who already has an account
// lands on the page that accepts the invite. When it is empty, signing in
// lands on Today.
const nextField = "next"

// returnPath returns next when it is a path on this site, and Today
// otherwise. Only a path starting with a single slash is accepted, because an
// absolute URL, a scheme-relative //host and a /\host all send the person to
// another site after signing in. A path holding a tab, a carriage return or a
// newline is refused for the same reason: the Location header keeps the
// character and the browser strips it before parsing the URL, so
// "/\t/example.com" arrives as "//example.com".
func returnPath(next string) string {
	if strings.ContainsAny(next, "\t\r\n") {
		return todayPath
	}
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return todayPath
}

// tooManySignIns is the sentence the sign-in page shows once the address or
// the whole route has spent its budget.
const tooManySignIns = "Too many attempts. Wait a few minutes and try again."

// signInPage is the data the sign-in template renders. The page has one
// button, no fields, and a line above the button saying why the last attempt
// failed.
type signInPage struct {
	// Refusal is the sentence above the button. It is empty until an attempt
	// has been refused.
	Refusal string
	// Action is the URL the form posts the signed credential to.
	Action string
	// Challenge is the URL the page's script posts to for a challenge, before it
	// calls the browser's credential API.
	Challenge string
	// Field is the name of the hidden input the signed credential goes in. The
	// script reads the name off the form, so it is written in one place.
	Field string
	// Recover is the URL the link in the note under the button points at.
	Recover string
	// Next is the path the sign-in redirects to afterwards. It is rendered as
	// a hidden input on the form, named NextField. It is empty for a sign-in
	// that lands on Today. The template then renders no input at all.
	Next string
	// NextField is the name of that hidden input.
	NextField string
}

func newSignInPage(refusal, next string) signInPage {
	if next == todayPath {
		next = ""
	}
	return signInPage{
		Refusal:   refusal,
		Action:    signInPath,
		Challenge: challengePath,
		Field:     credentialField,
		Recover:   recoverPath,
		Next:      next,
		NextField: nextField,
	}
}

// showSignIn handles GET /signin and renders the sign-in page. It is public,
// and it renders for a signed-in visitor too, because signing in again
// replaces the session they already have.
func (h *passkeyCeremony) showSignIn(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "signin"}, newSignInPage("", returnPath(r.URL.Query().Get(nextField))))
}

// renderSignIn renders the sign-in page with refusal above the button and
// status as the response status. The form keeps the next path that was posted,
// so signing in on the second try still lands there.
func (h *passkeyCeremony) renderSignIn(w http.ResponseWriter, r *http.Request, status int, refusal string) {
	h.templates.render(w, r, view{page: "signin", status: status}, newSignInPage(refusal, returnPath(r.PostFormValue(nextField))))
}

// tooManySignInAnswers is the response to a POST /signin past the budget. It
// renders the sign-in page with the rate-limit line above the button, because
// the request came from a form and a person is reading the response.
func (h *passkeyCeremony) tooManySignInAnswers(w http.ResponseWriter, r *http.Request) {
	h.renderSignIn(w, r, http.StatusTooManyRequests, tooManySignIns)
}

// tooManySignInChallenges is the response to a POST /signin/challenge past the
// budget. The rate-limit line is plain text, because the page's script fetches
// this URL and puts the body above the button.
func tooManySignInChallenges(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, tooManySignIns, http.StatusTooManyRequests)
}
