package http

import (
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

func TestWhenWord(t *testing.T) {
	// A Thursday, so the weekdays inside the week run Friday to Wednesday.
	today := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		days int
		want string
	}{
		{1, "tomorrow"},
		{2, "Saturday"},
		{6, "Wednesday"},
		{7, "in 7 days"},
		{60, "in 60 days"},
		{61, "in November"},
		// July 2027 is inside the year, so the month alone says which one.
		{330, "in July"},
		{365, "in September 2027"},
		{600, "in April 2028"},
	}
	for _, c := range cases {
		if got := whenWord(c.days, today); got != c.want {
			t.Errorf("whenWord(%d) = %q, want %q", c.days, got, c.want)
		}
	}
}

func TestLateWord(t *testing.T) {
	if got := lateWord(1); got != "1 day late" {
		t.Errorf("lateWord(1) = %q, want 1 day late", got)
	}
	if got := lateWord(2); got != "2 days late" {
		t.Errorf("lateWord(2) = %q, want 2 days late", got)
	}
}

// A month-only schedule has no day, so neither the overdue nor the upcoming
// text counts days.
func TestOverdueWord(t *testing.T) {
	today := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	day := schedule.Line{Due: today.AddDate(0, 0, -2), Precision: schedule.PrecisionDay, Days: -2}
	month := schedule.Line{Due: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), Precision: schedule.PrecisionMonth, Days: -186}

	if got := overdueWord(day, today); got != "2 days late" {
		t.Errorf("a day-precise line reads %q, want 2 days late", got)
	}
	if got := overdueWord(month, today); got != "overdue since March" {
		t.Errorf("a month-precise line reads %q, want overdue since March", got)
	}
}

func TestNextLine_NamesOneOrTwoPlantsAndCountsThreeOrMore(t *testing.T) {
	today := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	row := func(name string) schedule.Row {
		return schedule.Row{
			Plant: store.Plant{Nickname: &name},
			Care:  schedule.Line{Due: today.AddDate(0, 0, 12), Precision: schedule.PrecisionDay, Days: 12},
		}
	}
	spike, doris, bigFella := row("Spike"), row("Doris"), row("Big Fella")

	cases := []struct {
		rows []schedule.Row
		want string
	}{
		{[]schedule.Row{spike}, "Spike is next, in 12 days."},
		{[]schedule.Row{doris, spike}, "Doris and Spike are next, in 12 days."},
		{[]schedule.Row{bigFella, doris, spike}, "Big Fella and 2 more are next, in 12 days."},
	}
	for _, c := range cases {
		if got := nextLine(c.rows, today); got != c.want {
			t.Errorf("%d plants read %q, want %q", len(c.rows), got, c.want)
		}
	}
}

func TestComingWord(t *testing.T) {
	today := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	day := schedule.Line{Due: today.AddDate(0, 0, 2), Precision: schedule.PrecisionDay, Days: 2}
	month := schedule.Line{Due: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC), Precision: schedule.PrecisionMonth, Days: 179}

	if got := comingWord(day, today); got != "Saturday" {
		t.Errorf("a day-precise line reads %q, want Saturday", got)
	}
	if got := comingWord(month, today); got != "in March 2027" {
		t.Errorf("a month-precise line reads %q, want in March 2027", got)
	}
}

