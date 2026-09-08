package http

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	// recoverPath is the URL of the Recover an account page, linked from the
	// note under the sign-in button. The form on it posts the code to the same
	// URL.
	recoverPath = "/recover"
	// recoverChallengePath is the URL the Add this device page's script posts
	// to for a registration challenge.
	recoverChallengePath = recoverPath + "/challenge"
	// recoverPasskeyPath is the URL the Add this device form posts the
	// browser's credential to.
	recoverPasskeyPath = recoverPath + "/passkey"
)

// codeField is the name of the input the code is typed into, and of the
// hidden input that holds it on the Add this device form.
const codeField = "code"

const (
	recoverTitle  = "Recover an account"
	addThisDevice = "Add this device"
	registerLabel = "Add passkey"
	// The heading and the sentence shown for a code that cannot be used. A
	// used code, a code nobody made and a string that is not a code all get
	// this page, so somebody holding a stolen sheet of codes cannot find out
	// which of them still work.
	codeCannotBeUsedTitle = "This code can’t be used"
	codeCannotBeUsedLine  = "It may have been used already or replaced by a newer set. Try another code, or ask the garden’s owner for a new invite link."
	// The heading and the sentence shown for a post past the rate limit.
	tooManyAttemptsTitle = "Too many attempts"
	tooManyAttemptsLine  = "Wait a few minutes and try again."
)

// errCodeUsed is returned inside the transaction that registers the passkey
// when the code had been used by the time the post arrived.
var errCodeUsed = errors.New("the code has been used")

// recoverPage is the Recover an account page, in one of three shapes: the
// form with the code field, the registration form once a code has matched, or
// a heading and one sentence saying the code cannot be used.
type recoverPage struct {
	// Title is the heading. On the two forms it names the page. On a refused
	// code it says what happened instead.
	Title string
	// Line is the sentence under the heading when a code is refused. It is
	// empty on both forms. TryAnother is the URL of the Try another code link
	// under it, back to the form.
	Line       string
	TryAnother string
	// AddDevice is true once a code has matched. The page is then the form
	// that registers a passkey.
	AddDevice bool
	// Code is the matched code. The registration form posts it back in a
	// hidden input, so the challenge and the registration find the account
	// again.
	// CodeField is that input's name.
	Code      string
	CodeField string
	// Refusal is the sentence above the registration form saying why the
	// passkey was not added. It is empty until a post is refused.
	Refusal string
	// Action is the URL the form posts to. Challenge is the URL the
	// registration form's script posts to for a challenge. Field is the name
	// of the hidden input the browser's credential goes in.
	Action    string
	Challenge string
	Field     string
}

func recoverForm() recoverPage {
	return recoverPage{Title: recoverTitle, Action: recoverPath, CodeField: codeField}
}

func addDevicePage(code string) recoverPage {
	return recoverPage{
		Title:     addThisDevice,
		AddDevice: true,
		Code:      code,
		CodeField: codeField,
		Action:    recoverPasskeyPath,
		Challenge: recoverChallengePath,
		Field:     credentialField,
	}
}

func turnedAwayPage(title, line string) recoverPage {
	return recoverPage{Title: title, Line: line, TryAnother: recoverPath}
}

// recoverAccount serves the Recover an account page: the form, the post that
// checks a code, the registration challenge for a code that matched, and the
// post that saves the passkey. Using a code never starts a session, so the
// last of those redirects to sign in.
type recoverAccount struct {
	logger    *slog.Logger
	passkeys  *auth.Passkeys
	queries   *store.Queries
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
}

// show handles GET /recover.
func (h *recoverAccount) show(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "recover"}, recoverForm())
}

// check handles POST /recover, the code typed on the form. An unused code
// renders the registration form with the code in a hidden input and writes
// nothing. Every other code gets the same 422 page.
func (h *recoverAccount) check(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	code, _, ok, err := h.lookUp(r)
	if err != nil {
		serverError(h.logger, w, r, "check the recovery code", err)
		return
	}
	if !ok {
		h.renderCannotBeUsed(w, r)
		return
	}
	h.templates.render(w, r, view{page: "recover"}, addDevicePage(code))
}

// lookUp reads the code from the posted form and returns it in the form it is
// stored under, with the account it belongs to. ok is false when the code is
// used, was never made, or is not the shape of a code. The lookup is by hash
// across every account, so the page never asks whose code it is.
func (h *recoverAccount) lookUp(r *http.Request) (code string, user store.AppUser, ok bool, err error) {
	code, ok = auth.CanonicalRecoveryCode(r.PostForm.Get(codeField))
	if !ok {
		return "", store.AppUser{}, false, nil
	}
	userID, err := h.queries.GetLiveRecoveryCode(r.Context(), auth.HashToken(code))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", store.AppUser{}, false, nil
	}
	if err != nil {
		return "", store.AppUser{}, false, err
	}
	user, err = h.queries.GetUser(r.Context(), userID)
	if err != nil {
		return "", store.AppUser{}, false, err
	}
	return code, user, true, nil
}

