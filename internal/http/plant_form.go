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

// plantFormPageName is the one page behind both routes, because adding a plant
// and editing one are the same fields in the same order.
const plantFormPageName = "plant-form"

const newPlantPath = plantsPath + "/new"

func editPlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/edit"
}

func archivePlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/archive"
}

// waterSlug names the row the add form opens filled in, because every plant in
// the garden is watered and no other care is the common case.
const waterSlug = "water"

// acquiredSpan is how far back the acquired year reaches, this year included.
const acquiredSpan = 21

// referenceFields is the Reference fields in the order a plant's page reads
// them, each named by the input it is typed into. Water and Feed repeat the
// care types above them because a schedule says how often and a reference field
// says what the care means for this plant.
var referenceFields = [...]struct{ name, label string }{
	{"sun", "Sun"},
	{"water", "Water"},
	{"feed", "Feed"},
	{"soil", "Soil"},
	{"climate", "Climate"},
	{"pot", "Pot"},
}

// plantFields is the plant half of the form as far as it has been filled in.
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
	// month and year are zero where the select is on its first option.
	month int
	year  int
}

// facts pairs each of referenceFields with the field holding it because reading
// the form and drawing it walk one order.
func (f *plantFields) facts() [len(referenceFields)]*string {
	return [...]*string{&f.sun, &f.water, &f.feed, &f.soil, &f.climate, &f.pot}
}

// readPlantFields reads the plant half from a query or a form body. It returns
// false for an acquired month or year no option offers.
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

	// An absent select is left at zero because a schedule row's own request
	// carries that row and none of these fields.
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

// refuse is what the form says back when a post cannot be taken from it: a
// sentence under the three names and one under Acquired. Both are empty for a
// post the form takes.
func (f plantFields) refuse() (name, acquired string) {
	if f.nickname == "" && f.common == "" && f.botanical == "" {
		name = "Give it at least one name. Any of the three will do."
	}
	// A month with no year is asked about rather than dropped because the
	// plant's page shows nothing for one.
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

// update writes the same columns as create, because a field emptied on the
// form is a field emptied on the row.
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

// set is a text field as the column holds it. An empty field is null because an
// empty string in the column would read as an answer.
func set(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// acquiredPart is an acquired month or year as the column holds it, and null
// on the select's first option.
func acquiredPart(n int) *int16 {
	if n == 0 {
		return nil
	}
	return smallint(n)
}

// smallint is n as a smallint column holds it, and null for a number no such
// column can carry.
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
	// Action is where the form posts and where a schedule row re-renders from.
	Action string
	// Back is where Cancel leads.
	Back   string
	Submit string
	// Archive is where the edit form's foot links, and is empty on the add form
	// and for a reader who may not archive.
	Archive   string
	Nickname  string
	Common    string
	Botanical string
	// NameError sits under the three names together, because no one of them is
	// the required one.
	NameError string
	Location  string
	// Schedules is empty on the edit form, where changing a schedule happens
	// on the plant's own page beside the date it moves.
	Schedules []scheduleField
	Facts     []factField
	Notes     string
	Months    []option
	Years     []option
	// AcquiredError sits under the month and the year together.
	AcquiredError string
	// Reference opens the disclosure.
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

// acquiredYears is this year and the twenty before it, newest first.
func acquiredYears(now time.Time) []int {
	out := make([]int, 0, acquiredSpan)
	for i := range acquiredSpan {
		out = append(out, now.Year()-i)
	}
	return out
}

// readScheduleRows reads a row for every care type because a re-render draws
// the rows nobody opened as well as the open ones.
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

// openingRows is the schedule list a form opened for the first time draws.
func openingRows(cares []store.CareType, now time.Time) []scheduleDraft {
	rows := make([]scheduleDraft, 0, len(cares))
	for _, care := range cares {
		row := newScheduleDraft(care, now)
		row.open = care.Slug == waterSlug
		rows = append(rows, row)
	}
	return rows
}

// swappedRow is the row a re-render started from: the one a button has just
// closed, or the one whose fields came back with the request.
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

// newPlant answers GET /plants/new with the form: empty when it is opened, and
// as far as it has been filled in when a schedule row brings it back.
func (h *plants) newPlant(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	cares, err := h.queries.ListCareTypes(r.Context(), principal.Garden.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the care types", err)
		return
	}
	now := h.now().In(locationFor(principal.User))

	// A request carrying no query is the form being opened because a browser
	// coming back to it sends every field the form drew.
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

	// A swap started from one row is answered with that row, which is the
	// element that differs between the two states.
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

// create answers POST /plants/new by writing the plant and the schedules it
// arrives with.
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

// checkSchedules is the open rows as care_schedule holds them, and the
// sentences the rows that cannot be taken show instead, keyed by care type.
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

// edit answers GET /plants/{plant}/edit with the same form, filled in.
func (h *plants) edit(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plant, ok := h.editable(w, r, principal)
	if !ok {
		return
	}
	now := h.now().In(locationFor(principal.User))
	h.templates.render(w, r, view{page: plantFormPageName}, editPlantPage(principal, plantFieldsOf(plant), plant.ID, now))
}

// update answers POST /plants/{plant}/edit and sends the reader back to the
// plant.
func (h *plants) update(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	// The plant resolves before the form is read, because a plant the garden
	// does not have is a 404 whatever was posted at it.
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

// confirmArchive answers GET /plants/{plant}/archive with the plant's page,
// its foot carrying the question. A swap gets the foot alone, because that is
// the element that differs between the two states.
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
	// The question is refused for an archived plant because its page draws no
	// foot to ask it in.
	if detail.plant.ArchivedAt != nil {
		http.NotFound(w, r)
		return
	}

	page := newPlantPage(principal, detail)
	page.Foot = page.Foot.asking()
	h.templates.render(w, r, view{page: "plant", fragment: plantFootFragment(r)}, page)
}

// archive answers POST /plants/{plant}/archive. A plant is archived rather
// than deleted, because its history is the record of what happened to it.
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

// editable is the plant the two edit routes act on. It answers the request
// itself and reports false where there is nothing to edit. An archived plant is
// as far out of reach as one the garden does not have because the form would
// otherwise save a plant that has left the Plants list.
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
