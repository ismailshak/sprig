package http

import (
	"math"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// waterSlug is the care type whose schedule row starts open on the add form,
// because every plant is watered.
const waterSlug = "water"

// acquiredSpan is how many years the acquired-year select offers, this year
// included.
const acquiredSpan = 21

// detailFields are the fields under Details, in the order the plant's page
// shows them, with the name each input posts under. Water and Feed repeat two
// care type names because a schedule says how often and a Details field says
// what this plant needs.
var detailFields = [...]struct{ name, label string }{
	{"sun", "Sun"},
	{"water", "Water"},
	{"feed", "Feed"},
	{"soil", "Soil"},
	{"climate", "Climate"},
	{"pot", "Pot"},
}

// plantFields holds the plant form's values other than the schedule rows and
// the photo.
type plantFields struct {
	nickname  string
	common    string
	botanical string
	room      string
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

// facts returns a pointer to the field for each of detailFields, in the same
// order, so reading and rendering the form iterate one list.
func (f *plantFields) facts() [len(detailFields)]*string {
	return [...]*string{&f.sun, &f.water, &f.feed, &f.soil, &f.climate, &f.pot}
}

// readPlantFields reads plantFields from a query string or a form body. It
// returns false for an acquired month or year that is not one of the options.
func readPlantFields(values url.Values, now time.Time) (plantFields, bool) {
	f := plantFields{
		nickname:  strings.TrimSpace(values.Get("nickname")),
		common:    strings.TrimSpace(values.Get("common")),
		botanical: strings.TrimSpace(values.Get("botanical")),
		room:      strings.TrimSpace(values.Get("room")),
		notes:     strings.TrimSpace(values.Get("notes")),
	}
	for i, held := range f.facts() {
		*held = strings.TrimSpace(values.Get(detailFields[i].name))
	}

	// A missing select stays at zero, because the htmx request from a schedule
	// row sends only that row's fields.
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
		room:      value(plant.Location),
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
		name = "Enter at least one name."
	}
	// A month with no year is an error rather than dropped silently, since the
	// plant's page shows nothing for it.
	if f.month != 0 && f.year == 0 {
		acquired = "Choose a year as well as a month."
	}
	return name, acquired
}

func (f plantFields) create(gardenID uuid.UUID) store.CreatePlantParams {
	return store.CreatePlantParams{
		GardenID:      gardenID,
		Nickname:      set(f.nickname),
		CommonName:    set(f.common),
		BotanicalName: set(f.botanical),
		Location:      set(f.room),
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

// canonicalRoom returns the room in rooms that matches typed ignoring case.
// Without it, "bathroom" would be a second room beside "Bathroom" on the Plants
// list. Text matching no room is returned as typed, because typing a new name
// is how a room is added.
func canonicalRoom(rooms []string, typed string) string {
	for _, room := range rooms {
		if strings.EqualFold(room, typed) {
			return room
		}
	}
	return typed
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

// acquiredYears returns the years the acquired-year select offers, newest
// first.
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
