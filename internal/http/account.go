package http

import (
	"net/http"
	"slices"
	"strings"

	"github.com/ismailshak/sprig/internal/store"
)

// zones is the timezone select's options. The zone decides which day the app
// calls due today, so it is asked for rather than read off the browser. A
// browser answering from wherever it is that week would move every due date by
// a day with nobody having chosen it.
var zones = []string{
	"Europe/London", "Europe/Dublin", "Europe/Paris", "Europe/Berlin",
	"America/New_York", "America/Chicago", "America/Los_Angeles",
	"Asia/Tokyo", "Asia/Singapore", "Australia/Sydney",
}

// nameMissing is shown under Display name when the field is posted empty.
const nameMissing = "Give a display name. Every row in the log is signed with it."

type accountPage struct {
	Name string
	// NameError is shown under Display name, empty when the form is valid.
	NameError string
	Zones     []option
}

func (h *more) account(w http.ResponseWriter, r *http.Request) {
	user := PrincipalFrom(r).User
	h.templates.render(w, r, view{page: "account"}, newAccountPage(user.DisplayName, user.Timezone, user.Timezone))
}

func (h *more) saveAccount(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	zone := r.PostForm.Get("timezone")
	if !slices.Contains(zonesFor(principal.User.Timezone), zone) {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}

	if name == "" {
		page := newAccountPage(name, zone, principal.User.Timezone)
		page.NameError = nameMissing
		h.templates.render(w, r, view{page: "account", status: http.StatusUnprocessableEntity}, page)
		return
	}

	params := store.UpdateAccountParams{DisplayName: name, Timezone: zone, UserID: principal.User.ID}
	if err := h.queries.UpdateAccount(r.Context(), params); err != nil {
		serverError(h.logger, w, r, "save the account", err)
		return
	}
	http.Redirect(w, r, accountPath, http.StatusSeeOther)
}

// newAccountPage builds the form. held is the zone on the account. It is an
// option even when it is not one of the ten, so opening the page and saving
// moves nobody to a zone they did not choose.
func newAccountPage(name, selected, held string) accountPage {
	page := accountPage{Name: name}
	for _, zone := range zonesFor(held) {
		page.Zones = append(page.Zones, option{Value: zone, Label: zoneLabel(zone), On: zone == selected})
	}
	return page
}

// zonesFor returns the options for an account whose zone is held. A zone the
// list does not have goes last, so the ten stay in the order they are grouped
// in.
func zonesFor(held string) []string {
	if slices.Contains(zones, held) {
		return zones
	}
	return append(slices.Clone(zones), held)
}

// zoneLabel is how a zone reads on the screen: "America/New York", since the
// underscore is a detail of the zone database.
func zoneLabel(zone string) string {
	return strings.ReplaceAll(zone, "_", " ")
}
