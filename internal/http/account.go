package http

import (
	"context"
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
	Bar  topbar
	Name string
	// NameError is shown under Display name, empty when the form is valid.
	NameError string
	Handle    string
	// HandleError is shown under Handle, empty when the form is valid.
	HandleError string
	Zone        timezoneField
	// Codes is the row at the bottom of the page. It links to Recovery codes.
	// That page is reached from Account rather than from More, because every
	// other page under More is about the garden.
	Codes linkRow
	// Close is the URL of the Close account page, linked at the bottom.
	Close string
}

func (h *more) account(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	user := principal.User
	held := accountForm{name: user.DisplayName, handle: user.Handle, zone: user.Timezone}
	page, err := h.newAccountPage(r.Context(), principal, held)
	if err != nil {
		serverError(h.logger, w, r, "open the account", err)
		return
	}
	h.templates.render(w, r, view{page: "account"}, page)
}

func (h *more) saveAccount(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		badRequest(w)
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
		badRequest(w)
		return
	}

	// The page is built before the update, because both refusals below
	// re-render it: an empty field, and the unique index rejecting the handle.
	// A save that goes through redirects and throws it away.
	page, err := h.newAccountPage(r.Context(), principal, form)
	if err != nil {
		serverError(h.logger, w, r, "save the account", err)
		return
	}
	page.NameError = nameErrorFor(form.name)
	page.HandleError = handleErrorFor(form.handle)
	if page.NameError != "" || page.HandleError != "" {
		h.templates.render(w, r, view{page: "account", status: http.StatusUnprocessableEntity}, page)
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
		h.templates.render(w, r, view{page: "account", status: http.StatusUnprocessableEntity}, page)
		return
	case err != nil:
		serverError(h.logger, w, r, "save the account", err)
		return
	}
	// The digest hour is read in the timezone just saved.
	h.wake.call()
	http.Redirect(w, r, accountPath, http.StatusSeeOther)
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
// page is opened, and the posted values when a save was refused.
func (h *more) newAccountPage(ctx context.Context, principal auth.Principal, form accountForm) (accountPage, error) {
	page := accountPage{
		Bar:    moreBar("Account"),
		Name:   form.name,
		Handle: form.handle,
		Codes:  linkRow{Label: "Recovery codes", Href: recoveryPath},
		Close:  closeAccountPath,
	}
	page.Zone.Zones = zoneOptions(form.zone)

	if principal.Can(auth.MemberManage) {
		batch, live, err := h.recoveryBatch(ctx, principal.User.ID)
		if err != nil {
			return accountPage{}, err
		}
		switch {
		case !live:
			page.Codes.Note = accountCodesNote
		case batch.Unused == 0:
			page.Codes.Note = accountCodesNoneLeftNote
		}
	}
	return page, nil
}
