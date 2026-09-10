package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// schedulePath is the URL for editing one care type's schedule. A GET renders
// the editor and a POST saves. The care type is named by slug so renaming one
// does not change the URL.
func schedulePath(plantID uuid.UUID, slug string) string {
	return plantPath(plantID) + "/schedule/" + slug
}

// removeSchedulePath is the URL for removing a schedule. A GET renders the
// confirmation and a POST removes.
func removeSchedulePath(plantID uuid.UUID, slug string) string {
	return schedulePath(plantID, slug) + "/remove"
}

// scheduleEditor is the data for a schedule row while it is being edited. The
// controls are shared with the add-plant form. The buttons are this page's
// own, because a row here saves on its own and a row there is one field of a
// larger form.
type scheduleEditor struct {
	scheduleField
	// Cancel is the URL the Cancel link points at. It goes back to the plant's page.
	Cancel string
	// Remove is the URL the Remove link points at. Empty when there is no
	// schedule to remove.
	Remove string
	// Asking is true once Remove has been clicked and the row is showing the
	// confirmation.
	Asking bool
}

// editSchedule handles GET /plants/{plant}/schedule/{care}. It renders the
// plant's page with that row open as an editor, on the schedule the plant has
// or on a weekly cadence if it has none.
func (h *plants) editSchedule(w http.ResponseWriter, r *http.Request) {
	h.editor(w, r, false)
}

// confirmRemoveSchedule handles GET /plants/{plant}/schedule/{care}/remove. It
// renders the same editor with the remove confirmation showing.
func (h *plants) confirmRemoveSchedule(w http.ResponseWriter, r *http.Request) {
	h.editor(w, r, true)
}

// editor renders the editor for a GET. An htmx request gets the row alone,
// since that is the only element that changes.
func (h *plants) editor(w http.ResponseWriter, r *http.Request, asking bool) {
	principal := PrincipalFrom(r)
	detail, care, ok := h.plantAndCare(w, r, principal)
	if !ok {
		return
	}
	draft, ok := scheduleDraftFor(r.URL.Query(), detail, care)
	if !ok {
		h.templates.badRequest(w, r)
		return
	}

	page := newPlantPage(principal, detail)
	row := page.rowFor(care.Slug)
	if row == nil {
		h.templates.notFound(w, r)
		return
	}
	row.Edit = newScheduleEditor(draft, detail, asking)
	h.renderRow(w, r, page, *row, 0)
}

// saveSchedule handles POST /plants/{plant}/schedule/{care}. Invalid input
// re-renders the editor with a message under the field it concerns.
func (h *plants) saveSchedule(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	detail, care, ok := h.plantAndCare(w, r, principal)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	draft, ok := scheduleDraftFor(r.PostForm, detail, care)
	if !ok {
		h.templates.badRequest(w, r)
		return
	}

	params, message := draft.upsert(principal.Garden.ID, detail.plant.ID)
	if message != "" {
		page := newPlantPage(principal, detail)
		row := page.rowFor(care.Slug)
		if row == nil {
			h.templates.notFound(w, r)
			return
		}
		row.Edit = newScheduleEditor(draft, detail, false)
		row.Edit.Error = message
		h.renderRow(w, r, page, *row, http.StatusUnprocessableEntity)
		return
	}
	if _, err := h.queries.UpsertCareSchedule(r.Context(), params); err != nil {
		h.templates.serverError(h.logger, w, r, "save the schedule", err)
		return
	}
	h.settledRow(w, r, principal, detail.plant.ID, care.Slug, false)
}

// removeSchedule handles POST /plants/{plant}/schedule/{care}/remove. The
// events the schedule produced are kept, so the care type goes back to
// unscheduled without losing its history.
func (h *plants) removeSchedule(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	detail, care, ok := h.plantAndCare(w, r, principal)
	if !ok {
		return
	}
	_, err := h.queries.DeleteCareSchedule(r.Context(), store.DeleteCareScheduleParams{
		GardenID:   principal.Garden.ID,
		PlantID:    detail.plant.ID,
		CareTypeID: care.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "remove the schedule", err)
		return
	}
	h.settledRow(w, r, principal, detail.plant.ID, care.Slug, true)
}

// plantAndCare resolves the plant and care type from the URL for every editor
// handler. It writes the response itself and returns false when there is
// nothing to edit. An archived plant is a 404 like a plant the garden does not
// have, since its page has no schedule to change.
func (h *plants) plantAndCare(w http.ResponseWriter, r *http.Request, principal auth.Principal) (plantDetail, store.CareType, bool) {
	var none store.CareType
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return plantDetail{}, none, false
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return plantDetail{}, none, false
	case err != nil:
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return plantDetail{}, none, false
	}
	if detail.plant.ArchivedAt != nil {
		h.templates.notFound(w, r)
		return plantDetail{}, none, false
	}
	care, ok := careFor(detail.cares, r.PathValue("care"))
	if !ok {
		h.templates.notFound(w, r)
		return plantDetail{}, none, false
	}
	return detail, care, true
}

