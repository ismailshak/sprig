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
