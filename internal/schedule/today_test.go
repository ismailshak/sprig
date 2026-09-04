package schedule

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// 21:30 UTC on 3 September 2026 is 22:30 in London the same evening and
// 09:30 in Auckland the next morning.
var evening = time.Date(2026, time.September, 3, 21, 30, 0, 0, time.UTC)

func TestResolve_ReadsTheDayInTheReadersLocation(t *testing.T) {
	// Due on 4 September at 08:00 UTC, which is 09:00 in London and 20:00
	// in Auckland.
	rows := []store.ListCareSchedulesRow{
		scheduleRow(plantNamed("Monty"), water, cadence(set, 10, UnitDay)),
	}
	events := []store.CareEvent{eventOn(rows[0].Plant, water, day(time.August, 25))}

	inLondon := Resolve(rows, events, evening.In(london))[0]
	if inLondon.State != Upcoming || inLondon.Days != 1 {
		t.Errorf("in London the watering is %v in %d days, want Upcoming tomorrow", inLondon.State, inLondon.Days)
	}
	if want := time.Date(2026, time.September, 4, 0, 0, 0, 0, london); !inLondon.Due.Equal(want) || inLondon.Due.Location() != london {
		t.Errorf("in London Due = %s, want %s", inLondon.Due, want)
	}

	inAuckland := Resolve(rows, events, evening.In(auckland))[0]
	if inAuckland.State != DueToday || inAuckland.Days != 0 {
		t.Errorf("in Auckland the watering is %v in %d days, want DueToday", inAuckland.State, inAuckland.Days)
	}
}

func TestResolve_CountsDaysAcrossAClockChange(t *testing.T) {
	// The clocks go forward on 29 March 2026, so that day is twenty-three
	// hours long.
	now := time.Date(2026, time.March, 28, 8, 0, 0, 0, london)
	rows := []store.ListCareSchedulesRow{
		scheduleRow(plantNamed("Monty"), water, cadence(set, 3, UnitDay)),
	}
	events := []store.CareEvent{eventOn(rows[0].Plant, water, time.Date(2026, time.March, 27, 23, 30, 0, 0, time.UTC))}

	got := Resolve(rows, events, now)[0]
	if got.Days != 2 {
		t.Errorf("Days = %d, want 2", got.Days)
	}
	if want := time.Date(2026, time.March, 30, 0, 0, 0, 0, london); !got.Due.Equal(want) {
		t.Errorf("Due = %s, want %s", got.Due, want)
	}
}

func TestResolve_State(t *testing.T) {
	march := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		sched store.CareSchedule
		last  *store.CareEvent
		now   time.Time
		want  State
		days  int
	}{
		{
			name:  "an occurrence yesterday is overdue",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 24)),
			now:   day(time.September, 4),
			want:  Overdue,
			days:  -1,
		},
		{
			name:  "an occurrence today is due today whatever the hour",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 25)),
			now:   time.Date(2026, time.September, 4, 23, 59, 0, 0, time.UTC),
			want:  DueToday,
		},
		{
			name:  "an occurrence tomorrow is upcoming",
			sched: cadence(set, 10, UnitDay),
			last:  event(day(time.August, 25)),
			now:   day(time.September, 3),
			want:  Upcoming,
			days:  1,
		},
		{
			name:  "a month-precise occurrence is upcoming until its month",
			sched: monthPrecise(oneOff(set, march)),
			now:   time.Date(2026, time.February, 28, 8, 0, 0, 0, time.UTC),
			want:  Upcoming,
			days:  1,
		},
		{
			name:  "a month-precise occurrence is due on the first of its month",
			sched: monthPrecise(oneOff(set, march)),
			now:   time.Date(2026, time.March, 1, 8, 0, 0, 0, time.UTC),
			want:  DueToday,
		},
		{
			name:  "a month-precise occurrence is still due on the last day of its month",
			sched: monthPrecise(oneOff(set, march)),
			now:   time.Date(2026, time.March, 31, 8, 0, 0, 0, time.UTC),
			want:  DueToday,
			days:  -30,
		},
		{
			name:  "a month-precise occurrence is overdue once its month has gone",
			sched: monthPrecise(oneOff(set, march)),
			now:   time.Date(2026, time.April, 1, 8, 0, 0, 0, time.UTC),
			want:  Overdue,
			days:  -31,
		},
		{
			name:  "a shut season is dormant",
			sched: seasonal(cadence(set, 3, UnitWeek), time.March, time.September),
			last:  event(day(time.August, 20)),
			now:   time.Date(2026, time.November, 3, 8, 0, 0, 0, time.UTC),
			want:  Dormant,
		},
		{
			name:  "a completed one-off is spent",
			sched: oneOff(time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC), march),
			last:  event(time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)),
			now:   day(time.September, 3),
			want:  Spent,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := []store.ListCareSchedulesRow{scheduleRow(plantNamed("Monty"), water, c.sched)}
			var events []store.CareEvent
			if c.last != nil {
				events = []store.CareEvent{eventOn(rows[0].Plant, water, c.last.PerformedAt)}
			}
			got := Resolve(rows, events, c.now)[0]
			if got.State != c.want {
				t.Errorf("State = %v, want %v", got.State, c.want)
			}
			if got.Days != c.days {
				t.Errorf("Days = %d, want %d", got.Days, c.days)
			}
			if (got.State >= Dormant) != got.Due.IsZero() {
				t.Errorf("State = %v with Due = %s", got.State, got.Due)
			}
		})
	}
}

