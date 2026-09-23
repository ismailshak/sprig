package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	// setupPath is the URL of the Set up your garden page. The form on it
	// posts to the same URL.
	setupPath = "/setup"
	// setupChallengePath is the URL the page's script posts the form's fields
	// to for a registration challenge. It then runs
	// navigator.credentials.create and posts the form to setupPath.
	setupChallengePath = setupPath + "/challenge"
)

// seededCareTypes are the care types every new garden starts with, in the
// order the Garden page lists them.
var seededCareTypes = [...]struct{ name, slug string }{
	{"Water", "water"},
	{"Feed", "feed"},
	{"Repot", "repot"},
}

// The message shown under a field that was posted empty.
const (
	setupGardenMissing = "Enter a name for your garden."
	setupNameMissing   = "Enter a display name."
	zoneMissing        = "Choose a timezone."
)

// errSetupClosed is returned inside the transaction that creates the garden
// when sign-up is off and an account already exists. Another request has taken
// the one registration such an install allows.
var errSetupClosed = errors.New("an account exists and sign-up is off")

type setupForm struct {
	garden string
	name   string
	// handle is the Handle field in its stored form. It is empty when the field
	// was left blank, and the account then gets the handle made from its
	// display name.
	handle string
	zone   string
}

func readSetupForm(r *http.Request) setupForm {
	return setupForm{
		garden: strings.TrimSpace(r.PostForm.Get("garden")),
		name:   strings.TrimSpace(r.PostForm.Get("name")),
		handle: store.NormaliseHandle(r.PostForm.Get("handle")),
		zone:   r.PostForm.Get("timezone"),
	}
}

// handleOrDefault is the handle the account is created with: the one typed,
// or the display name's when the field was left blank.
func (f setupForm) handleOrDefault() string {
	if f.handle != "" {
		return f.handle
	}
	return store.HandleFor(f.name)
}

// errors returns the message to show under each empty field, and "" for a
// field that was filled in. A zone that is not on the select gets no message
// here, because no select produces one. The caller returns 400 for that.
func (f setupForm) errors() (garden, name, zone string) {
	if f.garden == "" {
		garden = setupGardenMissing
	}
	if f.name == "" {
		name = setupNameMissing
	}
	if f.zone == "" {
		zone = zoneMissing
	}
	return garden, name, zone
}

func (f setupForm) valid() bool {
	garden, name, zone := f.errors()
	return garden == "" && name == "" && zone == ""
}

type setupPage struct {
	Garden      string
	GardenError string
	Name        string
	NameError   string
	Handle      string
	// HandleError is shown under Handle when another account already holds
	// the one typed. A blank handle is not an error here, because the display
	// name's handle is used instead.
	HandleError string
	// Suggest is the URL the page's script GETs a free handle from.
	Suggest string
	Zone    timezoneField
	// Refusal is the message above the form saying why the device was not
	// enrolled. It is empty unless a registration has been refused.
	Refusal string
	// Action is the URL the form posts to.
	Action string
	// Challenge is the URL the page's script posts the form to for a
	// registration challenge.
	Challenge string
	// Field is the name of the hidden input the browser's credential goes in. It
	// is rendered on the input and again as the form's data-field, so the
	// script does not have the name written into it.
	Field string
	// SignIn is the URL the "Sign in to set up a garden as yourself" link
	// under the form points at. Signing in there redirects to the page for
	// setting up a garden as the account signed in. It is empty with sign-up
	// off. The template renders no link then, because that page is a 404 on
	// such an install.
	SignIn string
}

// newSetupPage fills the page from form. propose is true on a form nobody has
// posted yet, so the page's script selects the browser's own zone. It is
// false when re-rendering a refused post, so the zone the person chose stays.
// signupOn is SPRIG_SIGNUP_ENABLED.
func newSetupPage(form setupForm, propose, signupOn bool) setupPage {
	page := setupPage{
		Garden:    form.garden,
		Name:      form.name,
		Handle:    form.handle,
		Suggest:   handlePath,
		Zone:      timezoneField{Zones: zoneOptions(form.zone), Propose: propose},
		Action:    setupPath,
		Challenge: setupChallengePath,
		Field:     credentialField,
	}
	if signupOn {
		page.SignIn = signInToSetUpPath
	}
	return page
}

