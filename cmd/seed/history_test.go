package main

import (
	"testing"
	"time"
	"uuid"
)

// A Wednesday in September, which is the day the prototype draws and a month
// in which every seasonal schedule is open.
func testReference(t *testing.T) time.Time {
	t.Helper()

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("loading Europe/London: %v", err)
	}
	return time.Date(2026, time.September, 2, 0, 0, 0, 0, loc)
}

// A schedule's most recent event plus one interval is when it next falls due,
// so the seed places that event one interval before the due date it wants. Get
// it wrong and every plant lands in the wrong section of Today.
func TestOccurrences_TheNewestEventIsOneIntervalBeforeTheScheduleFallsDue(t *testing.T) {
	ref := testReference(t)

	for _, tc := range []struct {
		name   string
		s      schedule
		newest time.Time
	}{
		{
			name:   "days",
			s:      schedule{slug: "water", count: 10, unit: unitDay, dueIn: -2},
			newest: time.Date(2026, time.August, 21, 0, 0, 0, 0, ref.Location()),
		},
		{
			name:   "weeks",
			s:      schedule{slug: "water", count: 3, unit: unitWeek, dueIn: 0},
			newest: time.Date(2026, time.August, 12, 0, 0, 0, 0, ref.Location()),
		},
		{
			// A month is the calendar's rather than thirty days, so the answer
			// is the fourth of August.
			name:   "months",
			s:      schedule{slug: "water", count: 1, unit: unitMonth, dueIn: 2},
			newest: time.Date(2026, time.August, 4, 0, 0, 0, 0, ref.Location()),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			occs := occurrences(&tc.s, ref)
			if len(occs) == 0 {
				t.Fatal("the schedule produced no events at all")
			}
			if !occs[0].at.Equal(tc.newest) {
				t.Errorf("newest event on %s, want %s", occs[0].at.Format(time.DateOnly), tc.newest.Format(time.DateOnly))
			}
		})
	}
}

func TestOccurrences_TheHistoryStopsAtTheHorizonAndAtToday(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "water", count: 4, unit: unitDay, dueIn: 0}

	// The horizon is exclusive and the newest event is four days back, so the
	// oldest is 196 days ago and there are 49 of them rather than 50.
	occs := occurrences(&s, ref)
	if len(occs) != 49 {
		t.Errorf("derived %d events over %d days of a four-day cadence, want 49", len(occs), historyDays)
	}
	for _, o := range occs {
		ago := daysBetween(o.at, ref)
		if ago <= 0 || ago >= historyDays {
			t.Errorf("event %s is %d days ago, want between 1 and %d", o.at.Format(time.DateOnly), ago, historyDays-1)
		}
	}
}

func TestOccurrences_AClosedSeasonLeavesAHoleInTheLog(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "feed", count: 3, unit: unitWeek, dueIn: 14, seasonStart: 3, seasonEnd: 9}

	for _, o := range occurrences(&s, ref) {
		if m := o.at.Month(); m < time.March || m > time.September {
			t.Errorf("a feeding in %s, and the schedule is shut from October to February", m)
		}
	}
}

func TestOccurrences_AOneOffProducesNoHistory(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "repot", anchorMonth: time.March, anchorYear: 2}

	if occs := occurrences(&s, ref); len(occs) != 0 {
		t.Errorf("a one-off produced %d events, want none", len(occs))
	}
}

