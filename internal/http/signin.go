package http

import (
	"net/http"
)

// recoverPath is the URL of the Recover an account page, linked from the note
// under the sign-in button. No route serves it yet, so a visitor with no
// session is redirected back to the sign-in page.
const recoverPath = "/recover"

// tooManySignIns is the sentence the sign-in page shows once the address or
// the whole route has spent its budget. It says the limit is on the page
// rather than on the account, because the shared budget can be spent by
// somebody else's attempts.
const tooManySignIns = "Too many tries. Wait a few minutes and try again. This limit is on the page rather than on your account, so it can be somebody else's attempts you are waiting out."

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
}

func newSignInPage(refusal string) signInPage {
	return signInPage{
		Refusal:   refusal,
		Action:    signInPath,
		Challenge: challengePath,
		Field:     credentialField,
		Recover:   recoverPath,
	}
}

// showSignIn handles GET /signin and renders the sign-in page. It is public,
// and it renders for a signed-in visitor too, because signing in again
// replaces the session they already have.
func (h *passkeyCeremony) showSignIn(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "signin"}, newSignInPage(""))
}

// renderSignIn renders the sign-in page with refusal above the button and
// status as the response status.
func (h *passkeyCeremony) renderSignIn(w http.ResponseWriter, r *http.Request, status int, refusal string) {
	h.templates.render(w, r, view{page: "signin", status: status}, newSignInPage(refusal))
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
