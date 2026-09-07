package http

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// setupSignedInPath is the URL of the page where an account that is already
// signed in sets up a garden of its own. The form on it posts to the same URL.
const setupSignedInPath = setupPath + "/signed-in"

// signInToSetUpPath is the URL of the sign-in page with its next parameter set
// to setupSignedInPath, so signing in there redirects to it.
var signInToSetUpPath = signInPath + "?" + nextField + "=" + url.QueryEscape(setupSignedInPath)

// setupSignedInPage is the data for /setup/signed-in. The form has one field,
// the garden's name, because the account already has a display name, a
// timezone and a passkey.
type setupSignedInPage struct {
	Garden      string
	GardenError string
	// Name is the display name of the account signed in.
	Name string
	// Action is the URL the form posts to.
	Action string
	// SignIn is the URL the "Sign in as somebody else" link points at. It goes
	// to the sign-in page with next set to this page.
	SignIn string
}

func newSetupSignedInPage(principal auth.Principal, garden string) setupSignedInPage {
	return setupSignedInPage{
		Garden: garden,
		Name:   principal.User.DisplayName,
		Action: setupSignedInPath,
		SignIn: signInToSetUpPath,
	}
}

// signedInOpen reports whether GET and POST /setup/signed-in are served. They
// are served when sign-up is on, because an install with sign-up off offers no
// garden beyond the first one it was set up with. A closed route is a 404, the
// same as the public setup routes.
func (h *setup) signedInOpen() bool {
	return h.enabled
}

// showSignedIn handles GET /setup/signed-in.
func (h *setup) showSignedIn(w http.ResponseWriter, r *http.Request) {
	if !h.signedInOpen() {
		http.NotFound(w, r)
		return
	}
	h.templates.render(w, r, view{page: "setup-signed-in"}, newSetupSignedInPage(PrincipalFrom(r), ""))
}

// createSignedIn handles POST /setup/signed-in. It writes the garden, moves the
// session onto it and redirects to Today. It writes no account and no passkey,
// because the account signed in has both. Any membership the account already
// holds is left as it is.
func (h *setup) createSignedIn(w http.ResponseWriter, r *http.Request) {
	if !h.signedInOpen() {
		http.NotFound(w, r)
		return
	}
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("garden"))
	page := newSetupSignedInPage(principal, name)
	if name == "" {
		page.GardenError = setupGardenMissing
		h.templates.render(w, r, view{page: "setup-signed-in", status: http.StatusUnprocessableEntity}, page)
		return
	}

	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		garden, _, err := createGardenOwnedBy(ctx, q, name, principal.User.ID)
		if err != nil {
			return err
		}
		if _, err := q.SetSessionGarden(ctx, &garden.ID, principal.Session.TokenHash); err != nil {
			return err
		}
		// The new garden is recorded on the account as well, so the next
		// session starts there.
		return q.SetLastGarden(ctx, &garden.ID, principal.User.ID)
	})
	if err != nil {
		serverError(h.logger, w, r, "set up the garden", err)
		return
	}
	// The new membership starts with the digest on. The account may already
	// have a subscribed browser, so a digest can be due at once.
	h.wake.call()
	http.Redirect(w, r, todayPath, http.StatusSeeOther)
}
