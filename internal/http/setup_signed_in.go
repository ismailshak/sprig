package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// setupSignedInPath is the URL of the page where an account that is already
// signed in sets up a garden of its own. The form on it posts to the same URL.
const setupSignedInPath = setupPath + setupSignedInSuffix

// setupSignedInSuffix is appended to /setup or a setup link's path to make the
// path of the signed-in setup page.
const setupSignedInSuffix = "/signed-in"

// setupSignedInPathFor is the URL of the signed-in setup page for the setup
// link with token, or setupSignedInPath when token is "".
func setupSignedInPathFor(token string) string {
	if token == "" {
		return setupSignedInPath
	}
	return SetupLinkPath(token) + setupSignedInSuffix
}

// signInToSetUpPath is the URL of the sign-in page with its next parameter set
// to setupSignedInPathFor(token), so signing in there redirects to it.
func signInToSetUpPath(token string) string {
	return signInPath + "?" + nextField + "=" + url.QueryEscape(setupSignedInPathFor(token))
}

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

// newSetupSignedInPage fills the page for principal. token is the setup link's
// token, or "" on /setup/signed-in.
func newSetupSignedInPage(token string, principal auth.Principal, garden string) setupSignedInPage {
	return setupSignedInPage{
		Garden: garden,
		Name:   principal.User.DisplayName,
		Action: setupSignedInPathFor(token),
		SignIn: signInToSetUpPath(token),
	}
}

// signedInOpen reports whether the signed-in setup page and its post are
// served. Under a setup link's URL they are served while the link is unused
// and unexpired. On /setup/signed-in they are served when sign-up is on,
// because an install with sign-up off offers no garden beyond the first one
// without a setup link. A closed route is a 404, the same as the public setup
// routes.
func (h *setup) signedInOpen(r *http.Request) (bool, error) {
	if token := r.PathValue("token"); token != "" {
		return h.queries.SetupLinkOpen(r.Context(), auth.HashToken(token), h.now())
	}
	return h.enabled, nil
}

// showSignedIn handles GET /setup/signed-in and GET /setup/{token}/signed-in.
func (h *setup) showSignedIn(w http.ResponseWriter, r *http.Request) {
	if open, err := h.signedInOpen(r); err != nil {
		h.templates.serverError(h.logger, w, r, "open the setup page", err)
		return
	} else if !open {
		h.templates.notFound(w, r)
		return
	}
	h.templates.render(w, r, view{page: "setup-signed-in"}, newSetupSignedInPage(r.PathValue("token"), PrincipalFrom(r), ""))
}

// createSignedIn handles POST /setup/signed-in and POST
// /setup/{token}/signed-in. It writes the garden, moves the session onto it
// and redirects to Today. It writes no account and no passkey, because the
// account signed in has both. Any membership the account already holds is
// left as it is.
func (h *setup) createSignedIn(w http.ResponseWriter, r *http.Request) {
	if open, err := h.signedInOpen(r); err != nil {
		h.templates.serverError(h.logger, w, r, "set up the garden", err)
		return
	} else if !open {
		h.templates.notFound(w, r)
		return
	}
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("garden"))
	page := newSetupSignedInPage(r.PathValue("token"), principal, name)
	if name == "" {
		page.GardenError = setupGardenMissing
		h.templates.render(w, r, view{page: "setup-signed-in", status: http.StatusUnprocessableEntity}, page)
		return
	}

	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		if err := h.claim(ctx, q, r); err != nil {
			return err
		}
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
	if errors.Is(err, errSetupClosed) {
		h.templates.notFound(w, r)
		return
	}
	if err != nil {
		h.templates.serverError(h.logger, w, r, "set up the garden", err)
		return
	}
	// The new membership starts with the digest on. The account may already
	// have a subscribed browser, so a digest can be due at once.
	h.wake.call()
	http.Redirect(w, r, todayPath, http.StatusSeeOther)
}
