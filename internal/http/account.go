package http

import (
	"cmp"
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// nameMissing is shown under Display name when the field is posted empty.
const nameMissing = "Enter a display name."

// handleMissing is shown under Handle when the field normalises to nothing:
// empty, or with no letter or digit in it.
const handleMissing = "Enter a handle."

// handleTakenMessage is the message shown under Handle when another account
// already has the handle. It repeats the handle, because normalising can
// change what was typed before the save is refused.
func handleTakenMessage(handle string) string {
	return handle + " is already taken."
}

// accountForm is the three values the Account form posts. Opening the page
// fills it from the stored account instead.
type accountForm struct {
	name   string
	handle string
	zone   string
}

type accountPage struct {
	Bar topbar
	// Editing is true when the form is open. When it is false the values are
	// shown as text with an Edit link.
	Editing bool
	// Action is the URL the form posts to, in its action attribute and in
	// hx-post.
	Action string
	// Edit is the URL the Edit link points at. It opens the form.
	Edit string
	// Cancel is the URL the Cancel link points at. It closes the form without
	// saving.
	Cancel string
	Name   string
	// NameError is shown under Display name, empty when the form is valid.
	NameError string
	Handle    string
	// HandleError is shown under Handle, empty when the form is valid.
	HandleError string
	Zone        timezoneField
	// ZoneName is the timezone shown while the form is closed, with
	// underscores as spaces.
	ZoneName string
	// Codes is the row at the bottom of the page. It links to Recovery codes.
	// That page is reached from Account rather than from More, because every
	// other page under More is about the garden.
	Codes linkRow
	// Close is the URL of the Close account page, linked at the bottom.
	Close string
	// Saved is true after a save. "Saved" is shown under the Edit link.
	Saved bool
}

type account struct {
	logger    *slog.Logger
	queries   *store.Queries
	templates *Templates
	wake      wakeJobs
}

// show handles GET /more/account. It renders the account's values as text.
// Cancel swaps the account element with this response.
func (h *account) show(w http.ResponseWriter, r *http.Request) {
	page, err := h.storedAccountPage(r)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "open the account", err)
		return
	}
	page.Saved = saved(r)
	h.renderAccount(w, r, page, 0, "")
}

// edit handles GET /more/account/edit. It renders the page with the account's
// values in the form.
func (h *account) edit(w http.ResponseWriter, r *http.Request) {
	page, err := h.storedAccountPage(r)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "open the account", err)
		return
	}
	page.Editing = true
	h.renderAccount(w, r, page, 0, "")
}

func (h *account) storedAccountPage(r *http.Request) (accountPage, error) {
	principal := PrincipalFrom(r)
	user := principal.User
	return h.newAccountPage(r.Context(), principal, accountForm{name: user.DisplayName, handle: user.Handle, zone: user.Timezone})
}

// accountID is both the HTML id of the element holding the account's values
// and the name of the template that renders it. Edit, Save changes and Cancel
// swap it. Recovery codes and Close account are outside it.
const accountID = "account"

// renderAccount writes the page, or the account element alone for a swap of
// it. A status of zero means 200.
func (h *account) renderAccount(w http.ResponseWriter, r *http.Request, page accountPage, status int, announce string) {
	v := view{page: "account", status: status, announce: announce}
	if r.Header.Get("HX-Target") == accountID {
		v.fragment = accountID
	}
	h.templates.render(w, r, v, page)
}

func (h *account) saveAccount(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	// A handle is normalised rather than rejected for its shape, so "Emma
	// Fletcher" is stored as "emma_fletcher". The field is re-rendered from the
	// normalised value.
	form := accountForm{
		name:   strings.TrimSpace(r.PostForm.Get("name")),
		handle: store.NormaliseHandle(r.PostForm.Get("handle")),
		zone:   r.PostForm.Get("timezone"),
	}
	if !slices.Contains(zonesFor(principal.User.Timezone), form.zone) {
		h.templates.badRequest(w, r)
		return
	}

	// The page is built before the update, because both refusals below
	// re-render it with the form open: an empty field, and the unique index
	// rejecting the handle. A plain post that goes through redirects and throws
	// it away.
	page, err := h.newAccountPage(r.Context(), principal, form)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "save the account", err)
		return
	}
	page.Editing = true
	page.NameError = nameErrorFor(form.name)
	page.HandleError = handleErrorFor(form.handle)
	if page.NameError != "" || page.HandleError != "" {
		h.renderAccount(w, r, page, http.StatusUnprocessableEntity, cmp.Or(page.NameError, page.HandleError))
		return
	}

	params := store.UpdateAccountParams{
		DisplayName: form.name,
		Handle:      form.handle,
		Timezone:    form.zone,
		UserID:      principal.User.ID,
	}
	switch err := h.queries.UpdateAccount(r.Context(), params); {
	case store.HandleTaken(err):
		page.HandleError = handleTakenMessage(form.handle)
		h.renderAccount(w, r, page, http.StatusUnprocessableEntity, page.HandleError)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "save the account", err)
		return
	}
	// The digest hour is read in the timezone just saved.
	h.wake.call()
	// With htmx the response is the account element with the saved values as
	// text and the Saved line under them. A plain post redirects to the page
	// with Saved in the query string.
	if isHTMX(r) {
		page.Editing = false
		page.Saved = true
		h.renderAccount(w, r, page, 0, savedAnnouncement)
		return
	}
	http.Redirect(w, r, savedURL(accountPath), http.StatusSeeOther)
}

func nameErrorFor(name string) string {
	if name == "" {
		return nameMissing
	}
	return ""
}

// handleErrorFor returns the message to show under Handle, or "" when there is
// nothing to say. handle has already been normalised, so an empty string is the
// only thing left to reject.
func handleErrorFor(handle string) string {
	if handle == "" {
		return handleMissing
	}
	return ""
}

// newAccountPage fills the Account page from form: the stored values when the
// page is opened, and the posted values after a save.
func (h *account) newAccountPage(ctx context.Context, principal auth.Principal, form accountForm) (accountPage, error) {
	page := accountPage{
		Bar:      moreBar("Account"),
		Action:   accountPath,
		Edit:     accountEditPath,
		Cancel:   accountPath,
		Name:     form.name,
		Handle:   form.handle,
		ZoneName: zoneLabel(form.zone),
		Codes:    linkRow{Label: "Recovery codes", Href: recoveryPath},
		Close:    closeAccountPath,
	}
	page.Zone.Zones = zoneOptions(form.zone)

	batch, live, err := recoveryBatch(ctx, h.queries, principal.User.ID)
	if err != nil {
		return accountPage{}, err
	}
	switch {
	case !live:
		page.Codes.Note = accountCodesNote
	case batch.Unused == 0:
		page.Codes.Note = accountCodesNoneLeftNote
	}
	return page, nil
}
