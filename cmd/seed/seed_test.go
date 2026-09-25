package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/pgtest"
	engine "github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// seededTables lists the tables two runs of the seed must write identically.
var seededTables = []string{
	"garden", "app_user", "membership",
	"care_type", "plant", "care_schedule", "care_event",
	"passkey_credential", "invite", "recovery_code", "api_token",
	"push_subscription", "notification_preference", "notification_send",
}

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// migrateSchema migrates the template database that every test database in
// this package is copied from.
func migrateSchema(ctx context.Context, databaseURL string) error {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	// CREATE DATABASE refuses a template another session is connected to.
	defer pool.Close()

	return store.Migrate(ctx, pool, db.Migrations, slog.New(slog.DiscardHandler))
}

// seeded returns a fresh database with the seed applied. The seed commits, so
// these tests cannot share one database and roll back.
func seeded(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := store.Open(t.Context(), pgtest.Fresh(t, migrateSchema))
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := seed(t.Context(), pool, testReference(t)); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	return pool
}

// The e2e suite seeds from empty and asserts against what it finds, so any
// difference between two runs would make the suite fail on a day nobody
// changed anything.
func TestSeed_RunningItTwiceWritesTheSameRows(t *testing.T) {
	pool := seeded(t)
	first := dumpRows(t, pool)

	if _, err := seed(t.Context(), pool, testReference(t)); err != nil {
		t.Fatalf("seeding a second time: %v", err)
	}
	second := dumpRows(t, pool)

	for _, table := range seededTables {
		if len(first[table]) == 0 {
			t.Errorf("%s holds no rows after seeding", table)
			continue
		}
		if len(first[table]) != len(second[table]) {
			t.Errorf("%s held %d rows and then %d", table, len(first[table]), len(second[table]))
			continue
		}
		for i := range first[table] {
			if first[table][i] != second[table][i] {
				t.Errorf("%s row %d differs between runs:\n%s\n%s", table, i, first[table][i], second[table][i])
				break
			}
		}
	}
}

// An e2e test can give a seeded person a garden of their own. The next reseed
// then fails to delete the person while that membership exists.
func TestSeed_AGardenASeededPersonJoinedBetweenRunsIsDeletedByTheNextRun(t *testing.T) {
	pool := seeded(t)
	var gardenID uuid.UUID
	if err := pool.QueryRow(t.Context(), "INSERT INTO garden (name) VALUES ('Greenhouse') RETURNING id").Scan(&gardenID); err != nil {
		t.Fatalf("adding a garden: %v", err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", gardenID, sam.id); err != nil {
		t.Fatalf("adding %s's membership: %v", sam.handle, err)
	}

	if _, err := seed(t.Context(), pool, testReference(t)); err != nil {
		t.Fatalf("seeding a second time: %v", err)
	}

	var gardens int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM garden WHERE id = $1", gardenID).Scan(&gardens); err != nil {
		t.Fatalf("counting the garden: %v", err)
	}
	if gardens != 0 {
		t.Error("the garden added between runs is still there")
	}
}

