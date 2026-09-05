package http

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// The three kinds of schedule, as the When select names them. Each has a hint
// sentence under it because a label is too short to explain a scheduling rule.
const (
	shapeCadence = "cadence"
	shapeDate    = "date"
	shapeOnce    = "once"
)

// shape is one option of the When select and its hint.
type shape struct {
	value, label, hint string
}

var shapes = []shape{
	{shapeCadence, "Repeats", "Counted from the last time it was done"},
	{shapeDate, "On a fixed date", "The same date each time, however late you are"},
	{shapeOnce, "Just once", "One date, and then nothing"},
}

// anyDay is the day select's first option, meaning a month with no specific
// day. A repot is usually planned to a month, and forcing the 1st would invent
// a precision the plant's page then reports as days late.
const anyDay = 0

// maxInterval is the largest value the interval field accepts. It appears in
// the field's max attribute and in the error message.
const maxInterval = 999

// anchorSpan is how many years the year select offers, this year included.
const anchorSpan = 7

// scheduleDraft is the values of one care type's schedule row as filled in so
// far. A row is either open or unscheduled. There is no saved state, because
// on the add form the plant does not exist yet.
type scheduleDraft struct {
	care  store.CareType
	open  bool
	shape string
	// every is kept as typed rather than parsed, so a rejected value is shown
	// back in the field.
	every    string
	unit     string
	seasonal bool
	from, to int
	day      int
	month    int
	year     int
	// A shape picked without JavaScript arrives without the fields that shape
	// needs, because a browser posts only the controls it rendered. These two
	// record whether each field group was in the form.
	hasInterval bool
	hasAnchor   bool
}

// newScheduleDraft returns an untouched row, defaulting to weekly.
func newScheduleDraft(care store.CareType, now time.Time) scheduleDraft {
	return scheduleDraft{
		care:        care,
		shape:       shapeCadence,
		every:       "1",
		unit:        schedule.UnitWeek,
		from:        int(time.March),
		to:          int(time.September),
		day:         anyDay,
		month:       int(now.Month()),
		year:        now.Year() + 1,
		hasInterval: true,
		hasAnchor:   true,
	}
}

// readScheduleDraft reads one row of the add form from a query string or a
// form body. It returns false for a value none of the row's controls offers.
//
// The "open" values list the rows that are open and "close" the one a button
// just closed. That is how open rows survive a re-render without JavaScript.
func readScheduleDraft(values url.Values, care store.CareType, now time.Time) (scheduleDraft, bool) {
	f := newScheduleDraft(care, now)
	f.open = slices.Contains(values["open"], care.Slug) && !slices.Contains(values["close"], care.Slug)
	if !f.open {
		return f, true
	}
	return f.fill(values, now)
}

// fill applies the row's posted values over the draft. It is separate from
// readScheduleDraft because the in-place editor has one row and no open or
// close button.
func (f scheduleDraft) fill(values url.Values, now time.Time) (scheduleDraft, bool) {
	name := func(part string) string { return f.care.Slug + "-" + part }
	// The year options come from the draft the row was rendered from, so a
	// schedule anchored before this year keeps its year as an option.
	years := anchorYears(now, f.year)
	if picked := values.Get(name("shape")); picked != "" {
		if !slices.ContainsFunc(shapes, func(s shape) bool { return s.value == picked }) {
			return f, false
		}
		f.shape = picked
	}

	var ok bool
	f.hasInterval = values.Has(name("every"))
	if f.hasInterval {
		f.every = strings.TrimSpace(values.Get(name("every")))
		if f.unit = values.Get(name("unit")); !slices.Contains(units, f.unit) {
			return f, false
		}
	}

	f.seasonal = values.Has(name("seasonal"))
	// A box ticked without JavaScript arrives without the months, because they
	// are rendered only when the box was already ticked. The row keeps the
	// default months.
	if values.Has(name("from")) {
		if f.from, ok = monthIn(values.Get(name("from"))); !ok {
			return f, false
		}
		if f.to, ok = monthIn(values.Get(name("to"))); !ok {
			return f, false
		}
	}

	f.hasAnchor = values.Has(name("month"))
	if f.hasAnchor {
		if f.day, ok = numberIn(values.Get(name("day")), days()); !ok {
			return f, false
		}
		if f.month, ok = monthIn(values.Get(name("month"))); !ok {
			return f, false
		}
		if f.year, ok = numberIn(values.Get(name("year")), years); !ok {
			return f, false
		}
	}
	return f, true
}

