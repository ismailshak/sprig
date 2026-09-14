package main

import (
	"testing"
	"time"

	engine "github.com/ismailshak/sprig/internal/schedule"
)

// A Wednesday in September, a month in which every seasonal schedule is in
// season.
func testReference(t *testing.T) time.Time {
	t.Helper()

	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("loading Europe/London: %v", err)
	}
	return time.Date(2026, time.September, 2, 0, 0, 0, 0, loc)
}

// A schedule is next due one interval after its newest event, so the seed
// places that event one interval before the due date it wants. Getting this
// wrong puts every plant in the wrong section of Today.
func TestOccurrences_TheNewestEventIsOneIntervalBeforeTheDueDate(t *testing.T) {
	ref := testReference(t)

	for _, tc := range []struct {
		name   string
		s      schedule
		newest time.Time
	}{
		{
			name:   "days",
			s:      schedule{slug: "water", count: 10, unit: engine.UnitDay, dueIn: -2},
			newest: time.Date(2026, time.August, 21, 0, 0, 0, 0, ref.Location()),
		},
		{
			name:   "weeks",
			s:      schedule{slug: "water", count: 3, unit: engine.UnitWeek, dueIn: 0},
			newest: time.Date(2026, time.August, 12, 0, 0, 0, 0, ref.Location()),
		},
		{
			// A month is a calendar month, not 30 days, so the answer is 4
			// August.
			name:   "months",
			s:      schedule{slug: "water", count: 1, unit: engine.UnitMonth, dueIn: 2},
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

func TestOccurrences_HistoryStopsAtTheHorizonAndBeforeToday(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "water", count: 4, unit: engine.UnitDay, dueIn: 0}

	// The horizon is exclusive and the newest event is 4 days ago, so the
	// oldest is 196 days ago and there are 49 events, not 50.
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

func TestOccurrences_AClosedSeasonLeavesAGapInTheHistory(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "feed", count: 3, unit: engine.UnitWeek, dueIn: 14, seasonStart: 3, seasonEnd: 9}

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
// last event, so its history steps back from the next calendar occurrence.
func TestSchedule_AnAnchoredScheduleIsNextDueOnItsNextCalendarOccurrence(t *testing.T) {
	ref := testReference(t)
	s := schedule{slug: "feed", count: 1, unit: engine.UnitYear, anchorMonth: time.May, anchorDay: 1}

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

// A skip pushes the schedule back by its own override rather than the
// interval, so a skip as the newest event would make the due date disagree
// with dueIn.
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

func TestHistory_ASkipHasAnOverrideAndADoneEventDoesNot(t *testing.T) {
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

// The history is derived from a hash rather than a random source, which is
// what lets the e2e suite assert against it.
func TestHistory_TwoRunsProduceTheSameEvents(t *testing.T) {
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

// If every event were the owner's, no page would ever show care given by
// someone else. history attributes events to a garden's first two members, so
// those are the two the log has to hold. The sitters after them joined long
// after the history starts and logged nothing.
func TestHistory_TheFirstTwoMembersBothAppearInTheLog(t *testing.T) {
	ref := testReference(t)
	g := home()

	by := map[string]int{}
	for i := range g.plants {
		for _, e := range history(&g, &g.plants[i], ref) {
			by[e.performedBy.handle]++
		}
	}
	for _, m := range g.members[:2] {
		if by[m.person.handle] == 0 {
			t.Errorf("%s appears in none of the garden's events", m.person.handle)
		}
	}
}

// performed_at and recorded_at exist to hold different instants. If the fixture
// always made them equal, code reading the wrong column would look correct.
func TestHistory_SomeEventsAreRecordedAfterTheyHappened(t *testing.T) {
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
		{"a month back", 1, engine.UnitMonth, -1, time.Date(2026, time.February, 22, 0, 0, 0, 0, loc)},
		{"four months on", 1, engine.UnitMonth, 4, time.Date(2026, time.July, 22, 0, 0, 0, 0, loc)},
		{"a year on", 1, engine.UnitYear, 1, time.Date(2027, time.March, 22, 0, 0, 0, 0, loc)},
		{"three weeks back", 3, engine.UnitWeek, -1, time.Date(2026, time.March, 1, 0, 0, 0, 0, loc)},
		{"ten days back", 10, engine.UnitDay, -1, time.Date(2026, time.March, 12, 0, 0, 0, 0, loc)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := advance(from, tc.count, tc.unit, tc.n); !got.Equal(tc.want) {
				t.Errorf("advance = %s, want %s", got.Format(time.DateOnly), tc.want.Format(time.DateOnly))
			}
		})
	}
}

// The clocks go forward on 29 March 2026 and back on 25 October.
func TestDaysBetween_CountsWholeDaysAcrossAClockChange(t *testing.T) {
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

// A plant needs at least one of its three names, or it renders as a blank row.
func TestFixture_EveryPlantHasAName(t *testing.T) {
	for _, g := range []garden{home(), upstairs()} {
		for i := range g.plants {
			if g.plants[i].displayName() == "" {
				t.Errorf("a plant in %s carries none of the three names", g.name)
			}
		}
	}
}

// The hand-written id must be a valid version 7 UUID, the same version the
// column generates by default.
func TestSeedID_IsAWellFormedVersion7UUID(t *testing.T) {
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

// The reference is a local midnight, so a whole number of days from it is a
// whole day whatever hour the seed runs.
func TestReference_IsMidnightTodayInTheGardensTimezone(t *testing.T) {
	// Late evening in New York is already the next day in London. A reference
	// taken in the machine's own zone would get this wrong.
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