// An anchored schedule is fixed to the calendar rather than counted from the
// last time it was done, so its history steps back from the next occurrence
// rather than from the last event.
func TestSchedule_AnAnchoredSeriesFallsDueOnItsNextOccurrence(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "feed", count: 1, unit: unitYear, anchorMonth: time.May, anchorDay: 1}

	if got, want := s.anchor(ref), time.Date(2026, time.May, 1, 0, 0, 0, 0, ref.Location()); !got.Equal(want) {
		t.Errorf("the series starts on %s, want %s", got.Format(time.DateOnly), want.Format(time.DateOnly))
	}
	if got, want := s.next(ref), time.Date(2027, time.May, 1, 0, 0, 0, 0, ref.Location()); !got.Equal(want) {
		t.Errorf("next due on %s, want %s", got.Format(time.DateOnly), want.Format(time.DateOnly))
	}

	occs := occurrences(&s, ref)
	if len(occs) != 1 {
		t.Fatalf("derived %d feedings inside the horizon, want the one in May", len(occs))
	}
	if got, want := occs[0].at, s.anchor(ref); !got.Equal(want) {
		t.Errorf("the last feeding was %s, want the series start %s", got.Format(time.DateOnly), want.Format(time.DateOnly))
	}
}

// A skip resets the schedule by its own override rather than by the interval,
// so a skip in the newest position would make the log disagree with the roster.
func TestHistory_TheNewestEventOfACareTypeIsNeverASkip(t *testing.T) {
	ref := testReference(t)
	g := home()

	skips := 0
	for i := range g.plants {
		seen := map[string]bool{}
		for _, e := range history(&g, &g.plants[i], ref) {
			if !e.done {
				skips++
			}
			if !seen[e.careSlug] {
				seen[e.careSlug] = true
				if !e.done {
					t.Errorf("%s's newest %s is a skip", g.plants[i].displayName(), e.careSlug)
				}
			}
		}
	}
	if skips == 0 {
		t.Error("the whole garden's log holds no skip, so the case is never exercised")
	}
}

func TestHistory_ASkipCarriesAnOverrideAndADoneEventDoesNot(t *testing.T) {
	ref := testReference(t)
	g := home()

	for i := range g.plants {
		for _, e := range history(&g, &g.plants[i], ref) {
			switch {
			case !e.done && e.override == 0:
				t.Errorf("a skipped %s on %s carries no override", e.careSlug, e.plant.displayName())
			case e.done && e.override != 0:
				t.Errorf("a completed %s on %s carries an override of %d", e.careSlug, e.plant.displayName(), e.override)
			}
		}
	}
}

// The log is read out of a hash rather than out of a random source, which is
// the whole reason the e2e suite can assert against it.
func TestHistory_TwoDerivationsAgree(t *testing.T) {
	ref := testReference(t)
	g := home()

	first := history(&g, &g.plants[0], ref)
	second := history(&g, &g.plants[0], ref)
	if len(first) != len(second) {
		t.Fatalf("derived %d events and then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("event %d differs between derivations:\n%+v\n%+v", i, first[i], second[i])
		}
	}
}

// A fixture where every event is the owner's never shows care somebody else
// gave.
func TestHistory_BothMembersAppearInTheLog(t *testing.T) {
	ref := testReference(t)
	g := home()

	by := map[string]int{}
	for i := range g.plants {
		for _, e := range history(&g, &g.plants[i], ref) {
			by[e.performedBy.handle]++
		}
	}
	for _, m := range g.members {
		if by[m.person.handle] == 0 {
			t.Errorf("%s appears in none of the garden's events", m.person.handle)
		}
	}
}

// The two columns exist to hold different instants, so a fixture where they
// always agree would let a reader of either one look right.
func TestHistory_AnEventIsRecordedWhenItHappenedOrAfterward(t *testing.T) {
	ref := testReference(t)
	g := home()

	late := 0
	for i := range g.plants {
		for _, e := range history(&g, &g.plants[i], ref) {
			if e.recordedAt.Before(e.performedAt) {
				t.Errorf("a %s on %s was recorded before it happened", e.careSlug, e.plant.displayName())
			}
			if e.recordedAt.After(ref) {
				t.Errorf("a %s on %s was recorded after the reference instant", e.careSlug, e.plant.displayName())
			}
			if e.recordedAt.After(e.performedAt) {
				late++
			}
		}
	}
	if late == 0 {
		t.Error("every event was recorded at the instant it happened, so the two columns are never told apart")
	}
}