// setup serves Set up your garden: the page, the registration challenge its
// script asks for, and the post that creates the account, the garden, the
// owner's membership, the three care types and the passkey, then signs in. It
// also serves /setup/signed-in, where an account that is already signed in
// sets up a garden of its own.
type setup struct {
	logger   *slog.Logger
	passkeys *auth.Passkeys
	sessions *auth.Sessions
	// resolver reads the session cookie on a request, so GET /setup can tell a
	// signed-in browser from a stranger.
	resolver  Resolver
	queries   *store.Queries
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
	// enabled is SPRIG_SIGNUP_ENABLED: whether a stranger may create an
	// account and a garden of their own.
	enabled bool
	wake    wakeJobs
	// pushKey is the VAPID public key the Reminders page gives the browser to
	// subscribe with. It is empty when push is off, and the page then sends
	// the browser on to Today.
	pushKey string
}

// open reports whether the three public setup routes are served. They are when
// sign-up is on, and on an install with no account whatever the flag says,
// because a new install with sign-up off would otherwise have no way to make
// its first account. A closed route is a 404, so the response gives nothing
// away about the install.
func (h *setup) open(r *http.Request) (bool, error) {
	if h.enabled {
		return true, nil
	}
	any, err := h.queries.AnyUsers(r.Context())
	return !any, err
}

// show handles GET /setup. A browser that opens the page while signed in is
// redirected to /setup/signed-in, because the form here would make a second
// account for a person who has one.
func (h *setup) show(w http.ResponseWriter, r *http.Request) {
	if open, err := h.open(r); err != nil {
		h.templates.serverError(h.logger, w, r, "open the setup page", err)
		return
	} else if !open {
		h.templates.notFound(w, r)
		return
	}
	if hasSession(w, r, h.sessions, h.resolver, h.now()) {
		http.Redirect(w, r, setupSignedInPath, http.StatusSeeOther)
		return
	}
	h.templates.render(w, r, view{page: "setup"}, newSetupPage(setupForm{}, true, h.enabled))
}

