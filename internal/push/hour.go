package push

import (
	"slices"
	"time"
)

// dateKey is the layout of a digest's send key: the date in the member's
// timezone, as an ISO date. The keys sort as dates, so the latest one is the
// last day a digest went out.
const dateKey = "2006-01-02"

// maxLate is how far past its hour a digest may still be sent. A process that
// was down over somebody's hour sends it on starting if it is within this and
// drops it otherwise, because a morning's digest arriving in the afternoon is
// worse than none.
const maxLate = time.Hour

// instantOf returns the first instant on the calendar day year, month, day in
// loc at which the clock reads hour:00.
//
// time.Date is not enough on the two days a year the clocks change. Where the
// hour does not exist, time.Date returns a time an hour past it, so the instant
// the clocks jump is found by search instead. Where the hour happens twice,
// time.Date picks one without saying which, so both are computed and the
// earlier taken.
func instantOf(year int, month time.Month, day, hour int, loc *time.Location) time.Time {
	// The wanted wall-clock time, held as a UTC instant so an offset can be
	// subtracted from it.
	wall := time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
	reads := func(t time.Time) bool {
		local := t.In(loc)
		y, m, d := local.Date()
		return y == year && m == month && d == day && local.Hour() == hour && local.Minute() == 0
	}

	// Every UTC offset in force within a day either side of the wall time.
	// Across a clock change there are two, giving two candidate instants.
	var candidates, valid []time.Time
	for _, probe := range []time.Time{wall.Add(-24 * time.Hour), wall, wall.Add(24 * time.Hour)} {
		_, offset := probe.In(loc).Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		candidates = append(candidates, candidate)
		if reads(candidate) {
			valid = append(valid, candidate)
		}
	}
	if len(valid) > 0 {
		return slices.MinFunc(valid, time.Time.Compare)
	}

	// No candidate reads the wanted hour, so it falls in the gap of a clock
	// change. The search below narrows to the instant the offset changes, which
	// is the first instant after the gap.
	lo := slices.MinFunc(candidates, time.Time.Compare)
	hi := slices.MaxFunc(candidates, time.Time.Compare)
	_, after := hi.In(loc).Zone()
	for hi.Sub(lo) > time.Second {
		mid := lo.Add(hi.Sub(lo) / 2)
		if _, offset := mid.In(loc).Zone(); offset == after {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

// nextDigest returns the instant of a member's next digest and the send key
// for it. sentThrough is the latest send key in the ledger for the membership,
// or empty when no digest has gone out.
//
// The instant can be up to maxLate in the past, and the caller sends that
// digest at once. A day whose hour is further past than that is passed over.
// skipped is the instant passed over, or zero when none was.
func nextDigest(hour int, loc *time.Location, sentThrough string, now time.Time) (at time.Time, key string, skipped time.Time) {
	// A calendar day is held as a UTC midnight, so AddDate never crosses a
	// clock change.
	y, m, d := now.In(loc).Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	// A timezone change can put today before the last day sent for, so the
	// later of the two is where the search starts.
	if sent, err := time.Parse(dateKey, sentThrough); err == nil && !sent.Before(day) {
		day = sent.AddDate(0, 0, 1)
	}
	for {
		at = instantOf(day.Year(), day.Month(), day.Day(), hour, loc)
		if now.Sub(at) < maxLate {
			return at, day.Format(dateKey), skipped
		}
		skipped = at
		day = day.AddDate(0, 0, 1)
	}
}