func TestFeedWhen(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, london())
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"earlier today includes the time", now.Add(-2 * time.Hour), "today, 7:00am"},
		{"yesterday includes the time", now.AddDate(0, 0, -1).Add(9 * time.Hour), "yesterday, 6:00pm"},
		{"two days back is named as a day", now.AddDate(0, 0, -2), "Tuesday"},
		{"six days back is the last day named", now.AddDate(0, 0, -6), "Friday"},
		{"a week back is dated", now.AddDate(0, 0, -7), "27 Aug"},
		{"a year back is dated without the year", now.AddDate(-1, 0, 0), "3 Sep"},
		// An event's performed_at and the page's now are two reads of one
		// clock, so the event can be later by microseconds.
		{"an instant a shade after now is still today", now.Add(time.Millisecond), "today, 9:00am"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := feedWhen(c.at, now); got != c.want {
				t.Errorf("feedWhen = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFeedWhen_ReadsTheInstantInTheReadersZone(t *testing.T) {
	now := time.Date(2026, time.September, 3, 1, 0, 0, 0, london())
	at := time.Date(2026, time.September, 2, 23, 30, 0, 0, time.UTC)
	if got := feedWhen(at, now); got != "today, 12:30am" {
		t.Errorf("feedWhen = %q, want the half hour past midnight London reads it as", got)
	}
}

func TestDayHeading(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, london())
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"earlier today is named", now.Add(-2 * time.Hour), "Today"},
		{"yesterday is named too", now.AddDate(0, 0, -1), "Yesterday"},
		{"two days back is named as a day", now.AddDate(0, 0, -2), "Tuesday"},
		{"six days back is the last day named", now.AddDate(0, 0, -6), "Friday"},
		{"a week back is dated", now.AddDate(0, 0, -7), "27 August"},
		{"a day in another year takes the year", now.AddDate(-1, 0, 0), "3 September 2025"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dayHeading(c.at, now); got != c.want {
				t.Errorf("dayHeading = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDayHeading_ReadsTheInstantInTheReadersZone(t *testing.T) {
	now := time.Date(2026, time.September, 3, 1, 0, 0, 0, london())
	at := time.Date(2026, time.September, 2, 23, 30, 0, 0, time.UTC)
	if got := dayHeading(at, now); got != "Today" {
		t.Errorf("dayHeading = %q, want the day London was on at half past midnight", got)
	}
}

func TestEveryWord(t *testing.T) {
	cases := []struct {
		count int32
		unit  string
		want  string
	}{
		{1, "year", "year"},
		{1, "day", "day"},
		{3, "week", "3 weeks"},
		{10, "day", "10 days"},
	}
	for _, c := range cases {
		if got := everyWord(c.count, c.unit); got != c.want {
			t.Errorf("everyWord(%d, %q) = %q, want %q", c.count, c.unit, got, c.want)
		}
	}
}

func TestAnchorWord(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, london())
	cases := []struct {
		name      string
		due       time.Time
		precision string
		want      string
	}{
		{"a date in another year includes the year", date(2027, time.May, 1), "day", "1 May 2027"},
		{"a date this year does not", date(2026, time.December, 1), "day", "1 December"},
		{"a month-precise anchor keeps its vagueness", date(2028, time.March, 1), "month", "in March 2028"},
		{"a month this year is named alone", date(2026, time.November, 1), "month", "in November"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anchorWord(c.due, c.precision, now); got != c.want {
				t.Errorf("anchorWord = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAcquiredWord(t *testing.T) {
	year, month := int16(2024), int16(3)
	if got := acquiredWord(&year, &month); got != "March 2024" {
		t.Errorf("acquiredWord = %q, want March 2024", got)
	}
	if got := acquiredWord(&year, nil); got != "2024" {
		t.Errorf("a year with no month = %q, want 2024", got)
	}
	if got := acquiredWord(nil, &month); got != "" {
		t.Errorf("a month with no year = %q, want nothing, because a month alone is not a date", got)
	}
}

func TestAgoWord(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, london())
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"this morning is today", now.Add(-2 * time.Hour), "today"},
		// An event recorded a moment before the page was rendered can have a
		// later time than now.
		{"an instant a shade after now is still today", now.Add(time.Millisecond), "today"},
		{"last night is yesterday", now.AddDate(0, 0, -1).Add(9 * time.Hour), "yesterday"},
		{"two days back is named as a day", now.AddDate(0, 0, -2), "Tuesday"},
		{"a week back is dated", now.AddDate(0, 0, -7), "27 Aug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := agoWord(c.at, now); got != c.want {
				t.Errorf("agoWord = %q, want %q", got, c.want)
			}
		})
	}
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, london())
}
