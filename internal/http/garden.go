package http

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

// careTypesPath is the URL of the garden's care types. A GET renders the
// Garden page with an empty row at the end of the list for a care type that
// does not exist yet, and a POST creates one.
const careTypesPath = gardenPath + "/types"

// careTypePath is the URL of one care type. A GET renders the Garden page with
// that row open as an editor and a POST renames it. The care type is named by
// slug, which a rename does not change.
func careTypePath(slug string) string {
	return careTypesPath + "/" + slug
}

func offCareTypePath(slug string) string {
	return careTypePath(slug) + "/off"
}

func onCareTypePath(slug string) string {
	return careTypePath(slug) + "/on"
}

func deleteCareTypePath(slug string) string {
	return careTypePath(slug) + "/delete"
}

// The messages shown under a name field when a form is refused.
const (
	gardenNameMissing   = "Give the garden a name. It is what the top of Today says."
	careTypeNameMissing = "Give the care type a name."
	// A name of punctuation alone leaves nothing to build a slug from, and the
	// slug is what every schedule and every URL refers to the type by.
	careTypeNameUnusable = "Give it a name with a letter or a number in it."
)

// careTypeNameTaken is shown when the name would collide with a care type the
// garden already has. It names that type, since one that has been turned off
// is still in the list and is easy to miss.
func careTypeNameTaken(name string) string {
	return "There is already a care type called " + name + "."
}

type gardenPage struct {
	Bar topbar
	// Action is the URL the name form posts to.
	Action string
	Name   string
	// NameError is shown under the garden's name, empty when it is valid.
	NameError string
	// EditName is true when the reader may rename the garden. The name form is
	// left off the page when it is false.
	EditName bool
	// ManageTypes is true when the reader may change the care types. The Care
	// types section is left off the page when it is false.
	ManageTypes bool
	// Types is every care type in the garden, the ones that have been turned
	// off included, in the order they were created.
	Types []careTypeRow
	// AddType is the URL the Add a care type link points at.
	AddType string
	// Storage is the sentence under the Photos heading, saying how much of the
	// garden's photo storage is used.
	Storage string
}

// careTypeRow is one care type in the list. A closed row is a link to the care
// type's own URL. Editor is set on the one row that is open, and that row
// renders as a form instead.
type careTypeRow struct {
	Name string
	// Off is true for a care type that has been turned off. Its row is greyed
	// and says Off, and the type is out of the scheduler and out of the sheet.
	Off bool
	// Edit is the URL that opens this row as an editor.
	Edit   string
	Editor *careTypeEditor
}

// careTypeEditor is a row while it is being edited: the name in a text field,
// the sentence saying what can be done to this type, and the buttons.
type careTypeEditor struct {
	// Action is the URL Save posts to.
	Action string
	// Name is what the field holds: the care type's name, or what was typed
	// and refused.
	Name string
	// Error is shown under the field, empty when the name is valid.
	Error string
	// Why says how many events are recorded against this care type and which
	// of Turn it off and Delete follows from that. It is empty for a care type
	// that does not exist yet.
	Why string
	// Drop is the Turn it off, Turn it back on or Delete button. It is nil for
	// a care type that does not exist yet, which has nothing to remove.
	Drop *dropButton
	// Cancel is the URL the Cancel link points at. It closes the editor and
	// goes back to the Garden page.
	Cancel string
}

// dropButton is the one control in an open row that does something other than
// save it: the word on the button and the URL it posts to.
type dropButton struct {
	Label  string
	Action string
}

// careTypeEdit says which row of the list is open and what its field holds. The
// zero value leaves every row closed.
type careTypeEdit struct {
	// slug names the care type whose row is open.
	slug string
	// adding opens an empty row at the end of the list, for a care type that
	// does not exist yet.
	adding bool
	// name is what the open row's field holds, and message what is shown under
	// it.
	name    string
	message string
}

func (h *more) garden(w http.ResponseWriter, r *http.Request) {
	h.renderGarden(w, r, gardenPage{Name: PrincipalFrom(r).Garden.Name}, careTypeEdit{}, 0)
}

