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

// Next is when a schedule falls due, given the most recent event of its care
// type on its plant, or nil where there is none. The second result is false for
// a schedule with nothing left to produce.
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
// Nothing here reads the clock. An anchored series last done a year ago comes
// back as the occurrence that was missed, and the caller decides what counts as
// overdue.
func Next(s store.CareSchedule, last *store.CareEvent) (time.Time, bool) {
	// Counting from set_at leaves a plant added today due in a full interval
	// rather than overdue on arrival.
	since := s.SetAt
	if last != nil {
		since = last.PerformedAt
	}

	switch {
	case s.AnchorDate == nil:
		return advance(since, *s.IntervalCount, *s.IntervalUnit, 1), true

	case s.IntervalCount == nil:
		// A skip asks to be reminded rather than recording the care, so it
		// leaves the one-off due. The comparison reads PerformedAt rather
		// than RecordedAt, so care given before the schedule was set does
		// not spend it however late somebody logged it.
		if last != nil && last.Done && last.PerformedAt.After(s.SetAt) {
			return time.Time{}, false
		}
		return *s.AnchorDate, true

	default:
		// Each step counts from the anchor rather than from the step before
		// it, so a monthly series anchored on the 31st returns to the 31st
		// after February.
		for k := 0; ; k++ {
			at := advance(*s.AnchorDate, *s.IntervalCount, *s.IntervalUnit, k)
			if at.After(since) {
				return at, true
			}
		}
	}
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
