package schedule

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// State classifies a schedule relative to the reader's day.
type State int

// The states are declared in ranking order. When one care has to represent a
// plant, the lowest State wins.
const (
	// Overdue is an occurrence whose day has passed.
	Overdue State = iota
	// DueToday is an occurrence on the reader's day. A month-precise
	// occurrence is DueToday for the whole of its month.
	DueToday
	// Upcoming is an occurrence still to come.
	Upcoming
	// Dormant is a cadence outside its season.
	Dormant
	// Spent is a one-off that has been done.
	Spent
)

// comingUpDays is how far ahead the Coming up section looks. A week keeps the
// section short in a garden whose intervals run to a month.
const comingUpDays = 7

// Line is one schedule resolved against the reader's day, together with the
// rows it was resolved from, so a template can render it without another
// lookup.
type Line struct {
	Schedule store.CareSchedule
	Plant    store.Plant
	CareType store.CareType
	// Last is the most recent event of this care type on the plant, or nil if
	// there has never been one.
	Last *store.CareEvent

	State State
	// Due is midnight at the start of the due day in the reader's location.
	// It is zero for Dormant and Spent. For a month-precise occurrence it is
	// the first of the month.
	Due       time.Time
	Precision string
	// Days is the number of days from the reader's today to Due. Negative
	// means overdue. A month-precise occurrence counts to the first of its
	// month, so inside that month Days is zero or negative while State is
	// still DueToday.
	Days int
}

// Resolve computes a Line for each schedule against the day now falls on in
// its location. Callers pass now in the reader's location, as for Next. latest
// is the most recent event per plant and care type, as ListLatestCareEvents
// returns it. The result is in the same order as schedules.
func Resolve(schedules []store.ListCareSchedulesRow, latest []store.CareEvent, now time.Time) []Line {
	type key struct{ plant, careType uuid.UUID }
	last := make(map[key]*store.CareEvent, len(latest))
	for i := range latest {
		last[key{latest[i].PlantID, latest[i].CareTypeID}] = &latest[i]
	}

	loc := now.Location()
	today := dayOf(now, loc)

	lines := make([]Line, 0, len(schedules))
	for _, row := range schedules {
		line := Line{
			Schedule: row.CareSchedule,
			Plant:    row.Plant,
			CareType: row.CareType,
			Last:     last[key{row.CareSchedule.PlantID, row.CareSchedule.CareTypeID}],
		}
		o, ok := Next(line.Schedule, line.Last, now)
		switch {
		case ok:
			line.Due = dayOf(o.At, loc)
			line.Precision = o.Precision
			line.Days = DaysBetween(today, line.Due)
			line.State = state(today, line.Due, line.Precision)
		case line.Schedule.SeasonStartMonth != nil:
			line.State = Dormant
		default:
			line.State = Spent
		}
		lines = append(lines, line)
	}
	return lines
}

// state classifies a due date against today. A month-precise occurrence is
// overdue only once its month has ended, because a month names no day to be
// late against.
func state(today, due time.Time, precision string) State {
	end := due.AddDate(0, 0, 1)
	if precision == PrecisionMonth {
		end = due.AddDate(0, 1, 0)
	}
	switch {
	case today.Before(due):
		return Upcoming
	case today.Before(end):
		return DueToday
	default:
		return Overdue
	}
}

// DaysBetween counts calendar days from one instant to another, both read in
// to's location. The two midnights are converted to UTC before subtracting,
// because in the reader's location a day either side of a clock change is 23
// or 25 hours long.
func DaysBetween(from, to time.Time) int {
	f := midnightUTC(from.In(to.Location()))
	t := midnightUTC(to)
	return int(t.Sub(f) / (24 * time.Hour))
}

func midnightUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Day is the garden's schedules grouped into the three sections the Today
// page shows.
type Day struct {
	Overdue  []Row
	DueToday []Row
	ComingUp []Row
	// Next is every plant due on the soonest day beyond the ComingUp window,
	// in name order. It is empty when nothing is due beyond the week. The Today
	// page names these plants when the three sections are all empty.
	Next []Row
}

// Row is a plant on the Today page and the care that put it there.
type Row struct {
	Plant store.Plant
	// Care is the plant's most overdue care, or failing that its soonest. A
	// plant gets one row however many cares are due, because the sheet the
	// row opens offers the rest.
	Care Line
	// Lines is every schedule the plant has, in any state, in Resolve's order.
	// The sheet lists these care types and their intervals.
	Lines []Line
}

// Today groups resolved lines into the sections the Today page shows. A plant
// whose schedules are all dormant or spent appears in none of them.
func Today(lines []Line) Day {
	var day Day
	var later []Row
	for _, row := range rows(lines) {
		switch {
		case row.Care.State == Overdue:
			day.Overdue = append(day.Overdue, row)
		case row.Care.State == DueToday:
			day.DueToday = append(day.DueToday, row)
		case row.Care.Days <= comingUpDays:
			day.ComingUp = append(day.ComingUp, row)
		default:
			later = append(later, row)
		}
	}
	slices.SortStableFunc(day.Overdue, compareRows)
	slices.SortStableFunc(day.DueToday, compareRows)
	slices.SortStableFunc(day.ComingUp, compareRows)
	slices.SortStableFunc(later, compareRows)
	for _, row := range later {
		if soon(row.Care) != soon(later[0].Care) {
			break
		}
		day.Next = append(day.Next, row)
	}
	return day
}

// Nearest returns the plant's most pressing schedule: the most overdue, then
// the soonest, with dormant and spent ones ranked last. It returns false for a
// plant with no schedules.
func Nearest(lines []Line) (Line, bool) {
	if len(lines) == 0 {
		return Line{}, false
	}
	best := lines[0]
	for _, line := range lines[1:] {
		if compareLines(line, best) < 0 {
			best = line
		}
	}
	return best, true
}

func rows(lines []Line) []Row {
	var out []Row
	at := map[uuid.UUID]int{}
	for _, line := range lines {
		i, seen := at[line.Plant.ID]
		if !seen {
			i = len(out)
			at[line.Plant.ID] = i
			out = append(out, Row{Plant: line.Plant, Care: line})
		}
		out[i].Lines = append(out[i].Lines, line)
		if compareLines(line, out[i].Care) < 0 {
			out[i].Care = line
		}
	}

	return slices.DeleteFunc(out, func(r Row) bool { return r.Care.State >= Dormant })
}

func compareLines(a, b Line) int {
	return cmp.Or(cmp.Compare(a.State, b.State), cmp.Compare(soon(a), soon(b)))
}

// compareRows orders by due date, then name, then plant id, so two plants
// sharing a name keep a stable order.
func compareRows(a, b Row) int {
	return cmp.Or(
		cmp.Compare(soon(a.Care), soon(b.Care)),
		strings.Compare(strings.ToLower(a.Plant.DisplayName()), strings.ToLower(b.Plant.DisplayName())),
		cmp.Compare(a.Plant.ID.String(), b.Plant.ID.String()),
	)
}

// soon is the sort key for how far off a line is. A line due today sorts at
// zero, so a month-precise occurrence weeks into its month does not outrank a
// care due this morning.
func soon(l Line) int {
	if l.State == DueToday {
		return 0
	}
	return l.Days
}
