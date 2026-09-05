package http

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ismailshak/sprig/internal/auth"
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

// overdueWord is how far past its day a schedule has gone, in the lower case a
// row reads it in. A month-precise occurrence names the month it has passed
// rather than counting days it was never precise enough to earn: a repot
// pencilled for March reads "overdue since March" on 1 April, where "31 days
// late" claims a date nobody gave.
func overdueWord(line schedule.Line, now time.Time) string {
	if line.Precision == schedule.PrecisionMonth {
		return "overdue since " + strings.TrimPrefix(anchorWord(line.Due, line.Precision, now), "in ")
	}
	return lateWord(-line.Days)
}

// comingWord is when a schedule still to come falls due. A month-precise
// occurrence names its month in this direction too, so a repot pencilled for
// March reads "in March" rather than "Thursday" on the 26th of February.
func comingWord(line schedule.Line, now time.Time) string {
	if line.Precision == schedule.PrecisionMonth {
		return anchorWord(line.Due, line.Precision, now)
	}
	return whenWord(line.Days, now)
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
// against memory.
func feedWhen(at, now time.Time) string {
	at = at.In(now.Location())
	when := agoWord(at, now)
	if schedule.DaysBetween(at, now) <= 1 {
		return when + ", " + at.Format("3:04pm")
	}
	return when
}

// whoDid names the person and the action, as every line about an event does.
// The reader is "You", and a care that was not done reads "skipped".
func whoDid(principal auth.Principal, performedBy string, e store.CareEvent, ct store.CareType) (who, did string) {
	who, did = performedBy, carePast(ct)
	if e.PerformedBy == principal.User.ID {
		who = "You"
	}
	if !e.Done {
		did = "skipped"
	}
	return who, did
}

// dayHeading names the day a marker on Activity stands for. Today and
// yesterday are named rather than dated because a reader checks those two
// against memory.
func dayHeading(at, now time.Time) string {
	at = at.In(now.Location())
	switch days := schedule.DaysBetween(at, now); {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days <= 6:
		return at.Weekday().String()
	}
	if at.Year() == now.Year() {
		return at.Format("2 January")
	}
	return at.Format("2 January 2006")
}

// everyWord is a schedule's interval as the row states it, following the word
// "Every". A count of one is dropped, so a yearly schedule reads "Every year"
// rather than "Every 1 year".
func everyWord(count int32, unit string) string {
	if count == 1 {
		return unit
	}
	return fmt.Sprintf("%d %ss", count, unit)
}

// seasonWord is the months a seasonal schedule runs between, as the left of a
// schedule row carries them.
func seasonWord(start, end int16) string {
	return fmt.Sprintf("%s–%s", shortMonth(time.Month(start)), shortMonth(time.Month(end)))
}

func shortMonth(m time.Month) string {
	return m.String()[:3]
}

// anchorWord names the day an anchored schedule falls on, which is what
// somebody wrote down rather than a count towards it. A month-precise anchor
// keeps its vagueness and reads "in March". The year is dropped inside the
// current one, where the month and the day already fix the occurrence.
func anchorWord(due time.Time, precision string, now time.Time) string {
	year := ""
	if due.Year() != now.Year() {
		year = fmt.Sprintf(" %d", due.Year())
	}
	if precision == schedule.PrecisionMonth {
		return fmt.Sprintf("in %s%s", due.Month(), year)
	}
	return fmt.Sprintf("%d %s%s", due.Day(), due.Month(), year)
}

// acquiredWord is when a plant arrived, as the foot of the reference panel
// gives it. It is empty for a plant with no year, since a month alone is not a
// date.
func acquiredWord(year, month *int16) string {
	if year == nil {
		return ""
	}
	if month == nil {
		return strconv.Itoa(int(*year))
	}
	return fmt.Sprintf("%s %d", time.Month(*month), *year)
}

// agoWord names the day an event happened, as a plant's Recent gives it and
// the feed builds on. It carries no capital because it follows the action in
// the sentence.
func agoWord(at, now time.Time) string {
	at = at.In(now.Location())
	switch days := schedule.DaysBetween(at, now); {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days <= 6:
		return at.Weekday().String()
	}
	return at.Format("2 Jan")
}
