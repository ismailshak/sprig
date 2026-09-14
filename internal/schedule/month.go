package schedule

import (
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// Projected is one date in a month on which a schedule falls due.
type Projected struct {
	Occurrence
	// Overdue is true for a care whose due date has passed. InMonth lists it on
	// today, or in the current month for PrecisionMonth, rather than on the day
	// that has passed.
	Overdue bool
	// Estimated is true for a cadence date after the schedule's next
	// occurrence. It moves if the care before it is logged on a different day.
	Estimated bool
}

// InMonth returns the dates in month on which a schedule falls due, in date
// order, given the most recent event of its care type on the plant or nil.
// month is any time in the month, read in now's location. At is midnight in
// now's location, and for PrecisionMonth it is the first of the month.
//
// The first date is the one Next returns while the season is open today. A
// first date that has passed moves to today and is Overdue. After it, a cadence
// adds its interval to the date before, an anchored series takes its next
// anchor date, and a one-off has no later date. An override_interval_days on
// the last event decides the first date only.
//
// A season is checked against each date's own month rather than against now,
// so a cadence whose season is closed today still has dates after the next
// opening. A date in a closed month moves to the next opening.
func InMonth(s store.CareSchedule, last *store.CareEvent, month, now time.Time) []Projected {
	loc := now.Location()
	today := dayOf(now, loc)
	start := firstOfMonth(month.In(loc))
	end := start.AddDate(0, 1, 0)

	o, ok := shape(s, last, loc)
	if !ok {
		return nil
	}
	p := Projected{Occurrence: Occurrence{At: dayOf(o.At, loc), Precision: o.Precision}}
	if s.SeasonStartMonth != nil {
		// While the season is open, a date from before its latest opening moves
		// to the opening, as Next moves it.
		opens, closes := time.Month(*s.SeasonStartMonth), time.Month(*s.SeasonEndMonth)
		if opening := lastOpening(today, opens); inSeason(today.Month(), opens, closes) && p.At.Before(opening) {
			p.At = opening
		}
	}
	due := today
	if p.Precision == PrecisionMonth {
		// A month-precise care is overdue only once its month has ended.
		due = firstOfMonth(today)
	}
	if p.At.Before(due) {
		p.At, p.Overdue = due, true
	}
	if opened := inSeasonOrNextOpening(s, p.At); !opened.Equal(p.At) {
		// A date moved to the next opening is not overdue.
		p.At, p.Overdue = opened, false
	}

	var dates []Projected
	for p.At.Before(end) {
		if !p.At.Before(start) {
			dates = append(dates, p)
		}
		if p, ok = following(s, p.At, loc); !ok {
			break
		}
	}
	return dates
}

// following returns the date a schedule falls due after care done on day, or
// false for a one-off.
func following(s store.CareSchedule, day time.Time, loc *time.Location) (Projected, bool) {
	switch {
	case s.IntervalCount == nil:
		return Projected{}, false
	case s.AnchorDate == nil:
		at := dayOf(advance(day, *s.IntervalCount, *s.IntervalUnit, 1), loc)
		return Projected{Occurrence: Occurrence{At: inSeasonOrNextOpening(s, at), Precision: PrecisionDay}, Estimated: true}, true
	default:
		o, _ := shape(s, &store.CareEvent{PerformedAt: day, Done: true}, loc)
		return Projected{Occurrence: o}, true
	}
}

func inSeasonOrNextOpening(s store.CareSchedule, at time.Time) time.Time {
	if s.SeasonStartMonth == nil {
		return at
	}
	start, end := time.Month(*s.SeasonStartMonth), time.Month(*s.SeasonEndMonth)
	if inSeason(at.Month(), start, end) {
		return at
	}
	return nextOpening(at, start)
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}
