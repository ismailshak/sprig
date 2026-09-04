package schedule

import (
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

var set = time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)

func TestNext(t *testing.T) {
	cases := []struct {
		name  string
		sched store.CareSchedule
		last  *store.CareEvent
		// The zero time is a schedule with nothing left to produce.
		want time.Time
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
			name:  "a one-off is not spent by care recorded before it was set",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  event(day(time.June, 1)),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a repot backfilled from before the schedule was set does not spend it",
			sched: oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  backdated(day(time.August, 1), day(time.September, 5)),
			want:  time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "a spent one-off produces nothing rather than a date in the past",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  event(time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:  "a skip asks again rather than spending a one-off",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)),
			last:  skip(time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)),
			want:  time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Next(c.sched, c.last)
			if c.want.IsZero() {
				if ok {
					t.Fatalf("Next() = %s, true, want no occurrence", got)
				}
				return
			}
			if !ok {
				t.Fatal("Next() reported no occurrence, want one")
			}
			if !got.Equal(c.want) {
				t.Errorf("Next() = %s, want %s", got, c.want)
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
	e := event(performed)
	e.Done = false
	return e
}

func ptr[T any](v T) *T { return &v }
