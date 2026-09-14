package schedule

import (
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

func TestInMonth(t *testing.T) {
	type date struct {
		at        time.Time
		month     bool
		estimated bool
		overdue   bool
	}
	september := utc(time.September, 1)

	cases := []struct {
		name  string
		sched store.CareSchedule
		last  *store.CareEvent
		// A zero now means set, 3 September 2026.
		now   time.Time
		month time.Time
		want  []date
	}{
		{
			name:  "a cadence lists its next occurrence and an estimated date for each interval after it",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 30)),
			month: september,
			want:  []date{{at: utc(time.September, 9)}, {at: utc(time.September, 19), estimated: true}, {at: utc(time.September, 29), estimated: true}},
		},
		{
			name:  "an overdue cadence is listed on today and its later dates count from today",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 20)),
			month: september,
			want: []date{
				{at: utc(time.September, 3), overdue: true},
				{at: utc(time.September, 13), estimated: true},
				{at: utc(time.September, 23), estimated: true},
			},
		},
		{
			name:  "a cadence in a later month continues from the dates in the months before it",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 30)),
			month: utc(time.October, 1),
			want:  []date{{at: utc(time.October, 9), estimated: true}, {at: utc(time.October, 19), estimated: true}, {at: utc(time.October, 29), estimated: true}},
		},
		{
			name:  "a monthly cadence from the 31st stays on the 28th after February",
			sched: cadence(set, 1, UnitMonth),
			last:  event(day(time.January, 31)),
			now:   day(time.February, 1),
			month: utc(time.March, 1),
			want:  []date{{at: utc(time.March, 28), estimated: true}},
		},
		{
			name:  "a month before the current one lists nothing",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 20)),
			month: utc(time.August, 1),
		},
		{
			name:  "an anchored series lists every anchor date in the month and none is estimated",
			sched: anchoredOn(set, 1, UnitWeek, utc(time.September, 1)),
			last:  event(day(time.September, 1)),
			month: september,
			want:  []date{{at: utc(time.September, 8)}, {at: utc(time.September, 15)}, {at: utc(time.September, 22)}, {at: utc(time.September, 29)}},
		},
		{
			name:  "an overdue anchored series is listed on today and its later dates stay on the anchor",
			sched: anchoredOn(set, 1, UnitWeek, utc(time.August, 4)),
			last:  event(day(time.August, 18)),
			month: september,
			want: []date{
				{at: utc(time.September, 3), overdue: true},
				{at: utc(time.September, 8)},
				{at: utc(time.September, 15)},
				{at: utc(time.September, 22)},
				{at: utc(time.September, 29)},
			},
		},
		{
			name:  "a one-off lists its anchor",
			sched: oneOff(set, utc(time.September, 20)),
			month: september,
			want:  []date{{at: utc(time.September, 20)}},
		},
		{
			name:  "a one-off anchored in another month lists nothing",
			sched: oneOff(set, utc(time.October, 20)),
			month: september,
		},
		{
			name:  "a completed one-off lists nothing",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), utc(time.September, 1)),
			last:  event(day(time.September, 2)),
			month: september,
		},
		{
			name:  "an override decides the first date of a cadence and the interval decides the dates after it",
			sched: cadence(set, 10, UnitDay),
			last:  override(skip(day(time.September, 1)), 5),
			month: september,
			want:  []date{{at: utc(time.September, 6)}, {at: utc(time.September, 16), estimated: true}, {at: utc(time.September, 26), estimated: true}},
		},
		{
			name:  "an anchored series returns to its anchor dates after an override's date",
			sched: anchoredOn(set, 1, UnitWeek, utc(time.September, 1)),
			last:  override(skip(day(time.September, 2)), 2),
			month: september,
			want:  []date{{at: utc(time.September, 4)}, {at: utc(time.September, 8)}, {at: utc(time.September, 15)}, {at: utc(time.September, 22)}, {at: utc(time.September, 29)}},
		},
		{
			name:  "a month-precise anchor is listed once as its month",
			sched: monthPrecise(oneOff(set, utc(time.October, 1))),
			month: utc(time.October, 1),
			want:  []date{{at: utc(time.October, 1), month: true}},
		},
		{
			name:  "a month-precise anchor in the current month is not overdue",
			sched: monthPrecise(oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), utc(time.September, 1))),
			month: september,
			want:  []date{{at: utc(time.September, 1), month: true}},
		},
		{
			name:  "a month-precise anchor from an earlier month is overdue in the current month",
			sched: monthPrecise(oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), utc(time.August, 1))),
			month: september,
			want:  []date{{at: utc(time.September, 1), month: true, overdue: true}},
		},
		{
			name:  "a cadence whose season is closed today starts its dates at the next opening",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   day(time.November, 15),
			month: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
			want: []date{
				{at: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)},
				{at: time.Date(2027, time.March, 22, 0, 0, 0, 0, time.UTC), estimated: true},
			},
		},
		{
			name:  "a cadence overdue from last season is due and not overdue on the day its season opens",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   time.Date(2027, time.March, 1, 10, 0, 0, 0, time.UTC),
			month: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
			want: []date{
				{at: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)},
				{at: time.Date(2027, time.March, 22, 0, 0, 0, 0, time.UTC), estimated: true},
			},
		},
		{
			name:  "a season's last month lists its dates",
			sched: seasonal(cadence(set, 1, UnitWeek), time.March, time.September),
			last:  event(day(time.September, 20)),
			month: september,
			want:  []date{{at: utc(time.September, 27)}},
		},
		{
			name:  "the month after a season closes lists nothing",
			sched: seasonal(cadence(set, 1, UnitWeek), time.March, time.September),
			last:  event(day(time.September, 20)),
			month: utc(time.October, 1),
		},
		{
			name:  "dates either side of a clock change are at midnight",
			sched: cadence(set, 3, UnitDay),
			// Noon in London on 22 October. The clocks go back on 25 October.
			last:  event(time.Date(2026, time.October, 22, 12, 0, 0, 0, london)),
			now:   time.Date(2026, time.October, 22, 13, 0, 0, 0, london),
			month: time.Date(2026, time.October, 1, 0, 0, 0, 0, london),
			want: []date{
				{at: time.Date(2026, time.October, 25, 0, 0, 0, 0, london)},
				{at: time.Date(2026, time.October, 28, 0, 0, 0, 0, london), estimated: true},
				{at: time.Date(2026, time.October, 31, 0, 0, 0, 0, london), estimated: true},
			},
		},
		{
			name:  "the month is read in now's timezone west of Greenwich",
			sched: oneOff(set, utc(time.August, 31)),
			// 23:30 on 31 August in Los Angeles is 1 September in UTC.
			now:   time.Date(2026, time.August, 31, 23, 30, 0, 0, losAngeles),
			month: time.Date(2026, time.August, 31, 23, 30, 0, 0, losAngeles),
			want:  []date{{at: time.Date(2026, time.August, 31, 0, 0, 0, 0, losAngeles)}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := c.now
			if now.IsZero() {
				now = set
			}
			got := InMonth(c.sched, c.last, c.month, now)
			if len(got) != len(c.want) {
				t.Fatalf("InMonth() returned %d dates, want %d: %v", len(got), len(c.want), got)
			}
			for i, w := range c.want {
				precision := PrecisionDay
				if w.month {
					precision = PrecisionMonth
				}
				g := got[i]
				if !g.At.Equal(w.at) || g.Precision != precision || g.Estimated != w.estimated || g.Overdue != w.overdue {
					t.Errorf("date %d = %s %s, estimated %t, overdue %t; want %s %s, estimated %t, overdue %t",
						i, g.At, g.Precision, g.Estimated, g.Overdue, w.at, precision, w.estimated, w.overdue)
				}
			}
		})
	}
}

// utc is midnight UTC on a day in 2026.
func utc(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
}
