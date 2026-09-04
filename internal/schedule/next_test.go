package schedule

import (
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

var set = time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)

// One zone east of Greenwich, one west, and one that changes its clocks.
var (
	auckland   = mustLoad("Pacific/Auckland")
	losAngeles = mustLoad("America/Los_Angeles")
	london     = mustLoad("Europe/London")
)

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestNext(t *testing.T) {
	cases := []struct {
		name  string
		sched store.CareSchedule
		last  *store.CareEvent
		now   time.Time
		// The zero time is a schedule with nothing left to produce.
		want  time.Time
		month bool
	}{
		{
			name:  "a cadence with no event counts from when the schedule was set",
			sched: cadence(set, 10, UnitDay),
			want:  time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC),
		},
		{
			name:  "a cadence counts from the last event",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 30)),
			want:  time.Date(2026, time.September, 9, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "watering late moves everything after it",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.September, 5)),
			want:  time.Date(2026, time.September, 15, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a backdated event counts from when the care happened",
			sched: cadence(set, 10, UnitDay),
			last:  backdated(day(time.August, 28), day(time.September, 3)),
			want:  time.Date(2026, time.September, 7, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a skip is still the latest event and resets the clock",
			sched: cadence(set, 10, UnitDay),
			last:  skip(day(time.September, 1)),
			want:  time.Date(2026, time.September, 11, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a cadence in weeks",
			sched: cadence(set, 3, UnitWeek),
			last:  event(day(time.August, 20)),
			want:  time.Date(2026, time.September, 10, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a cadence in months lands on the same day of the month",
			sched: cadence(set, 2, UnitMonth),
			last:  event(day(time.July, 15)),
			want:  time.Date(2026, time.September, 15, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a cadence in years lands on the same date",
			sched: cadence(set, 1, UnitYear),
			last:  event(day(time.April, 6)),
			want:  time.Date(2027, time.April, 6, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a cadence in months from the 31st takes the shorter month's last day",
			sched: cadence(set, 1, UnitMonth),
			last:  event(day(time.January, 31)),
			want:  time.Date(2026, time.February, 28, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "an anchored schedule with no event steps past the day it was set",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			want:  time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "an annual anchored schedule fed eleven days late still falls on its own date next year",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			last:  event(day(time.August, 12)),
			want:  time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "an anchored schedule not done since last year comes back overdue rather than skipped forward",
			sched: anchoredOn(time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC), 1, UnitYear, time.Date(2025, time.August, 1, 0, 0, 0, 0, time.UTC)),
			last:  event(time.Date(2025, time.August, 12, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "an anchored schedule steps by the whole interval",
			sched: anchoredOn(time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC), 2, UnitYear, time.Date(2025, time.August, 1, 0, 0, 0, 0, time.UTC)),
			last:  event(time.Date(2025, time.August, 3, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "care done on the anchor's own day is that occurrence",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			last:  event(day(time.August, 1)),
			want:  time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "care done at the anchor's first instant is that occurrence",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			last:  event(time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)),
			want:  time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "an anchor already passed when the schedule was set steps past it",
			sched: anchored(set, 1, UnitYear, time.March, 1),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "an anchor that has not arrived yet is the first occurrence",
			sched: anchored(set, 1, UnitYear, time.December, 1),
			want:  time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a monthly anchor on the 31st takes February's last day",
			sched: anchoredOn(set, 1, UnitMonth, time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)),
			last:  event(day(time.February, 3)),
			want:  time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a yearly anchor on 29 February takes the 28th in an ordinary year",
			sched: anchoredOn(set, 1, UnitYear, time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC)),
			last:  event(time.Date(2028, time.March, 1, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2029, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a monthly anchor returns to the 31st after a short February",
			sched: anchoredOn(set, 1, UnitMonth, time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)),
			last:  event(time.Date(2026, time.March, 5, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a one-off with no event is its anchor",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a one-off is not completed by care recorded before it was set",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  event(day(time.June, 1)),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a repot backfilled from before the schedule was set does not complete it",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  backdated(day(time.August, 1), day(time.September, 5)),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a completed one-off produces nothing rather than a date in the past",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  event(time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:  "a skip asks again rather than completing a one-off",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  skip(time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "an override beats a cadence",
			sched: cadence(set, 10, UnitDay),
			last:  override(skip(day(time.September, 1)), 2),
			want:  time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "an override beats an anchored series",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			last:  override(skip(day(time.August, 3)), 2),
			want:  time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "an override beats a one-off",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  override(skip(day(time.September, 5)), 7),
			want:  time.Date(2026, time.September, 12, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "an override on a month-precise one-off is a day",
			sched: monthPrecise(oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC))),
			last:  override(skip(day(time.September, 5)), 7),
			want:  time.Date(2026, time.September, 12, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a backdated override counts from when the plant was looked at",
			sched: cadence(set, 10, UnitDay),
			last:  override(skipBackdated(day(time.August, 30), day(time.September, 3)), 2),
			want:  time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "an override on a done event still wins",
			sched: cadence(set, 10, UnitDay),
			last:  override(event(day(time.September, 1)), 3),
			want:  time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "an override on a one-off's done event asks again rather than completing it",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  override(event(time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)), 10),
			want:  time.Date(2026, time.March, 12, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a month-precise one-off is its month",
			sched: monthPrecise(oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC))),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
			month: true,
		},
		{
			name:  "a month-precise anchored series is its month",
			sched: monthPrecise(anchored(set, 1, UnitYear, time.March, 1)),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
			month: true,
		},
		{
			name:  "care done late in a month-precise anchor's month is that occurrence",
			sched: monthPrecise(anchored(set, 1, UnitYear, time.March, 1)),
			last:  event(day(time.March, 28)),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
			month: true,
		},
		{
			name:  "a month-precise series not done since last year comes back as its month",
			sched: monthPrecise(anchoredOn(time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC), 1, UnitYear, time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC))),
			last:  event(time.Date(2025, time.March, 20, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
			month: true,
		},
		{
			name:  "care given on the anchor's morning east of Greenwich is that occurrence",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			// 09:00 in Auckland on 1 August, which is 21:00 UTC on 31 July.
			last: event(time.Date(2026, time.July, 31, 21, 0, 0, 0, time.UTC)),
			now:  time.Date(2026, time.August, 1, 10, 0, 0, 0, auckland),
			want: time.Date(2027, time.August, 1, 0, 0, 0, 0, auckland),
		},
		{
			name:  "care given the evening before the anchor west of Greenwich is not that occurrence",
			sched: anchored(set, 1, UnitYear, time.August, 1),
			// 20:00 in Los Angeles on 31 July, which is 03:00 UTC on 1 August.
			last: event(time.Date(2026, time.August, 1, 3, 0, 0, 0, time.UTC)),
			now:  time.Date(2026, time.August, 1, 10, 0, 0, 0, losAngeles),
			want: time.Date(2026, time.August, 1, 0, 0, 0, 0, losAngeles),
		},
		{
			name:  "an anchor read west of Greenwich is still its own date",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			now:   time.Date(2026, time.September, 3, 10, 0, 0, 0, losAngeles),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, losAngeles),
		},
		{
			name:  "a day added the night the clocks go forward is a calendar day",
			sched: cadence(set, 1, UnitDay),
			// 23:30 GMT on 28 March 2026, and the clocks go forward at 01:00.
			last: event(time.Date(2026, time.March, 28, 23, 30, 0, 0, time.UTC)),
			now:  time.Date(2026, time.March, 29, 8, 0, 0, 0, london),
			want: time.Date(2026, time.March, 29, 23, 30, 0, 0, london),
		},
		{
			name:  "a day added the night the clocks go back is a calendar day",
			sched: cadence(set, 1, UnitDay),
			// 00:30 BST on 25 October 2026, which is 23:30 UTC the day before.
			last: event(time.Date(2026, time.October, 24, 23, 30, 0, 0, time.UTC)),
			now:  time.Date(2026, time.October, 25, 8, 0, 0, 0, london),
			want: time.Date(2026, time.October, 26, 0, 30, 0, 0, london),
		},
		{
			name:  "an override added across a clock change is a calendar day",
			sched: cadence(set, 10, UnitDay),
			last:  override(skip(time.Date(2026, time.March, 28, 23, 30, 0, 0, time.UTC)), 1),
			now:   time.Date(2026, time.March, 29, 8, 0, 0, 0, london),
			want:  time.Date(2026, time.March, 29, 23, 30, 0, 0, london),
		},
		{
			name:  "a seasonal cadence inside its window is the usual computation",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   day(time.September, 3),
			want:  time.Date(2026, time.September, 10, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a seasonal cadence overdue inside its window stays where it fell",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   day(time.September, 20),
			want:  time.Date(2026, time.September, 10, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "the season shuts the day after its last month",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "the season is open to the end of its last month",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   time.Date(2026, time.September, 30, 23, 59, 0, 0, time.UTC),
			want:  time.Date(2026, time.September, 10, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "the season is open from the first instant of its first month",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a feed missed in September is not due again until the window opens",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   time.Date(2027, time.March, 5, 8, 0, 0, 0, time.UTC),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a feed that would fall after the close waits for the next opening",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.September, 25)),
			now:   day(time.September, 26),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a seasonal schedule set in winter is due when the window opens",
			sched: seasonal(cadence(day(time.November, 15), 3, UnitWeek), time.March, time.September),
			now:   time.Date(2027, time.March, 5, 8, 0, 0, 0, time.UTC),
			want:  time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a skip that asks to be reminded past the close is not due until the window opens",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  override(skip(day(time.September, 28)), 5),
			now:   day(time.October, 3),
		},
		{
			name:  "a wrapped season is open on the near side of the new year",
			sched: seasonal(cadence(set, 3, UnitWeek), time.November, time.February),
			last:  event(day(time.November, 20)),
			now:   day(time.December, 5),
			want:  time.Date(2026, time.December, 11, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a wrapped season is open on the far side of the new year",
			sched: seasonal(cadence(set, 3, UnitWeek), time.November, time.February),
			last:  event(day(time.December, 20)),
			now:   time.Date(2027, time.January, 15, 8, 0, 0, 0, time.UTC),
			want:  time.Date(2027, time.January, 10, 8, 0, 0, 0, time.UTC),
		},
		{
			name:  "a wrapped season is shut in the middle of the year",
			sched: seasonal(cadence(set, 3, UnitWeek), time.November, time.February),
			last:  event(day(time.February, 10)),
			now:   day(time.June, 15),
		},
		{
			name:  "a wrapped season asked about in January opened the previous November",
			sched: seasonal(cadence(set, 3, UnitWeek), time.November, time.February),
			last:  event(day(time.February, 10)),
			now:   time.Date(2027, time.January, 15, 8, 0, 0, 0, time.UTC),
			want:  time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a wrapped season's feed that would fall in spring waits for November",
			sched: seasonal(cadence(set, 3, UnitWeek), time.November, time.February),
			last:  event(day(time.February, 20)),
			now:   day(time.February, 21),
			want:  time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := c.now
			if now.IsZero() {
				now = set
			}
			got, ok := Next(c.sched, c.last, now)
			if c.want.IsZero() {
				if ok {
					t.Fatalf("Next() = %s, true, want no occurrence", got.At)
				}
				return
			}
			if !ok {
				t.Fatal("Next() reported no occurrence, want one")
			}
			if !got.At.Equal(c.want) {
				t.Errorf("Next().At = %s, want %s", got.At, c.want)
			}
			precision := PrecisionDay
			if c.month {
				precision = PrecisionMonth
			}
			if got.Precision != precision {
				t.Errorf("Next().Precision = %q, want %q", got.Precision, precision)
			}
		})
	}
}

func cadence(setAt time.Time, count int32, unit string) store.CareSchedule {
	return store.CareSchedule{IntervalCount: &count, IntervalUnit: &unit, SetAt: setAt}
}

func anchored(setAt time.Time, count int32, unit string, month time.Month, day int) store.CareSchedule {
	return anchoredOn(setAt, count, unit, time.Date(2026, month, day, 0, 0, 0, 0, time.UTC))
}

func anchoredOn(setAt time.Time, count int32, unit string, anchor time.Time) store.CareSchedule {
	s := cadence(setAt, count, unit)
	s.AnchorDate, s.AnchorPrecision = &anchor, ptr("day")
	return s
}

func oneOff(setAt, anchor time.Time) store.CareSchedule {
	return store.CareSchedule{AnchorDate: &anchor, AnchorPrecision: ptr("day"), SetAt: setAt}
}

// A month-precise anchor is stored as the first of its month.
func monthPrecise(s store.CareSchedule) store.CareSchedule {
	s.AnchorPrecision = ptr("month")
	return s
}

func seasonal(s store.CareSchedule, start, end time.Month) store.CareSchedule {
	s.SeasonStartMonth, s.SeasonEndMonth = ptr(int16(start)), ptr(int16(end)) //nolint:gosec // a month is 1 to 12
	return s
}

func day(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 8, 0, 0, 0, time.UTC)
}

func event(performed time.Time) *store.CareEvent {
	return backdated(performed, performed)
}

func backdated(performed, recorded time.Time) *store.CareEvent {
	return &store.CareEvent{PerformedAt: performed, RecordedAt: recorded, Done: true}
}

func skip(performed time.Time) *store.CareEvent {
	return skipBackdated(performed, performed)
}

func skipBackdated(performed, recorded time.Time) *store.CareEvent {
	e := backdated(performed, recorded)
	e.Done = false
	return e
}

func override(e *store.CareEvent, days int32) *store.CareEvent {
	e.OverrideIntervalDays = &days
	return e
}

func ptr[T any](v T) *T { return &v }
