package main

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// historyDays bounds the log by a date rather than by a count of events, so
// every cadence stops on the same day however often it repeats.
const historyDays = 200

// logEntry is one care_event before it has an identifier.
type logEntry struct {
	plant    *plant
	careSlug string
	// The next occurrence counts from performedAt rather than from recordedAt,
	// which separates a backdated entry from one written as it happened.
	performedAt time.Time
	recordedAt  time.Time
	performedBy *person
	// False is a skip, which resets the schedule without recording that the care
	// was given.
	done     bool
	note     string
	override int
}

// advance moves t by n intervals, and n may be negative. A month and a year
// move by the calendar rather than by a fixed number of days, because that is
// what a count and a unit mean to the due-date engine.
func advance(t time.Time, count int, unit string, n int) time.Time {
	switch unit {
	case unitDay:
		return t.AddDate(0, 0, count*n)
	case unitWeek:
		return t.AddDate(0, 0, 7*count*n)
	case unitMonth:
		return t.AddDate(0, count*n, 0)
	case unitYear:
		return t.AddDate(count*n, 0, 0)
	default:
		panic("unknown interval unit " + unit)
	}
}

// daysBetween counts whole days over the calendar, so the hour a clock change
// adds or removes does not turn thirty days into twenty-nine and a fraction.
func daysBetween(earlier, later time.Time) int {
	return int(later.Sub(earlier).Round(24*time.Hour) / (24 * time.Hour))
}

func (s *schedule) repeats() bool { return s.count > 0 }

// anchor is the day the series starts, placed in the reference's own year so
// the fixture keeps its position in the calendar whenever the seed runs.
func (s *schedule) anchor(ref time.Time) time.Time {
	day := s.anchorDay
	if day == 0 {
		day = 1
	}
	return time.Date(ref.Year()+s.anchorYear, s.anchorMonth, day, 0, 0, 0, 0, ref.Location())
}

func (s *schedule) anchored() bool { return s.anchorMonth != 0 }

// next is when the schedule falls due. A cadence is written as a number of days
// from the reference, which puts the seeded garden in the same three sections of
// Today whatever day it is seeded on. An anchored schedule names a place in the
// calendar instead.
func (s *schedule) next(ref time.Time) time.Time {
	if !s.anchored() {
		return ref.AddDate(0, 0, s.dueIn)
	}
	at := s.anchor(ref)
	for s.repeats() && !at.After(ref) {
		at = advance(at, s.count, s.unit, 1)
	}
	return at
}

func (s *schedule) inSeason(m time.Month) bool {
	if s.seasonStart == 0 {
		return true
	}
	start, end := time.Month(s.seasonStart), time.Month(s.seasonEnd)
	if start <= end {
		return m >= start && m <= end
	}
	return m >= start || m <= end
}

type occurrence struct {
	slug string
	at   time.Time
}

// history is every care event behind one plant. It works each cadence backwards
// from the day that cadence next falls due, and adds the events no cadence
// produced.
func history(g *garden, p *plant, ref time.Time) []logEntry {
	var occs []occurrence
	for i := range p.schedules {
		occs = append(occs, occurrences(&p.schedules[i], ref)...)
	}
	for _, e := range p.extraEvents {
		occs = append(occs, occurrence{slug: e.slug, at: ref.AddDate(0, 0, -e.daysAgo)})
	}

	// Newest first within a care type, so the first of each group is the one
	// every due date in the app counts from.
	slices.SortStableFunc(occs, func(a, b occurrence) int {
		if a.slug != b.slug {
			return cmp.Compare(a.slug, b.slug)
		}
		return b.at.Compare(a.at)
	})

	entries := make([]logEntry, 0, len(occs))
	for i, o := range occs {
		newest := i == 0 || occs[i-1].slug != o.slug
		entries = append(entries, entryFor(g, p, o, daysBetween(o.at, ref), newest))
	}
	return entries
}

// occurrences steps back through a schedule an interval at a time. A one-off
// has no interval, so it leaves no history.
func occurrences(s *schedule, ref time.Time) []occurrence {
	if !s.repeats() {
		return nil
	}

	next := s.next(ref)
	var out []occurrence
	for k := 1; ; k++ {
		at := advance(next, s.count, s.unit, -k)
		ago := daysBetween(at, ref)
		if ago >= historyDays {
			return out
		}
		if ago <= 0 {
			continue
		}
		// A schedule produced nothing while its season was shut, so a seasonal
		// feed leaves a gap in the log for every winter.
		if !s.inSeason(at.Month()) {
			continue
		}
		out = append(out, occurrence{slug: s.slug, at: at})
	}
}

// entryFor reads who performed the care, the hour of the day and the note out
// of a hash of the event rather than out of a random source, so the log varies
// and is still the same log the next time the seed runs.
//
// newest marks the most recent event of its care type on this plant, and that
// event may not be a skip. A skip resets the schedule by its own override rather
// than by the interval, so a skip in that position would make the log disagree
// with the roster. Older events are free to be skips, because nothing reads past
// the last one.
func entryFor(g *garden, p *plant, o occurrence, ago int, newest bool) logEntry {
	key := fmt.Sprintf("%s|%s|%d", p.displayName(), o.slug, ago)
	skipped := !newest && hash(key+"s")%13 == 0

	e := logEntry{
		plant:       p,
		careSlug:    o.slug,
		performedAt: clockOn(o.at, 7+int(hash(key+"h")%13), int(hash(key+"m")%60)),
		performedBy: g.members[0].person,
		done:        !skipped,
	}
	// The second member now and then, so a page showing somebody else's care has
	// rows to show.
	if hash(key+"y")%3 == 1 {
		e.performedBy = g.members[1].person
	}

	// Usually recorded as it was performed, and now and again hours later, which
	// is what the schema keeps two columns for.
	e.recordedAt = e.performedAt
	if hash(key+"l")%9 == 0 {
		e.recordedAt = clockOn(o.at, 23, 0)
	}

	// Two salts rather than one, because picking the note out of the number that
	// decided whether there is a note correlates the two and prints the same
	// sentence every time.
	switch {
	case skipped:
		e.note = pick(skipNotes, hash(key+"q"))
		e.override = 1 + int(hash(key+"z")%3)
	case hash(key+"n")%7 == 0:
		e.note = pick(doneNotes[o.slug], hash(key+"q"))
	}
	return e
}

func clockOn(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
}

func pick(from []string, n uint32) string {
	if len(from) == 0 {
		return ""
	}
	return from[int(n)%len(from)]
}

// hash is FNV-1a, which a fixture can use because it needs a stable answer
// rather than a strong one.
func hash(s string) uint32 {
	h := uint32(2166136261)
	for i := range len(s) {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// One pool of notes put "half strength" under a watering.
var doneNotes = map[string][]string{
	"water": {
		"Ran it until it came out of the bottom.",
		"New leaf unfurling.",
		"Bottom leaf has gone yellow.",
		"Wiped the dust off while I was there.",
	},
	"feed": {
		"Half strength, it is late in the season.",
		"First feed since the spring.",
		"Watered first, then fed.",
	},
	"repot": {
		"Went up one pot size.",
		"Roots had circled the bottom.",
	},
	"mist": {
		"Both sides of the fronds.",
		"Tips were browning again.",
	},
}

var skipNotes = []string{
	"Soil still damp.",
	"Pot still heavy when I lifted it.",
}