// challenge handles POST /recover/challenge and returns the options for
// navigator.credentials.create as JSON. A code that cannot be used gets a 422
// with no body. The page's script then submits the form, so POST
// /recover/passkey renders the response the person reads.
func (h *recoverAccount) challenge(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	_, user, ok, err := h.lookUp(r)
	if err != nil {
		serverError(h.logger, w, r, "start the registration", err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	held, err := h.queries.ListPasskeys(r.Context(), user.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the passkeys", err)
		return
	}
	creation, cookie, err := h.passkeys.BeginRegistration(r.Context(), h.now(), user, held)
	if err != nil {
		serverError(h.logger, w, r, "start the registration", err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(h.logger, w, r, creation)
}

// register handles POST /recover/passkey, the post holding the credential the
// browser created. Marking the code used and saving the passkey are one
// transaction, so a refused registration leaves the code unused. A code an
// earlier post already used saves no passkey. Success redirects to sign in
// and starts no session.
func (h *recoverAccount) register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	// The ceremony cookie is cleared whatever the outcome, so one challenge
	// cannot be answered twice.
	http.SetCookie(w, h.passkeys.ClearedCeremonyCookie())
	code, ok := auth.CanonicalRecoveryCode(r.PostForm.Get(codeField))
	if !ok {
		h.renderCannotBeUsed(w, r)
		return
	}
	body := strings.NewReader(r.PostForm.Get(credentialField))
	name := browserName(userAgentOf(r))
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		userID, err := q.RedeemRecoveryCode(ctx, h.now(), auth.HashToken(code))
		if errors.Is(err, pgx.ErrNoRows) {
			return errCodeUsed
		}
		if err != nil {
			return err
		}
		user, err := q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		_, err = h.passkeys.WithQueries(q).FinishRegistration(ctx, h.now(), r, user, body, name)
		return err
	})
	if errors.Is(err, errCodeUsed) {
		h.renderCannotBeUsed(w, r)
		return
	}
	if err != nil {
		page := addDevicePage(code)
		page.Refusal = registrationRefusal(h.logger, w, r, err, "register the passkey")
		if page.Refusal == "" {
			return
		}
		h.templates.render(w, r, view{page: "recover", status: http.StatusUnprocessableEntity}, page)
		return
	}
	http.Redirect(w, r, signInPath, http.StatusSeeOther)
}

// renderCannotBeUsed renders the "That code cannot be used" page with a 422.
func (h *recoverAccount) renderCannotBeUsed(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "recover", status: http.StatusUnprocessableEntity}, turnedAwayPage(codeCannotBeUsedTitle, codeCannotBeUsedLine))
}

// tooManyCodes renders the Too many attempts page with a 429. It is the response
// to a POST /recover or a POST /recover/passkey past the rate limit. Both are
// ordinary form posts, so a person reads the response.
func (h *recoverAccount) tooManyCodes(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "recover", status: http.StatusTooManyRequests}, turnedAwayPage(tooManyAttemptsTitle, tooManyAttemptsLine))
}

// tooManyRecoveryChallenges is the response to a POST /recover/challenge past
// the rate limit. The line is plain text, because the page's script fetches
// this URL and puts the body above the button.
func tooManyRecoveryChallenges(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, tooManyAttemptsTitle+". "+tooManyAttemptsLine, http.StatusTooManyRequests)
}

// recoverLimits are the two rate limiters shared by the three routes that
// take a recovery code. They are one pair rather than a pair per route,
// because each of the three responds differently to an unused code, so a
// limit per route would allow three times the guesses.
//
// Eight a minute from one address is a recovery and two retries after a
// refused device, at three posts for the first and two for each retry. The
// shared limit of ten a minute can be this tight because these routes are
// used a handful of times in an account's life, and at ten guesses a minute a
// sixty-bit code is out of reach.
type recoverLimits struct {
	perAddress      *Limiter
	shared          *Limiter
	trustedIPHeader string
}

func newRecoverLimits(trustedIPHeader string) recoverLimits {
	return recoverLimits{
		perAddress:      NewLimiter(rate.Every(time.Minute/8), 8),
		shared:          NewLimiter(rate.Every(time.Minute/10), 10),
		trustedIPHeader: trustedIPHeader,
	}
}

// around returns the middleware for one route, with refused as the handler
// that responds to a request past either limit.
func (l recoverLimits) around(refused http.Handler) []middleware {
	return []middleware{
		Limit(l.perAddress, ClientAddress(l.trustedIPHeader), refused),
		Limit(l.shared, AnySource, refused),
	}
}
