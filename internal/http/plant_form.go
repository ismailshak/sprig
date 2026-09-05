package http

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// plantFormPageName is the template for both the add and edit routes, since
// they are the same fields in the same order.
const plantFormPageName = "plant-form"

const newPlantPath = plantsPath + "/new"

func editPlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/edit"
}

func archivePlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/archive"
}

// waterSlug is the schedule row the add form opens with, since every plant is
// watered and no other care is that common.
const waterSlug = "water"

// acquiredSpan is how many years the acquired-year select offers, this year
// included.
const acquiredSpan = 21

// referenceFields is the Reference fields in the order the plant's page shows
// them, each with its input name. Water and Feed repeat the care types above
// because a schedule says how often and a reference field says what this plant
// needs.
var referenceFields = [...]struct{ name, label string }{
	{"sun", "Sun"},
	{"water", "Water"},
	{"feed", "Feed"},
	{"soil", "Soil"},
	{"climate", "Climate"},
	{"pot", "Pot"},
}

// plantFields is the plant half of the form's values.
type plantFields struct {
	nickname  string
	common    string
	botanical string
	location  string
	sun       string
	water     string
	feed      string
	soil      string
	climate   string
	pot       string
	notes     string
	// month and year are zero when the select is on its placeholder option.
	month int
	year  int
}

// facts returns a pointer to the field for each of referenceFields, in the same
// order, so reading and rendering the form iterate one list.
func (f *plantFields) facts() [len(referenceFields)]*string {
	return [...]*string{&f.sun, &f.water, &f.feed, &f.soil, &f.climate, &f.pot}
}

// readPlantFields reads the plant half from a query string or a form body. It
// returns false for an acquired month or year that is not one of the options.
func readPlantFields(values url.Values, now time.Time) (plantFields, bool) {
	f := plantFields{
		nickname:  strings.TrimSpace(values.Get("nickname")),
		common:    strings.TrimSpace(values.Get("common")),
		botanical: strings.TrimSpace(values.Get("botanical")),
		location:  strings.TrimSpace(values.Get("where")),
		notes:     strings.TrimSpace(values.Get("notes")),
	}
	for i, held := range f.facts() {
		*held = strings.TrimSpace(values.Get(referenceFields[i].name))
	}

	// A missing select stays at zero, because a schedule row's own request
	// sends that row and none of these fields.
	var ok bool
	if values.Has("acquired-month") {
		if f.month, ok = numberIn(values.Get("acquired-month"), append([]int{0}, months()...)); !ok {
			return f, false
		}
	}
	if values.Has("acquired-year") {
		if f.year, ok = numberIn(values.Get("acquired-year"), append([]int{0}, acquiredYears(now)...)); !ok {
			return f, false
		}
	}
	return f, true
}

func plantFieldsOf(plant store.Plant) plantFields {
	f := plantFields{
		nickname:  value(plant.Nickname),
		common:    value(plant.CommonName),
		botanical: value(plant.BotanicalName),
		location:  value(plant.Location),
		notes:     value(plant.Notes),
	}
	stored := [...]*string{plant.Sun, plant.WaterNeeds, plant.FeedNeeds, plant.Soil, plant.Climate, plant.Pot}
	for i, held := range f.facts() {
		*held = value(stored[i])
	}
	if plant.AcquiredMonth != nil {
		f.month = int(*plant.AcquiredMonth)
	}
	if plant.AcquiredYear != nil {
		f.year = int(*plant.AcquiredYear)
	}
	return f
}

// refuse returns the form's error messages: one under the three names and one
// under Acquired. Both are empty when the post is valid.
func (f plantFields) refuse() (name, acquired string) {
	if f.nickname == "" && f.common == "" && f.botanical == "" {
		name = "Give it at least one name. Any of the three will do."
	}
	// A month with no year is an error rather than dropped silently, since the
	// plant's page shows nothing for it.
	if f.month != 0 && f.year == 0 {
		acquired = "Give the year as well as the month."
	}
	return name, acquired
}