func TestResolve_PairsEachScheduleWithItsOwnLatestEvent(t *testing.T) {
	monty, fern := plantNamed("Monty"), plantNamed("Fern")
	rows := []store.ListCareSchedulesRow{
		scheduleRow(monty, water, cadence(set, 10, UnitDay)),
		scheduleRow(monty, feed, cadence(set, 10, UnitDay)),
		scheduleRow(fern, water, cadence(set, 10, UnitDay)),
	}
	events := []store.CareEvent{
		eventOn(monty, water, day(time.August, 24)),
		eventOn(fern, water, day(time.August, 30)),
	}

	lines := Resolve(rows, events, day(time.September, 3))
	if lines[0].Last == nil || !lines[0].Last.PerformedAt.Equal(day(time.August, 24)) {
		t.Errorf("Monty's watering paired with %+v, want the 24 August event", lines[0].Last)
	}
	if lines[1].Last != nil {
		t.Errorf("Monty's feed paired with %+v, want no event", lines[1].Last)
	}
	if lines[2].Last == nil || !lines[2].Last.PerformedAt.Equal(day(time.August, 30)) {
		t.Errorf("Fern's watering paired with %+v, want the 30 August event", lines[2].Last)
	}
}

func TestToday_GroupsPlantsBySection(t *testing.T) {
	now := day(time.September, 3)
	rows := []store.ListCareSchedulesRow{
		scheduleRow(plantNamed("Late"), water, cadence(set, 10, UnitDay)),     // due 1 September
		scheduleRow(plantNamed("Now"), water, cadence(set, 10, UnitDay)),      // due today
		scheduleRow(plantNamed("Tomorrow"), water, cadence(set, 10, UnitDay)), // due 4 September
		scheduleRow(plantNamed("Week"), water, cadence(set, 10, UnitDay)),     // due 10 September
		scheduleRow(plantNamed("Beyond"), water, cadence(set, 10, UnitDay)),   // due 11 September
	}
	events := []store.CareEvent{
		eventOn(rows[0].Plant, water, day(time.August, 22)),
		eventOn(rows[1].Plant, water, day(time.August, 24)),
		eventOn(rows[2].Plant, water, day(time.August, 25)),
		eventOn(rows[3].Plant, water, day(time.August, 31)),
		eventOn(rows[4].Plant, water, day(time.September, 1)),
	}

	got := Today(Resolve(rows, events, now))
	if names := rowNames(got.Overdue); !slices.Equal(names, []string{"Late"}) {
		t.Errorf("Overdue = %v, want [Late]", names)
	}
	if names := rowNames(got.DueToday); !slices.Equal(names, []string{"Now"}) {
		t.Errorf("DueToday = %v, want [Now]", names)
	}
	if names := rowNames(got.ComingUp); !slices.Equal(names, []string{"Tomorrow", "Week"}) {
		t.Errorf("ComingUp = %v, want [Tomorrow Week]", names)
	}
	if got.Next == nil || got.Next.Plant.DisplayName() != "Beyond" || got.Next.Care.Days != 8 {
		t.Errorf("Next = %+v, want Beyond in 8 days", got.Next)
	}
}

func TestToday_OnePlantIsOneRowCarryingItsMostPressingCare(t *testing.T) {
	now := day(time.September, 3)
	monty := plantNamed("Monty")
	rows := []store.ListCareSchedulesRow{
		scheduleRow(monty, water, cadence(set, 10, UnitDay)), // due 8 September
		scheduleRow(monty, feed, cadence(set, 10, UnitDay)),  // due 1 September
		scheduleRow(monty, repot, monthPrecise(oneOff(set, time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)))),
	}
	events := []store.CareEvent{
		eventOn(monty, water, day(time.August, 29)),
		eventOn(monty, feed, day(time.August, 22)),
	}

	got := Today(Resolve(rows, events, now))
	if len(got.Overdue) != 1 || len(got.DueToday)+len(got.ComingUp) != 0 || got.Next != nil {
		t.Fatalf("Monty landed on %d, %d and %d rows with Next %v, want one overdue row", len(got.Overdue), len(got.DueToday), len(got.ComingUp), got.Next)
	}
	row := got.Overdue[0]
	if row.Care.CareType.Slug != "feed" || row.Care.Days != -2 {
		t.Errorf("the row's care is %s in %d days, want feed two days late", row.Care.CareType.Slug, row.Care.Days)
	}
	if len(row.Lines) != 3 {
		t.Errorf("the row carries %d lines, want all 3 of the plant's schedules", len(row.Lines))
	}
}