// renderRow renders the one row for an htmx request and the whole page
// otherwise. A status of zero means 200. A swap announces a refused save's
// message and the remove confirmation's question. An editor opening is not
// announced, because focus moves into its first field and that is read out.
func (h *plants) renderRow(w http.ResponseWriter, r *http.Request, page plantPage, row scheduleRow, status int) {
	v := view{page: "plant", status: status}
	if isHTMX(r) {
		page.Schedule = []scheduleRow{row}
		v.fragment = scheduleRowsFragment
		switch {
		case row.Edit != nil && row.Edit.Error != "":
			v.announce = row.Edit.Error
		case row.Edit != nil && row.Edit.Asking:
			v.announce = "Remove this schedule? Cancel or Remove."
		}
	}
	h.templates.render(w, r, v, page)
}

// settledRow renders a row after a save or remove. It re-reads the row from
// the database so the due date shown is the server's, not the browser's.
// Without JavaScript it redirects to the plant's page instead. removed is
// true when the schedule was removed rather than saved. It picks the sentence
// the swap announces.
func (h *plants) settledRow(w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID, slug string, removed bool) {
	if !isHTMX(r) {
		http.Redirect(w, r, plantPath(plantID), http.StatusSeeOther)
		return
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return
	}
	page := newPlantPage(principal, detail)
	row := page.rowFor(slug)
	if row == nil {
		h.templates.notFound(w, r)
		return
	}
	page.Schedule = []scheduleRow{*row}
	announce := row.Care + " schedule saved. " + row.When + "."
	if removed {
		announce = row.Care + " schedule removed."
	}
	v := view{page: "plant", fragment: scheduleRowsFragment, announce: announce}
	h.templates.render(w, r, v, page)
}

// newScheduleEditor builds the row's editor. Remove is set only when there is
// a schedule to remove.
func newScheduleEditor(f scheduleDraft, d plantDetail, asking bool) *scheduleEditor {
	editor := &scheduleEditor{
		scheduleField: newScheduleField(f, schedulePath(d.plant.ID, f.care.Slug), d.now),
		Cancel:        plantPath(d.plant.ID),
		Asking:        asking,
	}
	if _, ok := scheduledLine(d.lines, f.care.Slug); ok {
		editor.Remove = removeSchedulePath(d.plant.ID, f.care.Slug)
	}
	return editor
}

// scheduleDraftFor builds the editor's draft: the plant's current schedule
// with any values the row's controls sent applied over it. A request with no
// values is the row being opened.
func scheduleDraftFor(values url.Values, d plantDetail, care store.CareType) (scheduleDraft, bool) {
	f := newScheduleDraft(care, d.now)
	if line, ok := scheduledLine(d.lines, care.Slug); ok {
		f = scheduleDraftOf(care, line.Schedule, d.now)
	}
	f.open = true
	if len(values) == 0 {
		return f, true
	}
	return f.fill(values, d.now)
}

// scheduledLine returns the plant's schedule for a care type, or false if it
// has none. A one-off that has been done counts as none: its row reads "Not
// scheduled", the editor opens on a weekly cadence with nothing to remove, and
// the next save replaces the spent row.
func scheduledLine(lines []schedule.Line, slug string) (schedule.Line, bool) {
	line, ok := lineFor(lines, slug)
	if !ok || line.State == schedule.Spent {
		return schedule.Line{}, false
	}
	return line, true
}

func careFor(cares []store.CareType, slug string) (store.CareType, bool) {
	for _, care := range cares {
		if care.Slug == slug {
			return care, true
		}
	}
	return store.CareType{}, false
}

// scheduleRowsFragment is the template that renders the Schedule section's
// rows, and the fragment returned for a swap targeting one of them.
const scheduleRowsFragment = "schedule-rows"

// scheduleRowPrefix starts every schedule row id, so a swap target names the
// care type its row is for.
const scheduleRowPrefix = "sched-"

// scheduleRowID is the HTML id of a care type's schedule row. It uses the slug
// rather than the name so renaming a care type does not change it.
func scheduleRowID(care store.CareType) string {
	return scheduleRowPrefix + care.Slug
}

// scheduleRowSlug returns the care type slug a swap target names, or false if
// the target is not a schedule row.
func scheduleRowSlug(target string) (string, bool) {
	return strings.CutPrefix(target, scheduleRowPrefix)
}
