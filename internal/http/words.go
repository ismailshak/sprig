package http

import (
	"fmt"
	"time"
)

func daysWord(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func lateWord(days int) string {
	return daysWord(days) + " late"
}

// whenWord names the day a care falls on, days after today. A weekday reads
// against a plan for the week and holds for six days. Past two months a day
// count stops being an answer, so the month replaces it, and the year joins
// the month once the next twelve months no longer fix it.
func whenWord(days int, today time.Time) string {
	switch {
	case days == 1:
		return "tomorrow"
	case days <= 6:
		return today.AddDate(0, 0, days).Weekday().String()
	case days <= 60:
		return "in " + daysWord(days)
	}
	due := today.AddDate(0, 0, days)
	if due.Before(today.AddDate(1, 0, 0)) {
		return "in " + due.Month().String()
	}
	return fmt.Sprintf("in %s %d", due.Month(), due.Year())
}
