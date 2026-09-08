package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	// registerPath is the URL the Passkeys page's script posts to for a
	// registration challenge. It then runs navigator.credentials.create and
	// posts the credential to passkeysPath.
	registerPath = passkeysPath + "/challenge"
	// challengePath is the URL the sign-in page's script posts to for a sign-in
	// challenge. It then runs navigator.credentials.get and posts the credential
	// to signInPath.
	challengePath = signInPath + "/challenge"
)

// credentialField is the name of the form field the page posts the browser's
// credential in. It holds what PublicKeyCredential.toJSON produced.
const credentialField = "credential"

// passkeyCeremony serves the four passkey requests: the challenge and the
// credential post that register a device, and the same pair for signing in.
type passkeyCeremony struct {
	logger    *slog.Logger
	passkeys  *auth.Passkeys
	sessions  *auth.Sessions
	queries   *store.Queries
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
}

// registerChallenge handles POST /more/passkeys/challenge and returns the
// options for navigator.credentials.create as JSON. It is a post rather than a
// get because it writes the challenge row.
func (h *passkeyCeremony) registerChallenge(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	held, err := h.queries.ListPasskeys(r.Context(), principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the passkeys", err)
		return
	}
	creation, cookie, err := h.passkeys.BeginRegistration(r.Context(), h.now(), principal.User, held)
	if err != nil {
		serverError(h.logger, w, r, "start the registration", err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(h.logger, w, r, creation)
}

// register handles POST /more/passkeys and saves the device the browser just
// enrolled. It redirects to the Passkeys page, or renders it again with the
// reason the device was refused.
func (h *passkeyCeremony) register(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	// The page that reports a refusal lists the devices already enrolled. The
	// list is read here, before the credential is checked, so a refusal renders
	// from it and runs no query after a failed write.
	keys, err := h.queries.ListPasskeys(r.Context(), principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the passkeys", err)
		return
	}
	// WebAuthn returns no name for a device, so the row is labelled with the
	// browser the registration came from. The Notifications page labels a
	// browser the same way.
	name := browserName(userAgentOf(r))

	http.SetCookie(w, h.passkeys.ClearedCeremonyCookie())
	body := strings.NewReader(r.PostForm.Get(credentialField))
	_, err = h.passkeys.FinishRegistration(r.Context(), h.now(), r, principal.User, body, name)
	if err != nil {
		h.refuseRegistration(w, r, principal, keys, err)
		return
	}
	http.Redirect(w, r, passkeysPath, http.StatusSeeOther)
}

// refuseRegistration re-renders the Passkeys page with the reason the device
// was not enrolled. keys is the list the page shows, read before the credential
// was checked.
func (h *passkeyCeremony) refuseRegistration(w http.ResponseWriter, r *http.Request, principal auth.Principal, keys []store.PasskeyCredential, err error) {
	message := registrationRefusal(h.logger, w, r, err, "add the passkey")
	if message == "" {
		return
	}
	page := newPasskeysPage(keys, h.now().In(locationFor(principal.User)))
	page.Error = message
	h.templates.render(w, r, view{page: "passkeys", status: http.StatusUnprocessableEntity}, page)
}

// registrationRefusal returns the sentence a page shows when a device was not
// enrolled. Every refusal the person can act on has its own sentence.
// Anything else gets no sentence, because the response has been written here
// instead: a 400 for a post with no credential in it, and a 500 for the rest.
// what is the action the 500's log line names as failed.
func registrationRefusal(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error, what string) string {
	switch {
	case errors.Is(err, auth.ErrNotVerified):
		return "This device didn’t verify you. Turn on its screen lock or set a PIN, then try again."
	case errors.Is(err, auth.ErrNotDiscoverable):
		return "This device can’t store a passkey. Try another device or a security key."
	case errors.Is(err, auth.ErrCeremonyGone):
		return "The request timed out. Try again."
	case errors.Is(err, auth.ErrAlreadyRegistered):
		return "This device already has a passkey for sprig."
	case errors.Is(err, auth.ErrFailedVerification):
		// The credential parsed and did not check out. The page's script never
		// produces one of those, so the reason goes in the log.
		logger.WarnContext(r.Context(), "refuse the passkey", slog.Any("error", err))
		return "The passkey couldn’t be verified and wasn’t added. Try again."
	case errors.Is(err, auth.ErrBadCredential):
		badRequest(w)
		return ""
	default:
		serverError(logger, w, r, what, err)
		return ""
	}
}

// signInChallenge handles POST /signin/challenge and returns the options for
// navigator.credentials.get as JSON. No account is named, because the browser
// offers the passkeys it holds for this site and the credential says who this
// is.
func (h *passkeyCeremony) signInChallenge(w http.ResponseWriter, r *http.Request) {
	assertion, cookie, err := h.passkeys.BeginAssertion(r.Context(), h.now())
	if err != nil {
		serverError(h.logger, w, r, "start the sign-in", err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(h.logger, w, r, assertion)
}

// signIn handles POST /signin and starts a session for the account the passkey
// proves. It redirects to the path in the form's next field. A field that is
// empty or names another site redirects to Today.
//
// The session starts with no garden. The next request sets it to the garden
// the account last switched to, or to the account's oldest live membership. An
// account with no live membership is left without a garden and gets the
// "You're in no garden" page, so somebody whose access ended can still sign in
// to set up a garden of their own or accept an invite.
func (h *passkeyCeremony) signIn(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}

	http.SetCookie(w, h.passkeys.ClearedCeremonyCookie())
	body := strings.NewReader(r.PostForm.Get(credentialField))
	user, passkey, err := h.passkeys.FinishAssertion(r.Context(), h.now(), r, body)
	if err != nil {
		h.refuseSignIn(w, r, err)
		return
	}

	// A browser that was already signed in gets a new session in place of the
	// one it arrived with.
	if err := h.sessions.DeleteFromRequest(r.Context(), r); err != nil {
		serverError(h.logger, w, r, "end the previous session", err)
		return
	}
	token, _, err := h.sessions.Create(r.Context(), h.now(), user.ID, nil, &passkey.ID, r.UserAgent())
	if err != nil {
		serverError(h.logger, w, r, "start the session", err)
		return
	}
	http.SetCookie(w, h.sessions.Cookie(token))
	next := returnPath(r.PostForm.Get(nextField))
	//nolint:gosec // returnPath passes on only a path on this site
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// refuseSignIn renders the sign-in page again as a 401, with the reason the
// sign-in failed above the button. Every reason the person can act on has its
// own sentence. A post with no credential in it is a 400, and anything else is
// a 500 with the error in the log.
func (h *passkeyCeremony) refuseSignIn(w http.ResponseWriter, r *http.Request, err error) {
	var message string
	switch {
	case errors.Is(err, auth.ErrNotVerified):
		message = "This device didn’t verify you. Unlock it and try again."
	case errors.Is(err, auth.ErrUnknownCredential):
		message = "This passkey isn’t registered. It may have been removed on the Passkeys page."
	case errors.Is(err, auth.ErrClonedCredential):
		// Two copies of the private key are in use, and this request may be from
		// either of them, so the message does not say what was wrong. The log
		// line names the passkey row, so whoever reads it can find the account.
		h.logger.WarnContext(r.Context(), "refuse the sign-in", slog.Any("error", err))
		message = "This passkey can’t be used. Ask the garden’s owner for a new invite link."
	case errors.Is(err, auth.ErrFailedVerification):
		h.logger.WarnContext(r.Context(), "refuse the sign-in", slog.Any("error", err))
		message = "This passkey couldn’t be verified. Try again."
	case errors.Is(err, auth.ErrCeremonyGone):
		message = "Sign-in timed out. Try again."
	case errors.Is(err, auth.ErrBadCredential):
		badRequest(w)
		return
	default:
		serverError(h.logger, w, r, "sign in", err)
		return
	}
	h.renderSignIn(w, r, http.StatusUnauthorized, message)
}

// writeJSON writes v as a JSON response with Cache-Control: no-store, because a
// challenge can only be answered once.
func writeJSON(logger *slog.Logger, w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		serverError(logger, w, r, "encode the challenge", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	// A failed write means the client hung up, and nothing here can act on it.
	_, _ = w.Write(body)
}

// userAgentOf returns the request's User-Agent, or nil when it sent none.
func userAgentOf(r *http.Request) *string {
	agent := r.UserAgent()
	if agent == "" {
		return nil
	}
	return &agent
}
