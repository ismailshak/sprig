package main

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	engine "github.com/ismailshak/sprig/internal/schedule"
)

// historyDays is how far back the generated event history goes. Bounding by
// days rather than by event count means every cadence stops on the same day
// however often it repeats.
const historyDays = 200

// logEntry is one care_event row before it is assigned an id.
type logEntry struct {
	plant    *plant
	careSlug string
	// performedAt is when the care happened and recordedAt when it was
	// entered. Due dates count from performedAt, so a backdated entry behaves
	// differently from one entered at the time.
	performedAt time.Time
	recordedAt  time.Time
	performedBy *person
	// done is false for a skip, which pushes the schedule back without
	// recording that care was given.
	done     bool
	note     string
	override int
}

// advance moves t by n intervals of count units. n may be negative. Months and
// years move by the calendar rather than by a fixed number of days, matching
// the due-date engine.
func advance(t time.Time, count int, unit string, n int) time.Time {
	switch unit {
	case engine.UnitDay:
		return t.AddDate(0, 0, count*n)
	case engine.UnitWeek:
		return t.AddDate(0, 0, 7*count*n)
	case engine.UnitMonth:
		return t.AddDate(0, count*n, 0)
	case engine.UnitYear:
		return t.AddDate(count*n, 0, 0)
	default:
		panic("unknown interval unit " + unit)
	}
}

// daysBetween counts whole calendar days, rounding away the hour a clock change
// adds or removes so that 30 days does not become 29 and a fraction.
func daysBetween(earlier, later time.Time) int {
	return int(later.Sub(earlier).Round(24*time.Hour) / (24 * time.Hour))
}

func (s *schedule) repeats() bool { return s.count > 0 }

// anchor returns the anchor date, placed relative to the reference year so the
// fixture keeps its place in the calendar whichever year the seed runs.
func (s *schedule) anchor(ref time.Time) time.Time {
	day := s.anchorDay
	if day == 0 {
		day = 1
	}
	return time.Date(ref.Year()+s.anchorYear, s.anchorMonth, day, 0, 0, 0, 0, ref.Location())
}

func (s *schedule) anchored() bool { return s.anchorMonth != 0 }

// next returns when the schedule next falls due. A cadence is written as a
// number of days from the reference, so the seeded garden lands in the same
// three sections of Today whatever day it is seeded. An anchored schedule
// names a calendar date instead.
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

// history generates every care event for one plant. It works each cadence
// backwards from its next due date and adds the plant's extraEvents.
func history(g *garden, p *plant, ref time.Time) []logEntry {
	var occs []occurrence
	for i := range p.schedules {
		occs = append(occs, occurrences(&p.schedules[i], ref)...)
	}
	for _, e := range p.extraEvents {
		occs = append(occs, occurrence{slug: e.slug, at: ref.AddDate(0, 0, -e.daysAgo)})
	}

	// Sort newest first within each care type. The first of each group is the
	// event the app's due dates count from.
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

// occurrences returns the past occurrences of a schedule, stepping back one
// interval at a time from the next due date. A one-off has no interval and so
// no history.
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
		// A seasonal schedule produces nothing out of season, so a seasonal
		// feed has a gap in the log every winter.
		if !s.inSeason(at.Month()) {
			continue
		}
		out = append(out, occurrence{slug: s.slug, at: at})
	}
}

// entryFor builds the event for one occurrence. Who performed it, the time of
// day and the note come from a hash of the event rather than a random source,
// so the log varies but is identical on every run.
//
// newest is true for the most recent event of its care type on this plant.
// That event is never a skip, because a skip pushes the schedule back by its
// own override rather than the interval, and the due date would then disagree
// with the plant list. Older events may be skips, since nothing reads past the
// newest one.
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
	// Attribute some events to the second member, so pages showing someone
	// else's care have rows.
	if hash(key+"y")%3 == 1 {
		e.performedBy = g.members[1].person
	}

	// Usually recorded when performed, occasionally hours later, so both
	// columns get exercised.
	e.recordedAt = e.performedAt
	if hash(key+"l")%9 == 0 {
		e.recordedAt = clockOn(o.at, 23, 0)
	}

	// Two different salts, because choosing the note with the same number that
	// decided whether there is a note would always pick the same sentence.
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

// hash is FNV-1a. The fixture needs a stable hash, not a strong one.
func hash(s string) uint32 {
	h := uint32(2166136261)
	for i := range len(s) {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// doneNotes is keyed by care type slug, so a feeding note never lands on a
// watering.
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