// params converts the draft to a care_schedule row. The second result is the
// error message to show when a required value is missing or out of range, and
// is empty when the row is valid. The caller fills in PlantID, because on the
// add form the plant does not exist until it is written.
func (f scheduleDraft) params(gardenID uuid.UUID) (store.CreateCareScheduleParams, string) {
	p := store.CreateCareScheduleParams{GardenID: gardenID, CareTypeID: f.care.ID}

	if f.shape != shapeOnce {
		if !f.hasInterval {
			return p, "Say how often it repeats."
		}
		count, err := strconv.ParseInt(f.every, 10, 32)
		if err != nil || count < 1 || count > maxInterval {
			return p, fmt.Sprintf("Give a number between 1 and %d.", maxInterval)
		}
		p.IntervalCount = ptr(int32(count))
		p.IntervalUnit = &f.unit
	}

	if f.shape == shapeCadence {
		// The schema allows a season only on a repeating schedule, since a
		// dated one already names its month.
		if f.seasonal {
			p.SeasonStartMonth = smallint(f.from)
			p.SeasonEndMonth = smallint(f.to)
		}
		return p, ""
	}

	if !f.hasAnchor {
		return p, "Give it a date."
	}
	// A month-only anchor is stored on the 1st of its month with month
	// precision, so the schedule engine knows to ignore the day.
	day, precision := f.day, schedule.PrecisionDay
	if day == anyDay {
		day, precision = 1, schedule.PrecisionMonth
	}
	anchor := time.Date(f.year, time.Month(f.month), day, 0, 0, 0, 0, time.UTC)
	if anchor.Day() != day {
		return p, fmt.Sprintf("There is no %d %s.", f.day, time.Month(f.month))
	}
	p.AnchorDate = &anchor
	p.AnchorPrecision = &precision
	return p, ""
}

// scheduleDraftOf builds a draft from an existing schedule. The shape is
// worked out the way the schema defines it, by which nullable groups are set.
func scheduleDraftOf(care store.CareType, s store.CareSchedule, now time.Time) scheduleDraft {
	f := newScheduleDraft(care, now)
	switch {
	case s.AnchorDate == nil:
		f.shape = shapeCadence
	case s.IntervalCount == nil:
		f.shape = shapeOnce
	default:
		f.shape = shapeDate
	}
	if s.IntervalCount != nil {
		f.every = strconv.Itoa(int(*s.IntervalCount))
		f.unit = *s.IntervalUnit
	}
	// The schema sets the two season months together, so the end is non-nil
	// wherever the start is.
	if s.SeasonStartMonth != nil {
		f.seasonal = true
		f.from, f.to = int(*s.SeasonStartMonth), int(*s.SeasonEndMonth)
	}
	if s.AnchorDate != nil {
		// The column is a date, so its parts are read as stored rather than
		// converted to the reader's timezone first.
		year, month, day := s.AnchorDate.Date()
		f.day = day
		if *s.AnchorPrecision == schedule.PrecisionMonth {
			f.day = anyDay
		}
		f.month, f.year = int(month), year
	}
	return f
}

// upsert converts the draft to the row the in-place editor saves. The two
// generated params types have the same fields and differ only in their query.
func (f scheduleDraft) upsert(gardenID, plantID uuid.UUID) (store.UpsertCareScheduleParams, string) {
	p, message := f.params(gardenID)
	p.PlantID = plantID
	return store.UpsertCareScheduleParams(p), message
}

