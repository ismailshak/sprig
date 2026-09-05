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

// The three shapes a schedule takes, as the When select names them. Each
// carries a sentence under it because a label is too small a space to explain a
// scheduling rule.
const (
	shapeCadence = "cadence"
	shapeDate    = "date"
	shapeOnce    = "once"
)

// shape is one option of the When select and the sentence under it.
type shape struct {
	value, label, hint string
}

var shapes = []shape{
	{shapeCadence, "Repeats", "Counted from the last time it was done"},
	{shapeDate, "On a fixed date", "The same date each time, however late you are"},
	{shapeOnce, "Just once", "One date, and then nothing"},
}

// anyDay is the day select's first option. A month with no day is the answer a
// repot actually gets, and forcing a 1st would invent a precision the plant's
// page then reports back as days late.
const anyDay = 0

// maxInterval is the largest count the number field offers. It bounds the
// message the field gives back as well as the field itself.
const maxInterval = 999

// anchorSpan is how many years the date selects reach, this one included.
const anchorSpan = 7

// scheduleDraft is one care type's row as far as it has been filled in. A row
// is open or the plant is not scheduled for that care and there is no third
// state, because this form has no plant yet to save a row against.
type scheduleDraft struct {
	care  store.CareType
	open  bool
	shape string
	// every is what was typed rather than a number because a count the row
	// refuses comes back in the field it was typed into.
	every    string
	unit     string
	seasonal bool
	from, to int
	day      int
	month    int
	year     int
	// A shape picked without JavaScript arrives without the fields that shape
	// needs because a browser posts only the controls it drew. These two say
	// whether the group was drawn.
	hasInterval bool
	hasAnchor   bool
}

// newScheduleDraft is a row nobody has touched, on the weekly cadence most
// people set.
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

// readScheduleDraft reads one row from a query or a form body. It returns
// false for a value none of the row's controls offers.
//
// open names the rows that are open and close the one a button just closed,
// which is how the open rows survive a re-render with no JavaScript.
func readScheduleDraft(values url.Values, care store.CareType, now time.Time) (scheduleDraft, bool) {
	f := newScheduleDraft(care, now)
	f.open = slices.Contains(values["open"], care.Slug) && !slices.Contains(values["close"], care.Slug)
	if !f.open {
		return f, true
	}

	name := func(part string) string { return care.Slug + "-" + part }
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
	// A box ticked without JavaScript arrives without the months because they
	// are drawn only where the box was already ticked. The row keeps the window
	// the editor would have offered.
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
		if f.year, ok = numberIn(values.Get(name("year")), anchorYears(now)); !ok {
			return f, false
		}
	}
	return f, true
}

// params is the row as care_schedule holds it. The second result is what the
// row says when the answer its shape needs is missing or is not a number the
// field offers, and is empty for a row the form takes. PlantID is the caller's
// to fill in, because the plant these rows arrive with does not exist until
// they are written.
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
		// The schema puts a season only on a cadence, because a date already
		// names its month.
		if f.seasonal {
			p.SeasonStartMonth = smallint(f.from)
			p.SeasonEndMonth = smallint(f.to)
		}
		return p, ""
	}

	if !f.hasAnchor {
		return p, "Give it a date."
	}
	// A month-precise anchor is stored on the first of its month, because the
	// day is the part nobody gave and the engine is meant to ignore it.
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

// scheduleField is one row of the schedule list as the form draws it. An open
// row is the editor and a closed one names a care the plant is not scheduled
// for.
type scheduleField struct {
	// ID is the row's id in both states.
	ID   string
	Care string
	Slug string
	Open bool
	// Path is where the row re-renders from, which is the form's own URL.
	Path string
	// Shapes draws the When select, and Hint the sentence under it.
	Shapes []option
	Hint   string
	// Repeats and Dated are which of the two field groups the shape needs.
	Repeats bool
	Dated   bool
	Every   string
	// Max is the largest count the field takes.
	Max   int
	Units []option
	// Seasonal draws the months, which only a cadence can carry.
	Seasonal bool
	From     []option
	To       []option
	Days     []option
	Months   []option
	Years    []option
	// Error is what the row says when a post could not be taken from it.
	Error string
}

func newScheduleField(f scheduleDraft, path string, now time.Time) scheduleField {
	field := scheduleField{
		ID:       scheduleFieldID(f.care),
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
	// A count of one takes the singular because the label reads in the number's
	// company.
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
	field.Years = numberOptions(anchorYears(now), f.year)
	return field
}

// scheduleFieldID names the row for the swap that replaces it. It uses the
// slug rather than the name because renaming a care type is free.
func scheduleFieldID(care store.CareType) string {
	return "sched-" + care.Slug
}

// units is the four the schema allows, in the order the select offers them.
var units = []string{schedule.UnitDay, schedule.UnitWeek, schedule.UnitMonth, schedule.UnitYear}

// option is one of a select's options, drawn in Go so that what the form offers
// and what it accepts back are one list.
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

// days is Any day and then every day a month can have.
func days() []int {
	out := make([]int, 0, 32)
	for d := anyDay; d <= 31; d++ {
		out = append(out, d)
	}
	return out
}

// anchorYears is this year and the six after it.
func anchorYears(now time.Time) []int {
	out := make([]int, 0, anchorSpan)
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

// numberIn is value as one of the numbers offered, and false for anything
// else.
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