// The seed places a cadence's newest event one interval before its dueIn, so
// Today has rows in all three of its sections. Postgres does the date
// arithmetic here rather than the seed's own advance function, so a mistake in
// that function is caught.
func TestSeed_EveryCadenceFallsDueOnItsDueInDay(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)

	want := map[uuid.UUID]string{}
	for _, g := range []garden{home(), upstairs()} {
		for i := range g.plants {
			for _, s := range g.plants[i].schedules {
				if s.repeats() && !s.anchored() {
					want[s.id] = ref.AddDate(0, 0, s.dueIn).Format(time.DateOnly)
				}
			}
		}
	}

	rows, err := pool.Query(t.Context(), `
		WITH latest AS (
			SELECT DISTINCT ON (plant_id, care_type_id) plant_id, care_type_id, performed_at
			FROM care_event
			ORDER BY plant_id, care_type_id, performed_at DESC
		)
		SELECT s.id,
			to_char(
				(date(l.performed_at AT TIME ZONE $1)
					+ (s.interval_count || ' ' || s.interval_unit)::interval)::date,
				'YYYY-MM-DD')
		FROM care_schedule s
		JOIN latest l ON l.plant_id = s.plant_id AND l.care_type_id = s.care_type_id
		WHERE s.anchor_date IS NULL`, ellie.timezone)
	if err != nil {
		t.Fatalf("reading the schedules: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var id uuid.UUID
		var due string
		if err := rows.Scan(&id, &due); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		seen++
		if due != want[id] {
			t.Errorf("schedule %v falls due on %s, want %s", id, due, want[id])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the schedules: %v", err)
	}
	if seen != len(want) {
		t.Errorf("%d of %d cadences have an event behind them", seen, len(want))
	}
}

// A completed one-off produces no due date, so one in the fixture would leave
// a schedule line missing from a plant page. The check uses schedule.Next
// rather than a copy of the rule in SQL.
func TestSeed_NoOneOffIsCompleted(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)
	ctx := t.Context()
	queries := store.New(pool)

	for _, g := range []garden{home(), upstairs()} {
		schedules, err := queries.ListCareSchedules(ctx, g.id)
		if err != nil {
			t.Fatalf("listing %s's schedules: %v", g.name, err)
		}
		events, err := queries.ListLatestCareEvents(ctx, g.id)
		if err != nil {
			t.Fatalf("listing %s's latest events: %v", g.name, err)
		}

		type key struct{ plant, careType uuid.UUID }
		latest := map[key]*store.CareEvent{}
		for i := range events {
			latest[key{events[i].PlantID, events[i].CareTypeID}] = &events[i]
		}

		for _, row := range schedules {
			if row.CareSchedule.IntervalCount != nil {
				continue
			}
			last := latest[key{row.CareSchedule.PlantID, row.CareSchedule.CareTypeID}]
			if _, ok := engine.Next(row.CareSchedule, last, ref); !ok {
				t.Errorf("the %s one-off on plant %v is completed and produces no date", row.CareType.Slug, row.Plant.ID)
			}
		}
	}
}

// The second garden exists to catch a query missing its garden WHERE clause.
// That only works if none of its rows is reachable from the first garden.
func TestSeed_NoRowOfTheSecondGardenIsReachableFromTheFirst(t *testing.T) {
	pool := seeded(t)
	ctx := t.Context()
	queries := store.New(pool)

	first, second := home(), upstairs()
	strangers := map[uuid.UUID]bool{}
	for i := range second.plants {
		strangers[second.plants[i].id] = true
	}

	plants, err := queries.ListPlants(ctx, first.id)
	if err != nil {
		t.Fatalf("listing %s's plants: %v", first.name, err)
	}
	for _, p := range plants {
		if strangers[p.ID] {
			t.Errorf("%s's plant list holds a plant belonging to %s", first.name, second.name)
		}
	}

	schedules, err := queries.ListCareSchedules(ctx, first.id)
	if err != nil {
		t.Fatalf("listing %s's schedules: %v", first.name, err)
	}
	for _, s := range schedules {
		if strangers[s.Plant.ID] {
			t.Errorf("%s's schedules hold one for a plant belonging to %s", first.name, second.name)
		}
	}

	// Another garden's plant returns 404, not 403, so the query must return
	// no row rather than a row a handler then rejects.
	if _, err := queries.GetPlant(ctx, first.id, second.plants[0].id); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("reading %s's plant from %s returned %v, want pgx.ErrNoRows", second.name, first.name, err)
	}
}

// The plant list omits archived plants and archived care types. The seed writes
// one of each so a query that forgets to filter them is caught.
func TestSeed_ThePlantListOmitsArchivedPlantsAndCareTypes(t *testing.T) {
	pool := seeded(t)
	ctx := t.Context()
	queries := store.New(pool)
	first := home()

	plants, err := queries.ListPlants(ctx, first.id)
	if err != nil {
		t.Fatalf("listing plants: %v", err)
	}
	if got, want := len(plants), len(livingPlants()); got != want {
		t.Errorf("the plant list holds %d plants, want the %d that are not archived", got, want)
	}

	careTypes, err := queries.ListCareTypes(ctx, first.id)
	if err != nil {
		t.Fatalf("listing care types: %v", err)
	}
	for _, ct := range careTypes {
		if ct.Slug == "mist" {
			t.Error("the archived care type is still in the scheduler's list")
		}
	}
	if got, want := len(careTypes), len(first.careTypes)-1; got != want {
		t.Errorf("the garden offers %d care types, want %d", got, want)
	}
}