func (f plantFields) create(gardenID uuid.UUID) store.CreatePlantParams {
	return store.CreatePlantParams{
		GardenID:      gardenID,
		Nickname:      set(f.nickname),
		CommonName:    set(f.common),
		BotanicalName: set(f.botanical),
		Location:      set(f.location),
		Sun:           set(f.sun),
		WaterNeeds:    set(f.water),
		FeedNeeds:     set(f.feed),
		Soil:          set(f.soil),
		Climate:       set(f.climate),
		Pot:           set(f.pot),
		Notes:         set(f.notes),
		AcquiredYear:  acquiredPart(f.year),
		AcquiredMonth: acquiredPart(f.month),
	}
}

// update writes the same columns as create, so a field emptied on the form is
// emptied on the row.
func (f plantFields) update(gardenID, plantID uuid.UUID) store.UpdatePlantParams {
	c := f.create(gardenID)
	return store.UpdatePlantParams{
		GardenID:      gardenID,
		PlantID:       plantID,
		Nickname:      c.Nickname,
		CommonName:    c.CommonName,
		BotanicalName: c.BotanicalName,
		Location:      c.Location,
		Sun:           c.Sun,
		WaterNeeds:    c.WaterNeeds,
		FeedNeeds:     c.FeedNeeds,
		Soil:          c.Soil,
		Climate:       c.Climate,
		Pot:           c.Pot,
		Notes:         c.Notes,
		AcquiredYear:  c.AcquiredYear,
		AcquiredMonth: c.AcquiredMonth,
	}
}