func TestToday_OverdueBeatsDueTodayBeatsUpcoming(t *testing.T) {
	now := day(time.September, 3)
	monty := plantNamed("Monty")
	rows := []store.ListCareSchedulesRow{
		scheduleRow(monty, water, cadence(set, 10, UnitDay)), // due today
		scheduleRow(monty, feed, cadence(set, 10, UnitDay)),  // due tomorrow
		// Due for the whole of September, so Days counts from the first.
		scheduleRow(monty, repot, monthPrecise(oneOff(set, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)))),
	}
	events := []store.CareEvent{
		eventOn(monty, water, day(time.August, 24)),
		eventOn(monty, feed, day(time.August, 25)),
	}

	got := Today(Resolve(rows, events, now))
	if len(got.DueToday) != 1 {
		t.Fatalf("Monty is on %d due-today rows, want 1", len(got.DueToday))
	}
	// soon ranks the repot level with the watering despite its negative Days,
	// so the watering keeps the row by arriving first.
	if slug := got.DueToday[0].Care.CareType.Slug; slug != "water" {
		t.Errorf("the row's care is %s, want water", slug)
	}
}

func TestToday_OrdersASectionByHowSoonThenByName(t *testing.T) {
	now := day(time.September, 3)
	rows := []store.ListCareSchedulesRow{
		scheduleRow(plantNamed("Zed"), water, cadence(set, 10, UnitDay)),
		scheduleRow(plantNamed("bert"), water, cadence(set, 10, UnitDay)),
		scheduleRow(plantNamed("Alma"), water, cadence(set, 10, UnitDay)),
		scheduleRow(plantNamed("Later"), water, cadence(set, 10, UnitDay)),
	}
	events := []store.CareEvent{
		eventOn(rows[0].Plant, water, day(time.August, 26)),
		eventOn(rows[1].Plant, water, day(time.August, 26)),
		eventOn(rows[2].Plant, water, day(time.August, 26)),
		eventOn(rows[3].Plant, water, day(time.August, 28)),
	}

	got := Today(Resolve(rows, events, now))
	if names := rowNames(got.ComingUp); !slices.Equal(names, []string{"Alma", "bert", "Zed", "Later"}) {
		t.Errorf("ComingUp = %v, want [Alma bert Zed Later]", names)
	}
}

func TestToday_LeavesOutAPlantWithNothingDue(t *testing.T) {
	now := time.Date(2026, time.November, 3, 8, 0, 0, 0, time.UTC)
	dormant, spent := plantNamed("Dormant"), plantNamed("Spent")
	rows := []store.ListCareSchedulesRow{
		scheduleRow(dormant, feed, seasonal(cadence(set, 3, UnitWeek), time.March, time.September)),
		scheduleRow(spent, repot, oneOff(set, time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC))),
	}
	events := []store.CareEvent{
		eventOn(spent, repot, time.Date(2026, time.October, 2, 8, 0, 0, 0, time.UTC)),
	}

	got := Today(Resolve(rows, events, now))
	if n := len(got.Overdue) + len(got.DueToday) + len(got.ComingUp); n != 0 || got.Next != nil {
		t.Errorf("Today holds %d rows and Next %v, want nothing", n, got.Next)
	}
}

func TestToday_NextIsTheSoonestBeyondTheWeek(t *testing.T) {
	now := day(time.September, 3)
	rows := []store.ListCareSchedulesRow{
		scheduleRow(plantNamed("Far"), water, cadence(set, 30, UnitDay)),
		scheduleRow(plantNamed("Near"), water, cadence(set, 30, UnitDay)),
	}
	events := []store.CareEvent{
		eventOn(rows[0].Plant, water, day(time.August, 20)),
		eventOn(rows[1].Plant, water, day(time.August, 15)),
	}

	got := Today(Resolve(rows, events, now))
	if got.Next == nil || got.Next.Plant.DisplayName() != "Near" || got.Next.Care.Days != 11 {
		t.Errorf("Next = %+v, want Near in 11 days", got.Next)
	}
}

var (
	water = careType("water")
	feed  = careType("feed")
	repot = careType("repot")
)

func careType(slug string) store.CareType {
	return store.CareType{ID: uuid.New(), Slug: slug, Name: slug}
}

func plantNamed(nickname string) store.Plant {
	return store.Plant{ID: uuid.New(), Nickname: &nickname}
}

func scheduleRow(p store.Plant, ct store.CareType, s store.CareSchedule) store.ListCareSchedulesRow {
	s.ID, s.PlantID, s.CareTypeID = uuid.New(), p.ID, ct.ID
	return store.ListCareSchedulesRow{CareSchedule: s, Plant: p, CareType: ct}
}

func eventOn(p store.Plant, ct store.CareType, performed time.Time) store.CareEvent {
	e := event(performed)
	e.PlantID, e.CareTypeID = p.ID, ct.ID
	return *e
}

func rowNames(rows []Row) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Plant.DisplayName())
	}
	return names
}