// One account in two gardens is why session has a garden_id. If every user
// had one membership, a query resolving a user to "their garden" would look
// correct.
func TestSeed_OneAccountHoldsMembershipsInBothGardens(t *testing.T) {
	pool := seeded(t)

	rows, err := pool.Query(t.Context(), `
		SELECT g.name FROM membership m
		JOIN garden g ON g.id = m.garden_id
		WHERE m.user_id = $1
		ORDER BY g.created_at`, sam.id)
	if err != nil {
		t.Fatalf("reading %s's memberships: %v", sam.handle, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading %s's memberships: %v", sam.handle, err)
	}

	if len(names) != 2 {
		t.Fatalf("%s holds %d memberships, want 2", sam.handle, len(names))
	}
	if names[0] != home().name {
		t.Errorf("the older of %s's gardens is %s, want %s", sam.handle, names[0], home().name)
	}
}

// dumpRows reads every seeded table as JSON text, so two runs can be compared
// column by column without naming the columns here. Rows are ordered by that
// text rather than by id, because notification_preference has no id.
func dumpRows(t *testing.T, pool *pgxpool.Pool) map[string][]string {
	t.Helper()

	out := map[string][]string{}
	for _, table := range seededTables {
		name := pgx.Identifier{table}.Sanitize()
		rows, err := pool.Query(t.Context(), "SELECT to_jsonb(t)::text FROM "+name+" t ORDER BY 1")
		if err != nil {
			t.Fatalf("reading %s: %v", table, err)
		}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				rows.Close()
				t.Fatalf("scanning %s: %v", table, err)
			}
			out[table] = append(out[table], row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", table, err)
		}
	}
	return out
}

// A schedule's dueIn is how many days after the reference it next falls due,
// so each plant must land in the Today section that offset implies.
func TestSeed_TodayPlacesEachPlantInTheSectionItsDueInImplies(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)
	ctx := t.Context()
	queries := store.New(pool)

	g := home()
	schedules, err := queries.ListCareSchedules(ctx, g.id)
	if err != nil {
		t.Fatalf("listing %s's schedules: %v", g.name, err)
	}
	events, err := queries.ListLatestCareEvents(ctx, g.id)
	if err != nil {
		t.Fatalf("listing %s's latest events: %v", g.name, err)
	}

	day := engine.Today(engine.Resolve(schedules, events, ref))

	sections := []struct {
		name string
		rows []engine.Row
		want []string
	}{
		{"Overdue", day.Overdue, []string{"Big Fella"}},
		{"Due today", day.DueToday, []string{"Doris", "Gerald", "Nigel"}},
		{"Coming up", day.ComingUp, []string{"Trail Mix", "Spike"}},
	}
	for _, s := range sections {
		got := make([]string, 0, len(s.rows))
		for _, r := range s.rows {
			got = append(got, r.Plant.DisplayName())
		}
		if !slices.Equal(got, s.want) {
			t.Errorf("%s = %v, want %v", s.name, got, s.want)
		}
	}

	// Gerald's watering is five days off and his feed is due today.
	for _, r := range day.DueToday {
		if r.Plant.DisplayName() == "Gerald" && r.Care.CareType.Slug != "feed" {
			t.Errorf("Gerald's row is about %s, want feed", r.Care.CareType.Slug)
		}
	}
	// Ferngully and the pothos with no nickname are both due in 8 days, one
	// day past the end of Coming up, so Next holds both rows.
	next := make([]string, 0, len(day.Next))
	for _, r := range day.Next {
		next = append(next, r.Plant.DisplayName())
	}
	if !slices.Equal(next, []string{"Ferngully", "Golden pothos"}) || day.Next[0].Care.Days != 8 {
		t.Errorf("Next = %v in %d days, want [Ferngully Golden pothos] in 8 days", next, day.Next[0].Care.Days)
	}
}