// saveGardenName writes the garden's name. An empty name renders the page
// again with the message under the field.
func (h *more) saveGardenName(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if name == "" {
		h.renderGarden(w, r, gardenPage{NameError: gardenNameMissing}, careTypeEdit{}, http.StatusUnprocessableEntity)
		return
	}
	if err := h.queries.RenameGarden(r.Context(), name, principal.Garden.ID); err != nil {
		serverError(h.logger, w, r, "rename the garden", err)
		return
	}
	http.Redirect(w, r, gardenPath, http.StatusSeeOther)
}

// newCareType handles GET /more/garden/types. It renders the page with an
// empty row at the end of the list.
func (h *more) newCareType(w http.ResponseWriter, r *http.Request) {
	h.renderGarden(w, r, gardenPage{Name: PrincipalFrom(r).Garden.Name}, careTypeEdit{adding: true}, 0)
}

// createCareType handles POST /more/garden/types. The slug is generated from
// the name here and never changes again.
func (h *more) createCareType(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	name, ok := h.postedName(w, r)
	if !ok {
		return
	}
	edit := careTypeEdit{adding: true, name: name}

	slug := store.CareTypeSlug(name)
	if message := nameProblem(name, slug); message != "" {
		edit.message = message
		h.refuseCareType(w, r, edit)
		return
	}
	taken, err := h.careTypeWithSlug(r.Context(), principal.Garden.ID, slug)
	if err != nil {
		serverError(h.logger, w, r, "check the care type's name", err)
		return
	}
	if taken != "" {
		edit.message = careTypeNameTaken(taken)
		h.refuseCareType(w, r, edit)
		return
	}

	params := store.CreateCareTypeParams{GardenID: principal.Garden.ID, Name: name, Slug: slug}
	if _, err := h.queries.CreateCareType(r.Context(), params); err != nil {
		serverError(h.logger, w, r, "create the care type", err)
		return
	}
	http.Redirect(w, r, gardenPath, http.StatusSeeOther)
}

// editCareType handles GET /more/garden/types/{care}. It renders the Garden
// page with that row open as an editor.
func (h *more) editCareType(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	care, ok := h.careTypeFromPath(w, r, principal)
	if !ok {
		return
	}
	h.renderGarden(w, r, gardenPage{Name: principal.Garden.Name}, careTypeEdit{slug: care.Slug, name: care.Name}, 0)
}

// renameCareType handles POST /more/garden/types/{care}. Only the name is
// written, so every schedule and every event the type has keeps pointing at
// it.
func (h *more) renameCareType(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	care, ok := h.careTypeFromPath(w, r, principal)
	if !ok {
		return
	}
	name, ok := h.postedName(w, r)
	if !ok {
		return
	}
	edit := careTypeEdit{slug: care.Slug, name: name}

	slug := store.CareTypeSlug(name)
	if message := nameProblem(name, slug); message != "" {
		edit.message = message
		h.refuseCareType(w, r, edit)
		return
	}
	if slug != care.Slug {
		taken, err := h.careTypeWithSlug(r.Context(), principal.Garden.ID, slug)
		if err != nil {
			serverError(h.logger, w, r, "check the care type's name", err)
			return
		}
		if taken != "" {
			edit.message = careTypeNameTaken(taken)
			h.refuseCareType(w, r, edit)
			return
		}
	}

	params := store.RenameCareTypeParams{Name: name, GardenID: principal.Garden.ID, CareTypeID: care.ID}
	if _, err := h.queries.RenameCareType(r.Context(), params); err != nil {
		serverError(h.logger, w, r, "rename the care type", err)
		return
	}
	http.Redirect(w, r, gardenPath, http.StatusSeeOther)
}

// turnOffCareType handles POST /more/garden/types/{care}/off. The type leaves
// the scheduler and the sheet, and every event recorded against it stays where
// it is.
func (h *more) turnOffCareType(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	care, ok := h.careTypeFromPath(w, r, principal)
	if !ok {
		return
	}
	_, err := h.queries.ArchiveCareType(r.Context(), principal.Garden.ID, care.ID)
	h.afterCareTypeChange(w, r, "turn the care type off", err)
}