func TestAdvance_MonthsAndYearsMoveByTheCalendar(t *testing.T) {
	loc := testReference(t).Location()
	from := time.Date(2026, time.March, 22, 0, 0, 0, 0, loc)

	for _, tc := range []struct {
		name  string
		count int
		unit  string
		n     int
		want  time.Time
	}{
		{"a month back", 1, unitMonth, -1, time.Date(2026, time.February, 22, 0, 0, 0, 0, loc)},
		{"four months on", 1, unitMonth, 4, time.Date(2026, time.July, 22, 0, 0, 0, 0, loc)},
		{"a year on", 1, unitYear, 1, time.Date(2027, time.March, 22, 0, 0, 0, 0, loc)},
		{"three weeks back", 3, unitWeek, -1, time.Date(2026, time.March, 1, 0, 0, 0, 0, loc)},
		{"ten days back", 10, unitDay, -1, time.Date(2026, time.March, 12, 0, 0, 0, 0, loc)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := advance(from, tc.count, tc.unit, tc.n); !got.Equal(tc.want) {
				t.Errorf("advance = %s, want %s", got.Format(time.DateOnly), tc.want.Format(time.DateOnly))
			}
		})
	}
}

// The clocks go forward on 29 March 2026 and back on 25 October.
func TestDaysBetween_ACountsWholeDaysAcrossAClockChange(t *testing.T) {
	loc := testReference(t).Location()

	for _, tc := range []struct {
		name           string
		earlier, later time.Time
		want           int
	}{
		{"across the spring forward", time.Date(2026, time.March, 28, 0, 0, 0, 0, loc), time.Date(2026, time.March, 30, 0, 0, 0, 0, loc), 2},
		{"across the autumn back", time.Date(2026, time.October, 24, 0, 0, 0, 0, loc), time.Date(2026, time.October, 26, 0, 0, 0, 0, loc), 2},
		{"a month spanning the spring forward", time.Date(2026, time.March, 1, 0, 0, 0, 0, loc), time.Date(2026, time.April, 1, 0, 0, 0, 0, loc), 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := daysBetween(tc.earlier, tc.later); got != tc.want {
				t.Errorf("daysBetween = %d, want %d", got, tc.want)
			}
		})
	}
}

// The three shapes are told apart by which fields are set, here and in the
// schema, and a fourth combination is a row no reader has an answer for.
func TestFixture_EveryScheduleIsOneOfTheThreeShapes(t *testing.T) {
	for _, g := range []garden{home(), upstairs()} {
		for i := range g.plants {
			p := &g.plants[i]
			for j := range p.schedules {
				s := &p.schedules[j]
				if !s.repeats() && !s.anchored() {
					t.Errorf("%s's %s schedule has neither an interval nor an anchor", p.displayName(), s.slug)
				}
				if s.repeats() && s.unit == "" {
					t.Errorf("%s's %s schedule has a count and no unit", p.displayName(), s.slug)
				}
				// An anchored schedule already names its month, so a season on
				// top of it would say nothing.
				if s.anchored() && s.seasonStart != 0 {
					t.Errorf("%s's %s schedule is anchored and carries a season", p.displayName(), s.slug)
				}
				if (s.seasonStart == 0) != (s.seasonEnd == 0) {
					t.Errorf("%s's %s schedule has half a season", p.displayName(), s.slug)
				}
			}
		}
	}
}

// A slug is how the seed finds the care type to hang a row off, so one that
// names nothing writes a schedule against a care type the garden lacks.
func TestFixture_EverySlugNamesACareTypeItsGardenHas(t *testing.T) {
	for _, g := range []garden{home(), upstairs()} {
		known := map[string]bool{}
		for _, ct := range g.careTypes {
			known[ct.slug] = true
		}
		for i := range g.plants {
			p := &g.plants[i]
			for _, s := range p.schedules {
				if !known[s.slug] {
					t.Errorf("%s in %s is scheduled for %q, which the garden has no care type for", p.displayName(), g.name, s.slug)
				}
			}
			for _, e := range p.extraEvents {
				if !known[e.slug] {
					t.Errorf("%s in %s has a %q event, which the garden has no care type for", p.displayName(), g.name, e.slug)
				}
			}
		}
	}
}