// challenge handles POST /setup/challenge and returns the options for
// navigator.credentials.create as JSON. The form's fields are posted with the
// request, because the handle and display name go into the challenge and the
// browser stores them with the passkey. A form with an empty field is refused
// here with a 422 and no body. The script then posts the form as it is, so the
// messages under the fields come from the post and no passkey is made for a
// form the post would refuse.
func (h *setup) challenge(w http.ResponseWriter, r *http.Request) {
	if open, err := h.open(r); err != nil {
		h.templates.serverError(h.logger, w, r, "start the setup", err)
		return
	} else if !open {
		h.templates.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	form := readSetupForm(r)
	if !form.valid() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if !slices.Contains(zones, form.zone) {
		h.templates.badRequest(w, r)
		return
	}
	// The handle is checked here as well as on the post, so the browser is not
	// asked to make a passkey the post will then refuse.
	if taken, err := handleTaken(r.Context(), h.queries, form.handle); err != nil {
		h.templates.serverError(h.logger, w, r, "start the setup", err)
		return
	} else if taken {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	// The account has no row yet. Its id is chosen here and stored with the
	// ceremony, so the row the post writes has the id the passkey was
	// registered under.
	account := store.AppUser{ID: uuid.NewV7(), DisplayName: form.name, Handle: form.handleOrDefault(), Timezone: form.zone}
	creation, cookie, err := h.passkeys.BeginSetup(r.Context(), h.now(), account)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "start the setup", err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(h.templates, h.logger, w, r, creation)
}

// create handles POST /setup. The account, the garden, the membership, the
// care types and the passkey are written in one transaction, so a refused
// registration rolls all of them back. A post that goes through signs in and
// redirects to the Reminders page.
func (h *setup) create(w http.ResponseWriter, r *http.Request) {
	if open, err := h.open(r); err != nil {
		h.templates.serverError(h.logger, w, r, "set up the garden", err)
		return
	} else if !open {
		h.templates.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	form := readSetupForm(r)
	if form.zone != "" && !slices.Contains(zones, form.zone) {
		h.templates.badRequest(w, r)
		return
	}
	page := newSetupPage(form, false, h.enabled)
	page.GardenError, page.NameError, page.Zone.Error = form.errors()
	if !form.valid() {
		h.templates.render(w, r, view{page: "setup", status: http.StatusUnprocessableEntity}, page)
		return
	}
	if taken, err := handleTaken(r.Context(), h.queries, form.handle); err != nil {
		h.templates.serverError(h.logger, w, r, "set up the garden", err)
		return
	} else if taken {
		page.HandleError = handleTakenMessage(form.handle)
		h.templates.render(w, r, view{page: "setup", status: http.StatusUnprocessableEntity}, page)
		return
	}

	http.SetCookie(w, h.passkeys.ClearedCeremonyCookie())
	ceremony, err := h.passkeys.TakeSetup(r.Context(), h.now(), r)
	if err != nil {
		h.refuse(w, r, page, err)
		return
	}

	body := strings.NewReader(r.PostForm.Get(credentialField))
	// WebAuthn returns no name for a device, so the passkey is named after the
	// browser the registration came from.
	name := browserName(userAgentOf(r))
	var (
		user       store.AppUser
		membership store.Membership
		passkey    store.PasskeyCredential
	)
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		ctx := r.Context()
		if !h.enabled {
			// The same check ran outside the transaction. It runs again
			// here under a lock, so two posts arriving at once cannot
			// both create the first account.
			if err := q.LockUsers(ctx); err != nil {
				return err
			}
			any, err := q.AnyUsers(ctx)
			if err != nil {
				return err
			}
			if any {
				return errSetupClosed
			}
		}
		var err error
		if form.handle != "" {
			user, err = q.CreateAccountWithHandle(ctx, ceremony.AccountID, form.name, form.handle, form.zone)
		} else {
			user, err = q.CreateAccount(ctx, ceremony.AccountID, form.name, form.zone)
		}
		if err != nil {
			return err
		}
		_, membership, err = createGardenOwnedBy(ctx, q, form.garden, user.ID)
		if err != nil {
			return err
		}
		passkey, err = h.passkeys.WithQueries(q).FinishSetup(ctx, ceremony, user, body, name)
		return err
	})
	if errors.Is(err, errSetupClosed) {
		h.templates.notFound(w, r)
		return
	}
	// The handle was free when the challenge and the post checked it, and
	// another account took it between the check and the write. The passkey the
	// browser made is not saved, since the transaction rolled back.
	if errors.Is(err, store.ErrHandleTaken) {
		page.HandleError = handleTakenMessage(form.handle)
		h.templates.render(w, r, view{page: "setup", status: http.StatusUnprocessableEntity}, page)
		return
	}
	if err != nil {
		h.refuse(w, r, page, err)
		return
	}

	now := h.now()
	// A browser that was already signed in gets a new session in place of the
	// one it arrived with.
	if err := h.sessions.DeleteFromRequest(r.Context(), r); err != nil {
		h.templates.serverError(h.logger, w, r, "end the previous session", err)
		return
	}
	token, _, err := h.sessions.Create(r.Context(), now, user.ID, &membership.GardenID, &passkey.ID, r.UserAgent())
	if err != nil {
		h.templates.serverError(h.logger, w, r, "start the session", err)
		return
	}
	http.SetCookie(w, h.sessions.Cookie(token))
	http.Redirect(w, r, remindersPath, http.StatusSeeOther)
}

// handleTaken reports whether the handle typed on Set up your garden or an
// invite's join form belongs to an account already, open or closed. A blank
// field is never taken, because the account then gets a handle made from its
// display name.
func handleTaken(ctx context.Context, queries *store.Queries, handle string) (bool, error) {
	if handle == "" {
		return false, nil
	}
	return queries.HandleExists(ctx, handle)
}

// createGardenOwnedBy writes a garden named name, an owner membership of it
// for userID, and the three care types every garden starts with.
func createGardenOwnedBy(ctx context.Context, q *store.Queries, name string, userID uuid.UUID) (store.Garden, store.Membership, error) {
	garden, err := q.CreateGarden(ctx, name)
	if err != nil {
		return store.Garden{}, store.Membership{}, err
	}
	membership, err := createMembership(ctx, q, newMembership{GardenID: garden.ID, UserID: userID, Role: "owner"})
	if err != nil {
		return store.Garden{}, store.Membership{}, err
	}
	for _, care := range seededCareTypes {
		if _, err := q.CreateCareType(ctx, store.CreateCareTypeParams{GardenID: garden.ID, Name: care.name, Slug: care.slug}); err != nil {
			return store.Garden{}, store.Membership{}, err
		}
	}
	return garden, membership, nil
}

// refuse renders the page again as a 422, with the fields as they were posted
// and the reason the device was not enrolled above the form. A post with no
// credential in it gets a 400 instead, and anything else a 500.
func (h *setup) refuse(w http.ResponseWriter, r *http.Request, page setupPage, err error) {
	page.Refusal = registrationRefusal(h.templates, h.logger, w, r, err, "set up the garden")
	if page.Refusal == "" {
		return
	}
	h.templates.render(w, r, view{page: "setup", status: http.StatusUnprocessableEntity}, page)
}