// scheduleField is the data one schedule row renders from. An open row is the
// editor and a closed one names a care type the plant has no schedule for.
type scheduleField struct {
	// ID is the row's HTML id, the same open or closed.
	ID   string
	Care string
	Slug string
	Open bool
	// Path is the URL the row re-renders from, the form's own URL.
	Path string
	// Shapes is the When select's options, and Hint the sentence under it.
	Shapes []option
	Hint   string
	// Repeats and Dated say which of the two field groups the shape needs.
	Repeats bool
	Dated   bool
	Every   string
	// Max is the interval field's max attribute.
	Max   int
	Units []option
	// Seasonal shows the month selects, which only a repeating schedule has.
	Seasonal bool
	From     []option
	To       []option
	Days     []option
	Months   []option
	Years    []option
	// Error is the message shown when the posted row was invalid.
	Error string
}

func newScheduleField(f scheduleDraft, path string, now time.Time) scheduleField {
	field := scheduleField{
		ID:       scheduleRowID(f.care),
		Care:     f.care.Name,
		Slug:     f.care.Slug,
		Open:     f.open,
		Path:     path,
		Repeats:  f.shape != shapeOnce,
		Dated:    f.shape != shapeCadence,
		Every:    f.every,
		Max:      maxInterval,
		Seasonal: f.seasonal,
	}
	for _, s := range shapes {
		field.Shapes = append(field.Shapes, option{Value: s.value, Label: s.label, On: s.value == f.shape})
		if s.value == f.shape {
			field.Hint = s.hint
		}
	}
	// The unit is singular after 1, since the label reads with the number.
	for _, unit := range units {
		label := unit
		if f.every != "1" {
			label += "s"
		}
		field.Units = append(field.Units, option{Value: unit, Label: label, On: unit == f.unit})
	}
	field.From = monthOptions(f.from)
	field.To = monthOptions(f.to)
	field.Days = append([]option{{Value: strconv.Itoa(anyDay), Label: "Any day", On: f.day == anyDay}}, numberOptions(days()[1:], f.day)...)
	field.Months = monthOptions(f.month)
	field.Years = numberOptions(anchorYears(now, f.year), f.year)
	return field
}

// units is the four units the schema allows, in the order the select lists
// them.
var units = []string{schedule.UnitDay, schedule.UnitWeek, schedule.UnitMonth, schedule.UnitYear}

// option is one option of a select. Options are built in Go so the form offers
// and accepts the same list.
type option struct {
	Value string
	Label string
	On    bool
}

func monthOptions(selected int) []option {
	out := make([]option, 0, 12)
	for m := time.January; m <= time.December; m++ {
		out = append(out, option{Value: strconv.Itoa(int(m)), Label: m.String(), On: int(m) == selected})
	}
	return out
}

func numberOptions(numbers []int, selected int) []option {
	out := make([]option, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, option{Value: strconv.Itoa(n), Label: strconv.Itoa(n), On: n == selected})
	}
	return out
}

// days is anyDay followed by 1 to 31.
func days() []int {
	out := make([]int, 0, 32)
	for d := anyDay; d <= 31; d++ {
		out = append(out, d)
	}
	return out
}

// anchorYears is this year and the six after it. If of is earlier than this
// year it is prepended, so a schedule anchored in the past keeps its year as an
// option.
func anchorYears(now time.Time, of int) []int {
	out := make([]int, 0, anchorSpan+1)
	if of < now.Year() {
		out = append(out, of)
	}
	for i := range anchorSpan {
		out = append(out, now.Year()+i)
	}
	return out
}

func monthIn(value string) (int, bool) {
	return numberIn(value, months())
}

func months() []int {
	out := make([]int, 0, 12)
	for m := time.January; m <= time.December; m++ {
		out = append(out, int(m))
	}
	return out
}

// numberIn parses value and returns false unless it is one of the offered
// numbers.
func numberIn(value string, offered []int) (int, bool) {
	n, err := strconv.Atoi(value)
	if err != nil || !slices.Contains(offered, n) {
		return 0, false
	}
	return n, true
}

func ptr[T any](v T) *T {
	return &v
}
