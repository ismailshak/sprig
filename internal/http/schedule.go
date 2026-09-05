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

// schedulePath is where one care type's schedule is read as an editor and
// saved. The care type is named by slug because renaming one is free.
func schedulePath(plantID uuid.UUID, slug string) string {
	return plantPath(plantID) + "/schedule/" + slug
}

// removeSchedulePath answers both halves of removing a schedule, a GET asking
// the question and a POST doing it.
func removeSchedulePath(plantID uuid.UUID, slug string) string {
	return schedulePath(plantID, slug) + "/remove"
}

// scheduleEditor is a schedule row open as the small form it is edited in. The
// controls are the add form's, and the foot is this page's, because a row here
// is saved on its own and a row there is one field of a form with its own
// button.
type scheduleEditor struct {
	scheduleField
	// Cancel is the way back to the row at rest, which the plant's own page
	// answers.
	Cancel string
	// Remove is empty for a care type the plant has no schedule for, which is
	// the state Remove leads to.
	Remove string
	// Asking draws the question in place of the foot's three controls.
	Asking bool
}

// editSchedule answers GET /plants/{plant}/schedule/{care} with the plant's
// page, that row open as the editor. It opens on the schedule the plant has,
// and on a weekly cadence for a care type it is not scheduled for.
func (h *plants) editSchedule(w http.ResponseWriter, r *http.Request) {
	h.editor(w, r, false)
}

// confirmRemoveSchedule answers GET /plants/{plant}/schedule/{care}/remove with
// the same editor, its foot carrying the question.
func (h *plants) confirmRemoveSchedule(w http.ResponseWriter, r *http.Request) {
	h.editor(w, r, true)
}

// editor renders the editor from a GET. A swap gets the row alone because that
// is the element that differs between the states.
func (h *plants) editor(w http.ResponseWriter, r *http.Request, asking bool) {
	principal := PrincipalFrom(r)
	detail, care, ok := h.plantAndCare(w, r, principal)
	if !ok {
		return
	}
	draft, ok := scheduleDraftFor(r.URL.Query(), detail, care)
	if !ok {
		http.Error(w, "the editor did not offer that", http.StatusBadRequest)
		return
	}

	page := newPlantPage(principal, detail)
	row := page.rowFor(care.Slug)
	if row == nil {
		http.NotFound(w, r)
		return
	}
	row.Edit = newScheduleEditor(draft, detail, asking)
	h.renderRow(w, r, page, *row, 0)
}

// saveSchedule answers POST /plants/{plant}/schedule/{care}. A row the editor
// cannot take comes back with a sentence under the field it is about.
func (h *plants) saveSchedule(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	detail, care, ok := h.plantAndCare(w, r, principal)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	draft, ok := scheduleDraftFor(r.PostForm, detail, care)
	if !ok {
		http.Error(w, "the editor did not offer that", http.StatusBadRequest)
		return
	}

	params, message := draft.upsert(principal.Garden.ID, detail.plant.ID)
	if message != "" {
		page := newPlantPage(principal, detail)
		row := page.rowFor(care.Slug)
		if row == nil {
			http.NotFound(w, r)
			return
		}
		row.Edit = newScheduleEditor(draft, detail, false)
		row.Edit.Error = message
		h.renderRow(w, r, page, *row, http.StatusUnprocessableEntity)
		return
	}
	if _, err := h.queries.UpsertCareSchedule(r.Context(), params); err != nil {
		serverError(h.logger, w, r, "save the schedule", err)
		return
	}
	h.settledRow(w, r, principal, detail.plant.ID, care.Slug)
}

// removeSchedule answers POST /plants/{plant}/schedule/{care}/remove. The
// events the schedule produced stay where they are, so the care type drops back
// to one the plant is not on rather than losing its history.
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
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "remove the schedule", err)
		return
	}
	h.settledRow(w, r, principal, detail.plant.ID, care.Slug)
}

// plantAndCare resolves the plant and the care type every half of the editor
// acts on. It answers the request itself and reports false where there is nothing to
// edit. An archived plant is as far out of reach as one the garden does not
// have because its page carries no schedule to change.
func (h *plants) plantAndCare(w http.ResponseWriter, r *http.Request, principal auth.Principal) (plantDetail, store.CareType, bool) {
	var none store.CareType
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return plantDetail{}, none, false
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return plantDetail{}, none, false
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return plantDetail{}, none, false
	}
	if detail.plant.ArchivedAt != nil {
		http.NotFound(w, r)
		return plantDetail{}, none, false
	}
	care, ok := careFor(detail.cares, r.PathValue("care"))
	if !ok {
		http.NotFound(w, r)
		return plantDetail{}, none, false
	}
	return detail, care, true
}

// renderRow answers a swap with the one row and a navigation with the whole
// page. A status of zero is 200.
func (h *plants) renderRow(w http.ResponseWriter, r *http.Request, page plantPage, row scheduleRow, status int) {
	v := view{page: "plant", status: status}
	if isHTMX(r) {
		page.Schedule = []scheduleRow{row}
		v.fragment = scheduleRowsFragment
	}
	h.templates.render(w, r, v, page)
}

// settledRow is the row a write left behind, read again so that the due date
// beside the rule is the server's answer rather than the browser's. A browser
// running no script is sent back to the plant instead.
func (h *plants) settledRow(w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID, slug string) {
	if !isHTMX(r) {
		http.Redirect(w, r, plantPath(plantID), http.StatusSeeOther)
		return
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	if err != nil {
		serverError(h.logger, w, r, "load the plant", err)
		return
	}
	page := newPlantPage(principal, detail)
	row := page.rowFor(slug)
	if row == nil {
		http.NotFound(w, r)
		return
	}
	h.renderRow(w, r, page, *row, 0)
}

// newScheduleEditor is the row open as the editor. Remove is drawn where there
// is a schedule to remove, which a care type the plant is not on has not.
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

// scheduleDraftFor is what the editor holds: the schedule the plant has, with
// whatever the row's own controls sent read over it. A request carrying none of
// them is the row being opened.
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

// scheduledLine is the schedule the plant is on for a care type, and false for
// one it is not on. A one-off that has been done is over: its row reads "Not
// scheduled", so the editor opens on a weekly cadence and offers nothing to
// remove, and the next save replaces the spent row.
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

// scheduleRowsFragment is the template that draws the Schedule section's rows,
// and the fragment a swap aimed at one of them is answered with.
const scheduleRowsFragment = "schedule-rows"

// scheduleRowPrefix opens the id of every schedule row, so a swap target names
// the care type its row is about.
const scheduleRowPrefix = "sched-"

// scheduleRowID names the row for the swap that replaces it. It uses the slug
// rather than the name because renaming a care type is free.
func scheduleRowID(care store.CareType) string {
	return scheduleRowPrefix + care.Slug
}

// scheduleRowSlug is the care type a swap target names, and false for a target
// that is not a schedule row.
func scheduleRowSlug(target string) (string, bool) {
	return strings.CutPrefix(target, scheduleRowPrefix)
}