// At least one of the three names is required, because a plant with none of
// them renders as a blank row.
func TestFixture_EveryPlantHasAName(t *testing.T) {
	for _, g := range []garden{home(), upstairs()} {
		for i := range g.plants {
			if g.plants[i].displayName() == "" {
				t.Errorf("a plant in %s carries none of the three names", g.name)
			}
		}
	}
}

// Two rows sharing an identifier is the one way a written identifier fails
// where a generated one could not.
func TestSeedID_TheFixtureNamesEveryRowOnlyOnce(t *testing.T) {
	seen := map[uuid.UUID]string{}
	claim := func(id uuid.UUID, what string) {
		t.Helper()
		if prior, ok := seen[id]; ok {
			t.Errorf("%s and %s are both %v", prior, what, id)
		}
		seen[id] = what
	}

	for _, p := range []*person{&ellie, &sam, &robin} {
		claim(p.id, "the user "+p.handle)
	}
	for _, g := range []garden{home(), upstairs()} {
		claim(g.id, "the garden "+g.name)
		for _, m := range g.members {
			claim(m.id, m.person.handle+"'s membership of "+g.name)
		}
		for _, ct := range g.careTypes {
			claim(ct.id, g.name+"'s "+ct.slug)
		}
		for i := range g.plants {
			p := &g.plants[i]
			claim(p.id, "the plant "+p.displayName())
			for _, s := range p.schedules {
				claim(s.id, p.displayName()+"'s "+s.slug+" schedule")
			}
		}
	}
}

// The identifier is written by hand into a shape Postgres reads back as the
// version 7 the column defaults to.
func TestSeedID_IsAWellFormedVersion7Identifier(t *testing.T) {
	id := seedID(tableCareEvent, 319)
	if got, want := id.String(), "00000000-0000-7000-8000-070000000319"; got != want {
		t.Errorf("seedID = %s, want %s", got, want)
	}
	if got := id[6] >> 4; got != 7 {
		t.Errorf("version nibble = %d, want 7", got)
	}
	if got := id[8] >> 6; got != 0b10 {
		t.Errorf("variant bits = %02b, want 10", got)
	}
}

// The reference is a local midnight, so a whole number of days from it lands on
// a whole day whatever hour the seed was run at.
func TestReference_IsTheStartOfTodayWhereTheGardenIs(t *testing.T) {
	// Late enough in the evening in New York to be the following day in London,
	// which is the case a reference taken in the machine's own zone gets wrong.
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("loading America/New_York: %v", err)
	}
	now := time.Date(2026, time.September, 1, 22, 30, 0, 0, newYork)

	ref, err := reference(now)
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	if got, want := ref.Format(time.DateOnly), "2026-09-02"; got != want {
		t.Errorf("reference = %s, want %s", got, want)
	}
	if h, m, s := ref.Clock(); h != 0 || m != 0 || s != 0 {
		t.Errorf("reference is at %02d:%02d:%02d, want midnight", h, m, s)
	}
}

func TestCheckIsLocal(t *testing.T) {
	for _, tc := range []struct {
		url     string
		allowed bool
	}{
		{"postgres://sprig:sprig@localhost:5432/sprig", true},
		{"postgres://sprig:sprig@127.0.0.1:5432/sprig", true},
		{"postgres://sprig:sprig@db:5432/sprig", true},
		{"postgres://sprig:sprig@sprig.perpetualpondering.com:5432/sprig", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			err := checkIsLocal(tc.url)
			if tc.allowed && err != nil {
				t.Errorf("refused a local database: %v", err)
			}
			if !tc.allowed && err == nil {
				t.Error("accepted a database that is not on this machine")
			}
		})
	}
}
