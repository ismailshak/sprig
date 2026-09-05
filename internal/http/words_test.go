package http

import (
	"testing"
	"time"
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

func TestFeedWhen(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, london())
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"earlier today carries the clock", now.Add(-2 * time.Hour), "today, 7:00am"},
		{"yesterday carries it too", now.AddDate(0, 0, -1).Add(9 * time.Hour), "yesterday, 6:00pm"},
		{"two days back is named as a day", now.AddDate(0, 0, -2), "Tuesday"},
		{"six days back is the last day named", now.AddDate(0, 0, -6), "Friday"},
		{"a week back is dated", now.AddDate(0, 0, -7), "27 Aug"},
		{"a year back is dated without the year", now.AddDate(-1, 0, 0), "3 Sep"},
		// A care's performed_at and the page's now are two reads of one clock.
		// The first can be the later by microseconds.
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
