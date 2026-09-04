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

// Occurrence is when a schedule falls due.
type Occurrence struct {
	At time.Time
	// PrecisionMonth means the whole of At's month is the occurrence and At is
	// the first of it.
	Precision string
}

// Next is when a schedule falls due, given the most recent event of its care
// type on its plant, or nil where there is none. The second result is false for
// a schedule with nothing to produce.
//
// The three shapes are told apart the way the schema tells them apart, by which
// of the nullable groups are set:
//
//	cadence   an interval and no anchor   the last event plus the interval
//	anchored  an interval and an anchor   the earliest anchor plus k intervals later than the last event
//	one-off   an anchor and no interval   the anchor, once
//
// A cadence counts from the event and an anchor from the calendar, so care
// given late moves a cadence and leaves an anchored series where it is.
//
// An override_interval_days on the last event replaces the answer the shape
// gives. A season on a cadence is a window of months, inclusive at both ends
// and wrapping at the year. While it is shut the schedule produces nothing,
// and while it is open an occurrence before the opening moves to the opening.
// The season applies to the override as well.
//
// now decides the season and nothing else. The season is read against now's
// calendar, so a caller passes now in the reader's location. An anchored series
// last done a year ago comes back as the occurrence that was missed, and the
// caller decides what counts as overdue.
func Next(s store.CareSchedule, last *store.CareEvent, now time.Time) (Occurrence, bool) {
	o, ok := shape(s, last)
	if !ok || s.SeasonStartMonth == nil {
		return o, ok
	}

	start, end := time.Month(*s.SeasonStartMonth), time.Month(*s.SeasonEndMonth)
	if !inSeason(now.Month(), start, end) {
		return Occurrence{}, false
	}

	// An occurrence left behind in a closed window would otherwise read as
	// months overdue. One past the close waits for the next opening, because
	// nobody is asked on a date the season is shut.
	loc := now.Location()
	if opening := lastOpening(now, start); o.At.Before(opening) {
		o.At = opening
	} else if !inSeason(o.At.In(loc).Month(), start, end) {
		o.At = nextOpening(o.At.In(loc), start)
	}
	return o, true
}

// shape is the occurrence before Next applies the season.
func shape(s store.CareSchedule, last *store.CareEvent) (Occurrence, bool) {
	if last != nil && last.OverrideIntervalDays != nil {
		// PerformedAt rather than RecordedAt, so a backdated skip counts from
		// when the plant was looked at rather than when somebody logged it.
		at := last.PerformedAt.AddDate(0, 0, int(*last.OverrideIntervalDays))
		return Occurrence{At: at, Precision: PrecisionDay}, true
	}

	// Counting from set_at leaves a plant added today due in a full interval
	// rather than overdue on arrival.
	since := s.SetAt
	if last != nil {
		since = last.PerformedAt
	}

	switch {
	case s.AnchorDate == nil:
		at := advance(since, *s.IntervalCount, *s.IntervalUnit, 1)
		return Occurrence{At: at, Precision: PrecisionDay}, true

	case s.IntervalCount == nil:
		// A skip asks to be reminded rather than recording the care, so it
		// leaves the one-off due. The comparison reads PerformedAt rather
		// than RecordedAt, so care given before the schedule was set does
		// not complete it however late somebody logged it.
		if last != nil && last.Done && last.PerformedAt.After(s.SetAt) {
			return Occurrence{}, false
		}
		return Occurrence{At: *s.AnchorDate, Precision: *s.AnchorPrecision}, true

	default:
		// Each step counts from the anchor rather than from the step before
		// it, so a monthly series anchored on the 31st returns to the 31st
		// after February.
		for k := 0; ; k++ {
			at := advance(*s.AnchorDate, *s.IntervalCount, *s.IntervalUnit, k)
			if at.After(since) {
				return Occurrence{At: at, Precision: *s.AnchorPrecision}, true
			}
		}
	}
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

// An unknown unit panics because care_schedule's check constraint makes one
// unreachable. Returning a zero time instead would drop the plant off Today and
// log nothing.
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