// turnOnCareType handles POST /more/garden/types/{care}/on.
func (h *more) turnOnCareType(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	care, ok := h.careTypeFromPath(w, r, principal)
	if !ok {
		return
	}
	_, err := h.queries.RestoreCareType(r.Context(), principal.Garden.ID, care.ID)
	h.afterCareTypeChange(w, r, "turn the care type back on", err)
}

// deleteCareType handles POST /more/garden/types/{care}/delete. A care type
// with an event recorded against it is a 404, because the row that has one
// offers Turn it off instead and never offers this.
func (h *more) deleteCareType(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	care, ok := h.careTypeFromPath(w, r, principal)
	if !ok {
		return
	}
	deleted, err := h.queries.DeleteUnusedCareType(r.Context(), principal.Garden.ID, care.ID)
	if err != nil {
		serverError(h.logger, w, r, "delete the care type", err)
		return
	}
	if deleted == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, gardenPath, http.StatusSeeOther)
}

// afterCareTypeChange finishes turning a care type off or back on. A statement
// that matched nothing was a button the row did not offer, such as turning off
// a type that is already off.
func (h *more) afterCareTypeChange(w http.ResponseWriter, r *http.Request, what string, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
	case err != nil:
		serverError(h.logger, w, r, what, err)
	default:
		http.Redirect(w, r, gardenPath, http.StatusSeeOther)
	}
}

// careTypeFromPath reads the care type the URL names. It writes the response
// itself and returns false when the garden has no type with that slug, whether
// it is on or off.
func (h *more) careTypeFromPath(w http.ResponseWriter, r *http.Request, principal auth.Principal) (store.CareType, bool) {
	care, err := h.queries.GetCareTypeBySlug(r.Context(), principal.Garden.ID, r.PathValue("care"))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return store.CareType{}, false
	case err != nil:
		serverError(h.logger, w, r, "read the care type", err)
		return store.CareType{}, false
	}
	return care, true
}

// postedName reads the name field of one of this page's forms.
func (h *more) postedName(w http.ResponseWriter, r *http.Request) (string, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return "", false
	}
	return strings.TrimSpace(r.PostForm.Get("name")), true
}

// refuseCareType renders the page with the row still open, what was typed
// still in the field and the reason under it.
func (h *more) refuseCareType(w http.ResponseWriter, r *http.Request, edit careTypeEdit) {
	page := gardenPage{Name: PrincipalFrom(r).Garden.Name}
	h.renderGarden(w, r, page, edit, http.StatusUnprocessableEntity)
}

// careTypeWithSlug returns the name of the care type holding slug, and the
// empty string when no type in the garden does. A type that has been turned
// off holds its slug as much as a live one, since its events still refer to
// it.
func (h *more) careTypeWithSlug(ctx context.Context, gardenID uuid.UUID, slug string) (string, error) {
	care, err := h.queries.GetCareTypeBySlug(ctx, gardenID, slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read the care type with the slug: %w", err)
	}
	return care.Name, nil
}

// renderGarden fills in the page's care types and writes it. A status of zero
// means 200.
func (h *more) renderGarden(w http.ResponseWriter, r *http.Request, page gardenPage, edit careTypeEdit, status int) {
	principal := PrincipalFrom(r)
	page.Bar = moreBar("Garden")
	page.Action = gardenPath
	page.AddType = careTypesPath
	page.EditName = principal.Can(auth.GardenEdit)
	page.ManageTypes = principal.Can(auth.CareTypeManage)
	if page.ManageTypes {
		types, err := h.queries.ListCareTypesWithEvents(r.Context(), principal.Garden.ID)
		if err != nil {
			serverError(h.logger, w, r, "list the care types", err)
			return
		}
		page.Types = careTypeRows(types, edit)
	}
	usage, err := h.photos.Usage(r.Context(), h.queries, principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "sum the garden's photos", err)
		return
	}
	page.Storage = storageLine(usage)
	h.templates.render(w, r, view{page: "garden", status: status}, page)
}

// nearlyFull is the share of the quota at which the storage line adds what
// happens when it is full and what to delete. Below it there is nothing to
// act on.
const nearlyFull = 0.9

