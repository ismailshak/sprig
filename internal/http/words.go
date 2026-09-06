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

// overdueWord returns how far past due a schedule is, in lower case, such as
// "3 days late". A schedule precise only to a month names the month instead: a
// repot planned for March reads "overdue since March" on 1 April, since "31
// days late" would claim a date nobody gave.
func overdueWord(line schedule.Line, now time.Time) string {
	if line.Precision == schedule.PrecisionMonth {
		return "overdue since " + strings.TrimPrefix(anchorWord(line.Due, line.Precision, now), "in ")
	}
	return lateWord(-line.Days)
}

// comingWord returns when an upcoming schedule is due, such as "tomorrow" or
// "in 12 days". A schedule precise only to a month names the month, so a repot
// planned for March reads "in March" rather than "Thursday" on 26 February.
func comingWord(line schedule.Line, now time.Time) string {
	if line.Precision == schedule.PrecisionMonth {
		return anchorWord(line.Due, line.Precision, now)
	}
	return whenWord(line.Days, now)
}

// whenWord returns the day a care falls on, given days after today. Up to six
// days ahead it is a weekday name. Up to 60 days it is a count. Beyond that it
// is a month, with the year added once it is more than a year away.
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

// careWords holds the noun and past tense for the five default care types,
// which cannot be derived from the name. Any other type falls back to its name,
// as in "Log dust" and "Logged dust just now".
var careWords = map[string]struct{ noun, past string }{
	"water": {"watering", "watered"},
	"feed":  {"feeding", "fed"},
	"repot": {"repotting", "repotted"},
	"mist":  {"misting", "misted"},
	"prune": {"pruning", "pruned"},
}

// careNoun returns the care as a noun, as in "Log watering".
func careNoun(ct store.CareType) string {
	if w, ok := careWords[ct.Slug]; ok {
		return w.noun
	}
	return strings.ToLower(ct.Name)
}

// carePast returns the care in the past tense, as in "watered just now".
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

// stamp formats a time as "Tue 1 Sep, 6:00pm".
func stamp(t time.Time) string {
	return t.Format("Mon 2 Jan, 3:04pm")
}

// clockWord formats the time of day in the reader's zone, for a log row.
func clockWord(at, now time.Time) string {
	return at.In(now.Location()).Format("3:04pm")
}

// feedWhen formats when an event happened for the feed on Today. Today and
// yesterday include the time of day, since those are the two a reader checks
// against memory.
func feedWhen(at, now time.Time) string {
	when := agoWord(at, now)
	if schedule.DaysBetween(at, now) <= 1 {
		return when + ", " + clockWord(at, now)
	}
	return when
}

// whoDid returns the person and the action for an event line. The reader is
// "You", and a skipped care reads "skipped".
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

// dayHeading formats the day for a marker on Activity. Today and yesterday are
// named rather than dated, since those are the two a reader checks against
// memory.
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

// everyWord formats a schedule's interval to follow "Every". A count of one is
// dropped, so a yearly schedule reads "Every year" rather than "Every 1 year".
func everyWord(count int32, unit string) string {
	if count == 1 {
		return unit
	}
	return fmt.Sprintf("%d %ss", count, unit)
}

// seasonWord formats a season as "Mar–Sep".
func seasonWord(start, end int16) string {
	return fmt.Sprintf("%s–%s", shortMonth(time.Month(start)), shortMonth(time.Month(end)))
}

func shortMonth(m time.Month) string {
	return m.String()[:3]
}

// anchorWord formats the date an anchored schedule falls on, such as "1 May"
// or "in March" for a month-only anchor. The year is included only when it is
// not the current one.
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

// acquiredWord formats when a plant was acquired, as "2021" or "March 2021".
// It is empty when there is no year, since a month alone is not a date.
func acquiredWord(year, month *int16) string {
	if year == nil {
		return ""
	}
	if month == nil {
		return strconv.Itoa(int(*year))
	}
	return fmt.Sprintf("%s %d", time.Month(*month), *year)
}

// agoWord formats the day an event happened, as "today", "yesterday", a
// weekday or "2 Jan". It is lower case because it follows the action in the
// sentence.
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

// offNote is the Notifications row's note on More. A row reports its state
// only when that state is a loose end, so notifications that are on say
// nothing at all.
func offNote(on bool) string {
	if on {
		return ""
	}
	return "Off"
}

// waitingNote is the People row's note on More: how many invites have been
// issued and not taken up, and nothing when there are none.
func waitingNote(invites int) string {
	switch invites {
	case 0:
		return ""
	case 1:
		return "1 invite waiting"
	}
	return fmt.Sprintf("%d invites waiting", invites)
}

// shortRevision cuts a git revision to the seven characters a person reads it
// by. A binary built outside a git working tree has none, and gets none.
func shortRevision(revision string) string {
	const short = 7
	if len(revision) < short {
		return revision
	}
	return revision[:short]
}

// usedNote is the second line of a passkey or a browser row: "Last used
// today", or "Never used" where nothing has been recorded against it. A device
// that has never been seen is the interesting row in the list, since it is
// either the one to remove or the one whose setup did not finish.
func usedNote(at *time.Time, now time.Time) string {
	if at == nil {
		return "Never used"
	}
	return "Last used " + agoWord(*at, now)
}

// hourLabel formats an hour of the day for the digest select, as "8:00am".
func hourLabel(hour int16) string {
	return time.Date(2000, time.January, 1, int(hour), 0, 0, 0, time.UTC).Format("3:04pm")
}

// codesNote is the note on More's Account row, "No recovery codes" when the
// reader has none. It names recovery codes because the row itself is only
// labelled Account.
func codesNote(missing bool) string {
	if missing {
		return "No recovery codes"
	}
	return ""
}

// accountCodesNote is the note on Account's Recovery codes row when the reader
// has none. It does not repeat "recovery codes" because the row is labelled
// with them.
const accountCodesNote = "None yet"

// codesLeftWord is the first line of the batch on Recovery codes, "8 of 10
// left".
func codesLeftWord(unused, size int64) string {
	return fmt.Sprintf("%d of %d left", unused, size)
}
