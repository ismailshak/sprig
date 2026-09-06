package http

import (
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
	setupGardenMissing = "Give the garden a name."
	setupNameMissing   = "Give yourself a name to sign your care with."
	zoneMissing        = "Pick a timezone."
)

// errSetupClosed is returned inside the transaction that creates the garden
// when sign-up is off and an account already exists. Another request has taken
// the one registration such an install allows.
var errSetupClosed = errors.New("an account exists and sign-up is off")

type setupForm struct {
	garden string
	name   string
	zone   string
}

func readSetupForm(r *http.Request) setupForm {
	return setupForm{
		garden: strings.TrimSpace(r.PostForm.Get("garden")),
		name:   strings.TrimSpace(r.PostForm.Get("name")),
		zone:   r.PostForm.Get("timezone"),
	}
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
	Zone        timezoneField
	// Refusal is the message above the form saying why the device was not
	// enrolled. It is empty unless a registration has been refused.
	Refusal string
	// Action is the URL the form posts to.
	Action string
	// Challenge is the URL the page's script posts the form to for a
	// registration challenge.
	Challenge string
	// Field is the name of the hidden input the browser's answer goes in. It
	// is rendered on the input and again as the form's data-field, so the
	// script does not have the name written into it.
	Field string
}

// newSetupPage fills the page from form. propose is true on a form nobody has
// posted yet, so the page's script selects the browser's own zone. It is
// false when re-rendering a refused post, so the zone the person chose stays.
func newSetupPage(form setupForm, propose bool) setupPage {
	return setupPage{
		Garden:    form.garden,
		Name:      form.name,
		Zone:      timezoneField{Zones: zoneOptions(form.zone), Propose: propose},
		Action:    setupPath,
		Challenge: setupChallengePath,
		Field:     credentialField,
	}
}

// setup serves Set up your garden: the page, the registration challenge its
// script asks for, and the post that creates the account, the garden, the
// owner's membership, the three care types and the passkey, then signs in.
type setup struct {
	logger    *slog.Logger
	passkeys  *auth.Passkeys
	sessions  *auth.Sessions
	queries   *store.Queries
	templates *Templates
	// now supplies the current time, so a test can fix the day.
	now func() time.Time
	// enabled is SPRIG_SIGNUP_ENABLED: whether a stranger may create an
	// account and a garden of their own.
	enabled bool
}

// open reports whether the three routes are served. They are when sign-up is
// on, and on an install with no account whatever the flag says, because a new
// install with sign-up off would otherwise have no way to make its first
// account. A closed route is a 404, so the response gives nothing away about
// the install.
func (h *setup) open(r *http.Request) (bool, error) {
	if h.enabled {
		return true, nil
	}
	any, err := h.queries.AnyUsers(r.Context())
	return !any, err
}

// show handles GET /setup.
func (h *setup) show(w http.ResponseWriter, r *http.Request) {
	if open, err := h.open(r); err != nil {
		serverError(h.logger, w, r, "open the setup page", err)
		return
	} else if !open {
		http.NotFound(w, r)
		return
	}
	h.templates.render(w, r, view{page: "setup"}, newSetupPage(setupForm{}, true))
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
		serverError(h.logger, w, r, "start the setup", err)
		return
	} else if !open {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	form := readSetupForm(r)
	if !form.valid() {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if !slices.Contains(zones, form.zone) {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}

	// The account has no row yet. Its id is chosen here and stored with the
	// ceremony, so the row the post writes has the id the passkey was
	// registered under.
	account := store.AppUser{ID: uuid.NewV7(), DisplayName: form.name, Handle: store.HandleFor(form.name), Timezone: form.zone}
	creation, cookie, err := h.passkeys.BeginSetup(r.Context(), h.now(), account)
	if err != nil {
		serverError(h.logger, w, r, "start the setup", err)
		return
	}
	http.SetCookie(w, cookie)
	writeJSON(h.logger, w, r, creation)
}

// create handles POST /setup. The account, the garden, the membership, the
// care types and the passkey are written in one transaction, so a refused
// registration rolls all of them back.
func (h *setup) create(w http.ResponseWriter, r *http.Request) {
	if open, err := h.open(r); err != nil {
		serverError(h.logger, w, r, "set up the garden", err)
		return
	} else if !open {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	form := readSetupForm(r)
	if form.zone != "" && !slices.Contains(zones, form.zone) {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}
	page := newSetupPage(form, false)
	page.GardenError, page.NameError, page.Zone.Error = form.errors()
	if !form.valid() {
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
		user, err = q.CreateAccount(ctx, ceremony.AccountID, form.name, form.zone)
		if err != nil {
			return err
		}
		garden, err := q.CreateGarden(ctx, form.garden)
		if err != nil {
			return err
		}
		membership, err = createMembership(ctx, q, newMembership{GardenID: garden.ID, UserID: user.ID, Role: "owner"})
		if err != nil {
			return err
		}
		for _, care := range seededCareTypes {
			if _, err := q.CreateCareType(ctx, store.CreateCareTypeParams{GardenID: garden.ID, Name: care.name, Slug: care.slug}); err != nil {
				return err
			}
		}
		passkey, err = h.passkeys.WithQueries(q).FinishSetup(ctx, ceremony, user, body, name)
		return err
	})
	if errors.Is(err, errSetupClosed) {
		http.NotFound(w, r)
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

// refuse renders the page again as a 422, with the fields as they were posted
// and the reason the device was not enrolled above the form. A post with no
// credential in it gets a 400 instead, and anything else a 500.
func (h *setup) refuse(w http.ResponseWriter, r *http.Request, page setupPage, err error) {
	page.Refusal = registrationRefusal(h.logger, w, r, err, "Create the garden", "set up the garden")
	if page.Refusal == "" {
		return
	}
	h.templates.render(w, r, view{page: "setup", status: http.StatusUnprocessableEntity}, page)
}
