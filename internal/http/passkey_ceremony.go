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
	// posts the answer to passkeysPath.
	registerPath = passkeysPath + "/challenge"
	// challengePath is the URL the sign-in page's script posts to for a sign-in
	// challenge. It then runs navigator.credentials.get and posts the answer to
	// signInPath.
	challengePath = signInPath + "/challenge"
)

// credentialField is the name of the form field the page posts the browser's
// answer to a challenge in. It holds what PublicKeyCredential.toJSON produced.
const credentialField = "credential"

// passkeyCeremony serves the four passkey requests: a challenge and an answer
// for registering a device, and the same pair for signing in.
type passkeyCeremony struct {
	logger    *slog.Logger
	passkeys  *auth.Passkeys
	sessions  *auth.Sessions
	resolver  *auth.Resolver
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
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	// The page that reports a refusal lists the devices already enrolled. The
	// list is read here, before the answer is checked, so a refusal renders
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
// was not enrolled. keys is the list the page shows, read before the answer was
// checked.
func (h *passkeyCeremony) refuseRegistration(w http.ResponseWriter, r *http.Request, principal auth.Principal, keys []store.PasskeyCredential, err error) {
	message := registrationRefusal(h.logger, w, r, err, "Add a passkey", "add the passkey")
	if message == "" {
		return
	}
	page := newPasskeysPage(keys, h.now().In(locationFor(principal.User)))
	page.Error = message
	h.templates.render(w, r, view{page: "passkeys", status: http.StatusUnprocessableEntity}, page)
}

// registrationRefusal returns the sentence a page shows when a device was not
// enrolled. button is the label of the button the sentence tells the person to
// press again. Every refusal the person can act on has its own sentence.
// Anything else gets no sentence, because the response has been written here
// instead: a 400 for a post with no credential in it, and a 500 for the rest.
// what is the action the 500's log line names as failed.
func registrationRefusal(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error, button, what string) string {
	switch {
	case errors.Is(err, auth.ErrNotVerified):
		return "This device did not check that it was you. Turn on its screen lock, or set a PIN on your security key, and try again."
	case errors.Is(err, auth.ErrNotDiscoverable):
		return "This device would not store the passkey, so there would be nothing to sign in with. Try a phone, a laptop or a security key with room on it."
	case errors.Is(err, auth.ErrCeremonyGone):
		return "That took too long, so the request has expired. Press " + button + " again."
	case errors.Is(err, auth.ErrAlreadyRegistered):
		return "This device already has a passkey for sprig."
	case errors.Is(err, auth.ErrFailedVerification):
		// The answer parsed and did not check out. The page's script never
		// produces one of those, so the reason goes in the log.
		logger.WarnContext(r.Context(), "refuse the passkey", slog.Any("error", err))
		return "This passkey could not be checked, so it was not added. Press " + button + " again."
	case errors.Is(err, auth.ErrBadCredential):
		http.Error(w, "the form did not send a credential", http.StatusBadRequest)
		return ""
	default:
		serverError(logger, w, r, what, err)
		return ""
	}
}

// signInChallenge handles POST /signin/challenge and returns the options for
// navigator.credentials.get as JSON. No account is named, because the browser
// offers the passkeys it holds for this site and its answer says who this is.
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
// proves, on its oldest live membership.
func (h *passkeyCeremony) signIn(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}

	http.SetCookie(w, h.passkeys.ClearedCeremonyCookie())
	body := strings.NewReader(r.PostForm.Get(credentialField))
	user, passkey, err := h.passkeys.FinishAssertion(r.Context(), h.now(), r, body)
	if err != nil {
		h.refuseSignIn(w, r, err)
		return
	}

	now := h.now()
	membership, err := h.resolver.OldestLiveMembership(r.Context(), now, user.ID)
	if errors.Is(err, auth.ErrNoLiveMembership) {
		// The passkey is valid and the account is in no garden, so there is
		// nothing to open a session on. A sitter whose membership ran out is
		// the case this covers. The status is 403 rather than 404 because the
		// 404 rule is for an object a request named, and this request named
		// none.
		h.renderSignIn(w, r, http.StatusForbidden, "That passkey signed in, and the account behind it is in no garden. Ask whoever runs the garden to invite you again.")
		return
	}
	if err != nil {
		serverError(h.logger, w, r, "find the garden", err)
		return
	}

	// A browser that was already signed in gets a new session in place of the
	// one it arrived with.
	if err := h.sessions.DeleteFromRequest(r.Context(), r); err != nil {
		serverError(h.logger, w, r, "end the previous session", err)
		return
	}
	token, _, err := h.sessions.Create(r.Context(), now, user.ID, membership.GardenID, &passkey.ID, r.UserAgent())
	if err != nil {
		serverError(h.logger, w, r, "start the session", err)
		return
	}
	http.SetCookie(w, h.sessions.Cookie(token))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// refuseSignIn renders the sign-in page again as a 401, with the reason the
// sign-in failed above the button. Every reason the person can act on has its
// own sentence. A post with no credential in it is a 400, and anything else is
// a 500 with the error in the log.
func (h *passkeyCeremony) refuseSignIn(w http.ResponseWriter, r *http.Request, err error) {
	var message string
	switch {
	case errors.Is(err, auth.ErrNotVerified):
		message = "This device did not check that it was you. Unlock it and try again."
	case errors.Is(err, auth.ErrUnknownCredential):
		message = "That passkey is not one sprig knows. It may have been removed from the account's Passkeys page."
	case errors.Is(err, auth.ErrClonedCredential):
		// Two copies of the private key are in use, and this request may be from
		// either of them, so the message does not say what was wrong. The log
		// line names the passkey row, so whoever reads it can find the account.
		h.logger.WarnContext(r.Context(), "refuse the sign-in", slog.Any("error", err))
		message = "That passkey cannot be used. Ask whoever runs the garden to remove it and invite you again."
	case errors.Is(err, auth.ErrFailedVerification):
		h.logger.WarnContext(r.Context(), "refuse the sign-in", slog.Any("error", err))
		message = "That passkey could not be checked. Try signing in again."
	case errors.Is(err, auth.ErrCeremonyGone):
		message = "That took too long, so the request has expired. Try signing in again."
	case errors.Is(err, auth.ErrBadCredential):
		http.Error(w, "the form did not send a credential", http.StatusBadRequest)
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
