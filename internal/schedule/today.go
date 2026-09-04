package schedule

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// State is what a schedule is doing on the reader's day.
type State int

// The states are declared in the order a plant's cares are ranked when one
// of them has to stand for the plant.
const (
	// Overdue is an occurrence whose day has passed.
	Overdue State = iota
	// DueToday is an occurrence on the reader's day. A month-precise
	// occurrence is due for the whole of its month.
	DueToday
	// Upcoming is an occurrence still to come.
	Upcoming
	// Dormant is a cadence whose season is shut.
	Dormant
	// Spent is a one-off that has been done.
	Spent
)

// A week keeps Coming up short on a garden whose intervals run to a month.
const comingUpDays = 7

// Line is one schedule resolved against the reader's day. It carries the rows
// it was resolved from, so a template needs nothing else to draw it.
type Line struct {
	Schedule store.CareSchedule
	Plant    store.Plant
	CareType store.CareType
	// Last is the most recent event of the care type on the plant, and nil
	// where there has never been one.
	Last *store.CareEvent

	State State
	// Due is the occurrence's day at midnight in the reader's location, and
	// zero for Dormant and Spent. A month-precise occurrence's Due is the
	// first of its month.
	Due       time.Time
	Precision string
	// Days is how many days after the reader's today Due falls, so a negative
	// count is overdue. A month-precise occurrence counts to the first of its
	// month, so inside the month Days is at or below zero while State is
	// DueToday.
	Days int
}

// Resolve reads a garden's schedules against the day now falls on in its
// location. A caller passes now in the reader's location, as for Next. latest
// holds the most recent event per plant and care type, which
// ListLatestCareEvents produces. The result keeps the order ListCareSchedules
// gave the schedules.
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
			line.Days = daysBetween(today, line.Due)
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

// A month-precise occurrence is overdue only once its month has gone, because
// a month names no day to be late against.
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

// daysBetween counts calendar days from one local midnight to another. It
// re-expresses both in UTC before subtracting, because a day either side of
// a clock change is twenty-three or twenty-five hours long in the reader's
// location.
func daysBetween(from, to time.Time) int {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(t.Sub(f) / (24 * time.Hour))
}

// Day is the garden as the reader's day finds it, in the three sections
// Today draws.
type Day struct {
	Overdue  []Row
	DueToday []Row
	ComingUp []Row
	// Next is the plant nearest to being due beyond ComingUp, which the empty
	// state names. It is nil where nothing is scheduled beyond the week.
	Next *Row
}

// Row is a plant on Today and the care that put it there.
type Row struct {
	Plant store.Plant
	// Care is the plant's most overdue care, failing that its soonest. A plant
	// is one row however many of its cares are due, because the sheet the row
	// opens offers the rest.
	Care Line
	// Lines is every schedule the plant has, whatever its state, in the order
	// Resolve gave them. The sheet lists its care types and their intervals.
	Lines []Line
}

// Today groups resolved lines into the sections the screen draws. A plant
// whose every schedule is dormant or spent is on no list.
func Today(lines []Line) Day {
	var day Day
	for _, row := range rows(lines) {
		switch {
		case row.Care.State == Overdue:
			day.Overdue = append(day.Overdue, row)
		case row.Care.State == DueToday:
			day.DueToday = append(day.DueToday, row)
		case row.Care.Days <= comingUpDays:
			day.ComingUp = append(day.ComingUp, row)
		case day.Next == nil || compareRows(row, *day.Next) < 0:
			next := row
			day.Next = &next
		}
	}
	slices.SortStableFunc(day.Overdue, compareRows)
	slices.SortStableFunc(day.DueToday, compareRows)
	slices.SortStableFunc(day.ComingUp, compareRows)
	return day
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

// compareRows falls back to the plant's id, so two plants sharing a name keep
// their order.
func compareRows(a, b Row) int {
	return cmp.Or(
		cmp.Compare(soon(a.Care), soon(b.Care)),
		strings.Compare(strings.ToLower(a.Plant.DisplayName()), strings.ToLower(b.Plant.DisplayName())),
		cmp.Compare(a.Plant.ID.String(), b.Plant.ID.String()),
	)
}

// soon is how far off a line is for ordering. A line due today is at zero, so
// a month-precise occurrence weeks into its month does not outrank a care due
// this morning.
func soon(l Line) int {
	if l.State == DueToday {
		return 0
	}
	return l.Days
}