// set converts a text field to its column value. An empty field is null, since
// an empty string in the column would count as a value.
func set(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// acquiredPart converts an acquired month or year to its column value, null
// for the placeholder option.
func acquiredPart(n int) *int16 {
	if n == 0 {
		return nil
	}
	return smallint(n)
}

// smallint converts n for a smallint column, null if it is out of range.
func smallint(n int) *int16 {
	if n < math.MinInt16 || n > math.MaxInt16 {
		return nil
	}
	return ptr(int16(n))
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type plantFormPage struct {
	Title string
	// Action is the URL the form posts to and a schedule row re-renders from.
	Action string
	// Back is the URL the Cancel link points at.
	Back   string
	Submit string
	// Archive is the URL of the Archive link at the bottom of the edit form.
	// Empty on the add form and for a reader who may not archive.
	Archive   string
	Nickname  string
	Common    string
	Botanical string
	// NameError is shown under all three name fields, since any one of them
	// satisfies the requirement.
	NameError string
	Location  string
	// Schedules is empty on the edit form. Schedules are edited on the plant's
	// page, beside the due date they change.
	Schedules []scheduleField
	Facts     []factField
	Notes     string
	Months    []option
	Years     []option
	// AcquiredError is shown under the month and year selects.
	AcquiredError string
	// Reference is true when the Reference disclosure starts open.
	Reference bool
}

type factField struct {
	Name  string
	Label string
	Value string
}

func newPlantFormPage(f plantFields, now time.Time) plantFormPage {
	page := plantFormPage{
		Nickname:  f.nickname,
		Common:    f.common,
		Botanical: f.botanical,
		Location:  f.location,
		Notes:     f.notes,
		Months:    append([]option{{Value: "0", Label: "Month", On: f.month == 0}}, monthOptions(f.month)...),
		Years:     append([]option{{Value: "0", Label: "Year", On: f.year == 0}}, numberOptions(acquiredYears(now), f.year)...),
		Reference: f.notes != "" || f.month != 0 || f.year != 0,
	}
	for i, held := range f.facts() {
		page.Facts = append(page.Facts, factField{Name: referenceFields[i].name, Label: referenceFields[i].label, Value: *held})
		page.Reference = page.Reference || *held != ""
	}
	return page
}

func addPlantPage(f plantFields, rows []scheduleDraft, now time.Time) plantFormPage {
	page := newPlantFormPage(f, now)
	page.Title = "Add a plant"
	page.Action = newPlantPath
	page.Back = plantsPath
	page.Submit = "Add plant"
	for _, row := range rows {
		page.Schedules = append(page.Schedules, newScheduleField(row, newPlantPath, now))
	}
	return page
}

func editPlantPage(principal auth.Principal, f plantFields, plantID uuid.UUID, now time.Time) plantFormPage {
	page := newPlantFormPage(f, now)
	page.Title = "Edit plant"
	page.Action = editPlantPath(plantID)
	page.Back = plantPath(plantID)
	page.Submit = "Save changes"
	if principal.Can(auth.PlantArchive) {
		page.Archive = archivePlantPath(plantID)
	}
	return page
}

// acquiredYears is this year and the 20 before it, newest first.
func acquiredYears(now time.Time) []int {
	out := make([]int, 0, acquiredSpan)
	for i := range acquiredSpan {
		out = append(out, now.Year()-i)
	}
	return out
}

// readScheduleRows reads a row for every care type, since a re-render shows
// the closed rows as well as the open ones.
func readScheduleRows(values url.Values, cares []store.CareType, now time.Time) ([]scheduleDraft, bool) {
	rows := make([]scheduleDraft, 0, len(cares))
	for _, care := range cares {
		row, ok := readScheduleDraft(values, care, now)
		if !ok {
			return nil, false
		}
		rows = append(rows, row)
	}
	return rows, true
}

// openingRows returns the schedule rows for a freshly opened add form.
func openingRows(cares []store.CareType, now time.Time) []scheduleDraft {
	rows := make([]scheduleDraft, 0, len(cares))
	for _, care := range cares {
		row := newScheduleDraft(care, now)
		row.open = care.Slug == waterSlug
		rows = append(rows, row)
	}
	return rows
}

// swappedRow returns the row the re-render was triggered from: the one a
// button just closed, or the one whose fields were sent with the request.
func swappedRow(rows []scheduleField, values url.Values) (scheduleField, bool) {
	slug := values.Get("close")
	if slug == "" {
		slug = values.Get("open")
	}
	for _, row := range rows {
		if row.Slug == slug {
			return row, true
		}
	}
	return scheduleField{}, false
}

// newPlant handles GET /plants/new. The form is empty when first opened, and
// keeps its values when a schedule row re-renders it.
func (h *plants) newPlant(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	cares, err := h.queries.ListCareTypes(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the care types", err)
		return
	}
	now := h.now().In(locationFor(principal.User))

	// A request with no query string is the form being opened. A re-render
	// sends every field the form rendered.
	query := r.URL.Query()
	if len(query) == 0 {
		h.templates.render(w, r, view{page: plantFormPageName}, addPlantPage(plantFields{}, openingRows(cares, now), now))
		return
	}

	fields, fieldsOK := readPlantFields(query, now)
	rows, rowsOK := readScheduleRows(query, cares, now)
	if !fieldsOK || !rowsOK {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}
	page := addPlantPage(fields, rows, now)

	// An htmx request from one row gets that row back, since it is the only
	// element that changes.
	if isHTMX(r) {
		row, ok := swappedRow(page.Schedules, query)
		if !ok {
			http.Error(w, "the form has no such row", http.StatusBadRequest)
			return
		}
		page.Schedules = []scheduleField{row}
		h.templates.render(w, r, view{page: plantFormPageName, fragment: "schedule-fields"}, page)
		return
	}
	h.templates.render(w, r, view{page: plantFormPageName}, page)
}

// create handles POST /plants/new. It writes the plant and its schedules in one
// transaction.
func (h *plants) create(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	cares, err := h.queries.ListCareTypes(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the care types", err)
		return
	}
	now := h.now().In(locationFor(principal.User))

	fields, fieldsOK := readPlantFields(r.PostForm, now)
	rows, rowsOK := readScheduleRows(r.PostForm, cares, now)
	if !fieldsOK || !rowsOK {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}

	schedules, messages, refused := checkSchedules(rows, principal.Garden.ID)
	page := addPlantPage(fields, rows, now)
	page.NameError, page.AcquiredError = fields.refuse()
	for i := range page.Schedules {
		page.Schedules[i].Error = messages[page.Schedules[i].Slug]
	}
	if refused || page.NameError != "" || page.AcquiredError != "" {
		h.templates.render(w, r, view{page: plantFormPageName, status: http.StatusUnprocessableEntity}, page)
		return
	}

	var plant store.Plant
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		var err error
		if plant, err = q.CreatePlant(r.Context(), fields.create(principal.Garden.ID)); err != nil {
			return err
		}
		for _, params := range schedules {
			params.PlantID = plant.ID
			if _, err := q.CreateCareSchedule(r.Context(), params); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		serverError(h.logger, w, r, "add the plant", err)
		return
	}
	http.Redirect(w, r, plantPath(plant.ID), http.StatusSeeOther)
}

// checkSchedules converts the open rows to care_schedule params. Invalid rows
// get an error message instead, keyed by care type slug.
func checkSchedules(rows []scheduleDraft, gardenID uuid.UUID) (schedules []store.CreateCareScheduleParams, messages map[string]string, refused bool) {
	messages = map[string]string{}
	for _, row := range rows {
		if !row.open {
			continue
		}
		params, message := row.params(gardenID)
		if message != "" {
			messages[row.care.Slug] = message
			refused = true
			continue
		}
		schedules = append(schedules, params)
	}
	return schedules, messages, refused
}

// edit handles GET /plants/{plant}/edit with the same form, filled in.
func (h *plants) edit(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plant, ok := h.editable(w, r, principal)
	if !ok {
		return
	}
	now := h.now().In(locationFor(principal.User))
	h.templates.render(w, r, view{page: plantFormPageName}, editPlantPage(principal, plantFieldsOf(plant), plant.ID, now))
}

// update handles POST /plants/{plant}/edit and redirects to the plant's page.
func (h *plants) update(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	// The plant is resolved before the form is read, because a plant the
	// garden does not have is a 404 whatever was posted.
	plant, ok := h.editable(w, r, principal)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	now := h.now().In(locationFor(principal.User))
	fields, fieldsOK := readPlantFields(r.PostForm, now)
	if !fieldsOK {
		http.Error(w, "the form did not offer that", http.StatusBadRequest)
		return
	}

	page := editPlantPage(principal, fields, plant.ID, now)
	page.NameError, page.AcquiredError = fields.refuse()
	if page.NameError != "" || page.AcquiredError != "" {
		h.templates.render(w, r, view{page: plantFormPageName, status: http.StatusUnprocessableEntity}, page)
		return
	}

	if _, err := h.queries.UpdatePlant(r.Context(), fields.update(principal.Garden.ID, plant.ID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(h.logger, w, r, "save the plant", err)
		return
	}
	http.Redirect(w, r, plantPath(plant.ID), http.StatusSeeOther)
}

// confirmArchive handles GET /plants/{plant}/archive. It renders the plant's
// page with the archive confirmation at the bottom. An htmx request gets the
// bottom section alone, since that is the only element that changes.
func (h *plants) confirmArchive(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return
	}
	// An archived plant has no buttons at the bottom of its page, so there is
	// nothing to confirm.
	if detail.plant.ArchivedAt != nil {
		http.NotFound(w, r)
		return
	}

	page := newPlantPage(principal, detail)
	page.Foot = page.Foot.asking()
	fragment, ok := plantSwap(r, &page)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.templates.render(w, r, view{page: "plant", fragment: fragment}, page)
}

// archive handles POST /plants/{plant}/archive. A plant is archived rather
// than deleted so its history is kept.
func (h *plants) archive(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.queries.ArchivePlant(r.Context(), principal.Garden.ID, plantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(h.logger, w, r, "archive the plant", err)
		return
	}
	http.Redirect(w, r, plantsPath, http.StatusSeeOther)
}

// editable resolves the plant for the two edit routes. It writes the response
// itself and returns false when there is nothing to edit. An archived plant is
// a 404 like a plant the garden does not have, since the form would otherwise
// save a plant that is no longer on the Plants list.
func (h *plants) editable(w http.ResponseWriter, r *http.Request, principal auth.Principal) (store.Plant, bool) {
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return store.Plant{}, false
	}
	plant, err := h.queries.GetPlant(r.Context(), principal.Garden.ID, plantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return store.Plant{}, false
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return store.Plant{}, false
	}
	if plant.ArchivedAt != nil {
		http.NotFound(w, r)
		return store.Plant{}, false
	}
	return plant, true
}