// storageLine is the sentence under Photos on the Garden page, such as "312 MB
// of 1 GB of photo storage used." From nearlyFull of the quota up it adds
// that uploads stop and that deleting progress photos is what makes room.
func storageLine(usage photo.Usage) string {
	line := storageFigure(usage.Used) + " of " + storageFigure(usage.Quota) + " of photo storage used."
	if float64(usage.Used) < nearlyFull*float64(usage.Quota) {
		return line
	}
	return line + " Uploads stop when it is full, and deleting progress photos is what makes room."
}

// storageFigure formats a byte count as whole megabytes, or as gigabytes from
// 1024 MB up. A gigabyte figure keeps one decimal place unless the number of
// gigabytes is whole. The units are binary and the labels the ordinary ones,
// so the 1 GiB default reads as 1 GB.
func storageFigure(bytes int64) string {
	mb := math.Round(float64(bytes) / (1 << 20))
	if mb < 1024 {
		return strconv.FormatFloat(mb, 'f', 0, 64) + " MB"
	}
	decimals := 1
	if math.Mod(mb, 1024) == 0 {
		decimals = 0
	}
	return strconv.FormatFloat(mb/1024, 'f', decimals, 64) + " GB"
}

// careTypeRows builds the list. The one row edit names is open, and a row
// being added is an extra one at the end.
func careTypeRows(types []store.ListCareTypesWithEventsRow, edit careTypeEdit) []careTypeRow {
	rows := make([]careTypeRow, 0, len(types)+1)
	for _, care := range types {
		row := careTypeRow{
			Name: care.CareType.Name,
			Off:  care.CareType.ArchivedAt != nil,
			Edit: careTypePath(care.CareType.Slug),
		}
		if care.CareType.Slug == edit.slug {
			row.Editor = openCareType(care, edit)
		}
		rows = append(rows, row)
	}
	if edit.adding {
		rows = append(rows, careTypeRow{Editor: &careTypeEditor{
			Action: careTypesPath,
			Name:   edit.name,
			Error:  edit.message,
			Cancel: gardenPath,
		}})
	}
	return rows
}

// openCareType builds the editor for one row. Which of the three buttons the
// row offers is decided here, and the sentence above it says the same thing in
// words, so a person reads why Delete is not on offer before pressing anything
// rather than getting it back as an error afterwards.
func openCareType(care store.ListCareTypesWithEventsRow, edit careTypeEdit) *careTypeEditor {
	slug := care.CareType.Slug
	editor := &careTypeEditor{
		Action: careTypePath(slug),
		Name:   edit.name,
		Error:  edit.message,
		Why:    careTypeWhy(care.Events, care.CareType.ArchivedAt != nil),
		Cancel: gardenPath,
	}
	switch {
	case care.CareType.ArchivedAt != nil:
		editor.Drop = &dropButton{Label: "Turn it back on", Action: onCareTypePath(slug)}
	case care.Events > 0:
		editor.Drop = &dropButton{Label: "Turn it off", Action: offCareTypePath(slug)}
	default:
		editor.Drop = &dropButton{Label: "Delete", Action: deleteCareTypePath(slug)}
	}
	return editor
}

// careTypeWhy is the sentence under the name in an open row: that the care type
// is off, or how many events are recorded against it.
func careTypeWhy(events int64, off bool) string {
	switch {
	case off:
		return "It is off, so it is out of every schedule and out of the sheet. Turning it back on puts it in both."
	case events == 0:
		return "Nothing has been recorded against it yet, so it can go for good."
	}
	return "Recorded " + timesRecorded(events) + ", so it can be renamed or turned off but never deleted — turning it off " +
		"takes it out of schedules and out of the sheet, and leaves every one of those events readable."
}

func timesRecorded(events int64) string {
	if events == 1 {
		return "once"
	}
	return strconv.FormatInt(events, 10) + " times"
}

// nameProblem returns the message shown under the field, and the empty string
// when the name can be stored. slug is what the name generates.
func nameProblem(name, slug string) string {
	switch {
	case name == "":
		return careTypeNameMissing
	case slug == "":
		return careTypeNameUnusable
	}
	return ""
}
