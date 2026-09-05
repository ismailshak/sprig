package http

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
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

// The five care types a garden starts with have English forms their names
// cannot give. A type the map does not know falls back to its own name, as in
// "Log dust" and "Logged dust just now".
var careWords = map[string]struct{ noun, past string }{
	"water": {"watering", "watered"},
	"feed":  {"feeding", "fed"},
	"repot": {"repotting", "repotted"},
	"mist":  {"misting", "misted"},
	"prune": {"pruning", "pruned"},
}

// careNoun is the act as a noun, for "Log watering".
func careNoun(ct store.CareType) string {
	if w, ok := careWords[ct.Slug]; ok {
		return w.noun
	}
	return strings.ToLower(ct.Name)
}

// carePast is the act done, for "watered just now".
func carePast(ct store.CareType) string {
	if w, ok := careWords[ct.Slug]; ok {
		return w.past
	}
	return "logged " + strings.ToLower(ct.Name)
}

func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// stamp names an instant the way a row does, "Tue 1 Sep, 6:00pm".
func stamp(t time.Time) string {
	return t.Format("Mon 2 Jan, 3:04pm")
}

// feedWhen names when an event happened, as the feed on Today says it. Today
// and yesterday carry the clock because those are the two a reader checks
// against memory. The rest of the week names the day since a weekday is placed
// faster than a date.
func feedWhen(at, now time.Time) string {
	at = at.In(now.Location())
	switch days := schedule.DaysBetween(at, now); {
	case days <= 0:
		return "today, " + at.Format("3:04pm")
	case days == 1:
		return "yesterday, " + at.Format("3:04pm")
	case days <= 6:
		return at.Weekday().String()
	}
	return at.Format("2 Jan")
}
