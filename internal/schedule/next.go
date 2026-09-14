package schedule

import (
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// care_schedule's check constraint allows these four units.
const (
	UnitDay   = "day"
	UnitWeek  = "week"
	UnitMonth = "month"
	UnitYear  = "year"
)

// care_schedule's check constraint allows these two anchor precisions.
const (
	PrecisionDay   = "day"
	PrecisionMonth = "month"
)

// Occurrence is a date a schedule falls due on.
type Occurrence struct {
	// At is in the location Next was given.
	At time.Time
	// Precision is PrecisionDay or PrecisionMonth. With PrecisionMonth the
	// schedule is due some time in At's month and At is the first of that month.
	Precision string
}

// Next returns when a schedule next falls due, given the most recent event of
// its care type on the plant, or nil if there is none. The second result is
// false when the schedule has nothing more to produce.
//
// The schedule's shape is decided by which nullable columns are set, the same
// way the schema constrains them:
//
//	cadence   an interval and no anchor   the last event plus the interval
//	anchored  an interval and an anchor   the earliest anchor plus k intervals later than the last event
//	one-off   an anchor and no interval   the anchor, once
//
// A cadence counts from the event and an anchor from the calendar, so care
// given late moves a cadence and leaves an anchored series where it is.
//
// An override_interval_days on the last event replaces the shape's result. A
// season on a cadence is a window of months, inclusive at both ends and
// wrapping at the year end. While the season is closed the schedule produces
// nothing. While it is open, an occurrence dated before the opening moves to
// the opening. The season applies to the override as well.
//
// now's location decides how long a day or a month is when one is added, which
// day an event fell on, and which day an anchor names. Callers pass now in the
// reader's location so the result agrees with the reader's calendar day.
//
// now's instant decides only whether the season is open. An anchored series
// last done a year ago returns the occurrence that was missed. The caller
// decides what counts as overdue.
func Next(s store.CareSchedule, last *store.CareEvent, now time.Time) (Occurrence, bool) {
	loc := now.Location()
	o, ok := shape(s, last, loc)
	if !ok || s.SeasonStartMonth == nil {
		return o, ok
	}

	start, end := time.Month(*s.SeasonStartMonth), time.Month(*s.SeasonEndMonth)
	if !inSeason(now.Month(), start, end) {
		return Occurrence{}, false
	}

	// An occurrence dated in the closed months would otherwise show as months
	// overdue, so it moves to the current season's opening. One dated after
	// the season closes moves to the next opening.
	if opening := lastOpening(now, start); o.At.Before(opening) {
		o.At = opening
	} else if !inSeason(o.At.In(loc).Month(), start, end) {
		o.At = nextOpening(o.At.In(loc), start)
	}
	return o, true
}

// shape computes the occurrence before the season is applied. Intervals are
// added to wall-clock times in loc, so a day added across a clock change is a
// calendar day and not 24 hours.
func shape(s store.CareSchedule, last *store.CareEvent, loc *time.Location) (Occurrence, bool) {
	if last != nil && last.OverrideIntervalDays != nil {
		// PerformedAt rather than RecordedAt, so a backdated skip counts from
		// when the plant was looked at rather than when somebody logged it.
		at := last.PerformedAt.In(loc).AddDate(0, 0, int(*last.OverrideIntervalDays))
		return Occurrence{At: at, Precision: PrecisionDay}, true
	}

	// Counting from set_at leaves a plant added today due in a full interval
	// rather than overdue on arrival.
	since := s.SetAt
	if last != nil {
		since = last.PerformedAt
	}
	since = since.In(loc)

	switch {
	case s.AnchorDate == nil:
		at := advance(since, *s.IntervalCount, *s.IntervalUnit, 1)
		return Occurrence{At: at, Precision: PrecisionDay}, true

	case s.IntervalCount == nil:
		// A skip is a reminder, not a record of care, so it does not complete
		// a one-off. The comparison uses PerformedAt rather than RecordedAt so
		// that care given before the schedule was set does not complete it,
		// however late it was logged.
		if last != nil && last.Done && last.PerformedAt.After(s.SetAt) {
			return Occurrence{}, false
		}
		return Occurrence{At: anchorDay(*s.AnchorDate, loc), Precision: *s.AnchorPrecision}, true

	default:
		// The comparison is between days rather than instants, so care given
		// on the anchor's own day is that occurrence in every location. An
		// instant east of Greenwich can precede the anchor's midnight UTC
		// while falling on the anchor's day, and compared as an instant it
		// would leave the series due on the day it was just done.
		//
		// Each step counts from the anchor rather than from the step before
		// it, so a monthly series anchored on the 31st returns to the 31st
		// after February.
		anchor, sinceDay := anchorDay(*s.AnchorDate, loc), dayOf(since, loc)
		for k := firstStep(anchor, sinceDay, *s.IntervalCount, *s.IntervalUnit); ; k++ {
			at := advance(anchor, *s.IntervalCount, *s.IntervalUnit, k)
			if at.After(sinceDay) {
				return Occurrence{At: at, Precision: *s.AnchorPrecision}, true
			}
		}
	}
}

// firstStep returns the step the search for the first occurrence after sinceDay
// starts at. Every step before it falls before sinceDay. Starting at zero would
// take 3,650 steps for a daily series anchored ten years back.
//
// For months and years it counts calendar months and ignores the day, because
// the day is clamped to the month's length.
func firstStep(anchor, sinceDay time.Time, count int32, unit string) int {
	var elapsed, per int
	switch unit {
	case UnitDay:
		elapsed, per = DaysBetween(anchor, sinceDay), int(count)
	case UnitWeek:
		elapsed, per = DaysBetween(anchor, sinceDay), 7*int(count)
	case UnitMonth:
		elapsed, per = monthsBetween(anchor, sinceDay), int(count)
	case UnitYear:
		elapsed, per = monthsBetween(anchor, sinceDay), 12*int(count)
	default:
		return 0
	}
	if elapsed <= 0 {
		return 0
	}
	return elapsed / per
}

func monthsBetween(from, to time.Time) int {
	return (to.Year()-from.Year())*12 + int(to.Month()-from.Month())
}

func dayOf(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// anchorDay is midnight in loc of the date an anchor names. pgx scans a date
// column as midnight UTC, so reading it through In(loc) would give the evening
// before anywhere west of Greenwich.
func anchorDay(anchor time.Time, loc *time.Location) time.Time {
	y, m, d := anchor.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// inSeason reports whether m lies in the window from start to end, inclusive
// at both ends. A window whose end is before its start wraps the year.
func inSeason(m, start, end time.Month) bool {
	if start <= end {
		return m >= start && m <= end
	}
	return m >= start || m <= end
}

// lastOpening is the first day of the most recent window that opened at or
// before t.
func lastOpening(t time.Time, start time.Month) time.Time {
	o := time.Date(t.Year(), start, 1, 0, 0, 0, 0, t.Location())
	if o.After(t) {
		o = o.AddDate(-1, 0, 0)
	}
	return o
}

// nextOpening is the first day of the first window that opens after t.
func nextOpening(t time.Time, start time.Month) time.Time {
	o := time.Date(t.Year(), start, 1, 0, 0, 0, 0, t.Location())
	if !o.After(t) {
		o = o.AddDate(1, 0, 0)
	}
	return o
}

// advance adds n intervals of count units to t. An unknown unit panics, because
// the check constraint on care_schedule makes one unreachable, and a zero time
// returned instead would silently drop the plant from Today.
func advance(t time.Time, count int32, unit string, n int) time.Time {
	steps := int(count) * n
	switch unit {
	case UnitDay:
		return t.AddDate(0, 0, steps)
	case UnitWeek:
		return t.AddDate(0, 0, 7*steps)
	case UnitMonth:
		return addMonths(t, steps)
	case UnitYear:
		return addMonths(t, 12*steps)
	default:
		panic("schedule: unknown interval unit " + unit)
	}
}

// addMonths clamps the day to the target month's last day, so a schedule on the
// 31st falls on 28 February. time.AddDate would roll it into March.
func addMonths(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	if last := first.AddDate(0, 1, -1).Day(); d > last {
		d = last
	}
	hour, min, sec := t.Clock()
	return time.Date(first.Year(), first.Month(), d, hour, min, sec, t.Nanosecond(), t.Location())
}
