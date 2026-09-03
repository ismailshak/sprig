package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema_ACareTypeSlugIsUniqueWithinItsGarden(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, _ := seedGardenAndUser(t, pool)

	var other uuid.UUID
	if err := pool.QueryRow(ctx, "INSERT INTO garden (name) VALUES ('Elsewhere') RETURNING id").Scan(&other); err != nil {
		t.Fatalf("inserting the second garden: %v", err)
	}

	insert := "INSERT INTO care_type (garden_id, name, slug) VALUES ($1, 'Water', 'water')"
	if _, err := pool.Exec(ctx, insert, garden); err != nil {
		t.Fatalf("inserting the care type: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, other); err != nil {
		t.Errorf("the second garden could not have a care type called water: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, garden); err == nil {
		t.Error("a second care type called water in the same garden was accepted")
	}
}

// Every other combination has to be refused rather than merely never written.
func TestSchema_TheThreeScheduleShapesAreTheOnlyOnes(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, _ := seedGardenAndUser(t, pool)
	careType := seedCareType(t, pool, garden, "water", "Water")

	// count, unit, anchor date, anchor precision, season start, season end.
	accepted := map[string][6]any{
		"a cadence":               {10, "day", nil, nil, nil, nil},
		"a cadence with a season": {3, "week", nil, nil, 3, 9},
		"a season that wraps":     {3, "week", nil, nil, 11, 2},
		"an anchored repeat":      {1, "year", "2027-08-01", "day", nil, nil},
		"a month-precise anchor":  {1, "year", "2027-08-01", "month", nil, nil},
		"a one-off":               {nil, nil, "2028-03-01", "month", nil, nil},
	}
	rejected := map[string][6]any{
		"no rule at all":              {nil, nil, nil, nil, nil, nil},
		"a count with no unit":        {10, nil, nil, nil, nil, nil},
		"a unit with no count":        {nil, "day", nil, nil, nil, nil},
		"an anchor with no precision": {nil, nil, "2028-03-01", nil, nil, nil},
		"a precision with no anchor":  {10, "day", nil, "day", nil, nil},
		"half a season":               {10, "day", nil, nil, 3, nil},
		"a season on an anchor":       {1, "year", "2027-08-01", "day", 3, 9},
		"a season on a one-off":       {nil, nil, "2028-03-01", "month", 3, 9},
		"an interval of nothing":      {0, "day", nil, nil, nil, nil},
		"a unit nobody named":         {2, "fortnight", nil, nil, nil, nil},
		"a precision nobody named":    {1, "year", "2027-08-01", "hour", nil, nil},
	}

	insert := `
		INSERT INTO care_schedule
			(garden_id, plant_id, care_type_id,
			 interval_count, interval_unit, anchor_date, anchor_precision,
			 season_start_month, season_end_month)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	for what, s := range accepted {
		plant := seedPlant(t, pool, garden, what)
		_, err := pool.Exec(ctx, insert, garden, plant, careType, s[0], s[1], s[2], s[3], s[4], s[5])
		if err != nil {
			t.Errorf("%s was rejected: %v", what, err)
		}
	}
	for what, s := range rejected {
		plant := seedPlant(t, pool, garden, what)
		_, err := pool.Exec(ctx, insert, garden, plant, careType, s[0], s[1], s[2], s[3], s[4], s[5])
		if err == nil {
			t.Errorf("%s was accepted", what)
		}
	}
}

func TestSchema_OneSchedulePerPlantAndCareType(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, _ := seedGardenAndUser(t, pool)
	plant := seedPlant(t, pool, garden, "Doris")
	careType := seedCareType(t, pool, garden, "water", "Water")

	insert := `
		INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
		VALUES ($1, $2, $3, 21, 'day')`
	if _, err := pool.Exec(ctx, insert, garden, plant, careType); err != nil {
		t.Fatalf("inserting the first schedule: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, garden, plant, careType); err == nil {
		t.Error("a second watering schedule for the same plant was accepted")
	}
}

// Garden scoping is a store rule everywhere else, and here it is also a foreign
// key, so a row spanning two gardens cannot be written by anybody.
func TestSchema_ARowCannotSpanTwoGardens(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	plant := seedPlant(t, pool, garden, "Nigel")

	var other uuid.UUID
	if err := pool.QueryRow(ctx, "INSERT INTO garden (name) VALUES ('Elsewhere') RETURNING id").Scan(&other); err != nil {
		t.Fatalf("inserting the second garden: %v", err)
	}
	theirs := seedCareType(t, pool, other, "water", "Water")

	_, err := pool.Exec(ctx, `
		INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
		VALUES ($1, $2, $3, 4, 'day')`, garden, plant, theirs)
	if err == nil {
		t.Error("a schedule joining our plant to their care type was accepted")
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done)
		VALUES ($1, $2, $3, $4, now(), true)`, garden, plant, theirs, user)
	if err == nil {
		t.Error("an event on our plant of their care type was accepted")
	}
}

// Deleting one would take the meaning of every event recorded against it.
func TestSchema_ACareTypeWithEventsCanOnlyBeArchived(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	plant := seedPlant(t, pool, garden, "Trail Mix")
	unused := seedCareType(t, pool, garden, "mist", "Mist")
	used := seedCareType(t, pool, garden, "water", "Water")

	if _, err := pool.Exec(ctx, "DELETE FROM care_type WHERE id = $1", unused); err != nil {
		t.Errorf("deleting a care type nothing has been recorded against: %v", err)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done)
		VALUES ($1, $2, $3, $4, now(), true)`, garden, plant, used, user)
	if err != nil {
		t.Fatalf("inserting the event: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM care_type WHERE id = $1", used); err == nil {
		t.Error("deleting a care type with an event behind it was accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE care_type SET archived_at = now() WHERE id = $1", used); err != nil {
		t.Errorf("archiving it instead: %v", err)
	}
}

// The reminder counts from when the care happened, not from when it was typed in.
func TestSchema_AnEventRecordsWhenItHappenedAndWhenItWasEntered(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	plant := seedPlant(t, pool, garden, "Ferngully")
	careType := seedCareType(t, pool, garden, "water", "Water")

	var performed, recorded time.Time
	err := pool.QueryRow(ctx, `
		INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done, note)
		VALUES ($1, $2, $3, $4, now() - interval '14 hours', true, 'Soil was bone dry')
		RETURNING performed_at, recorded_at`, garden, plant, careType, user).Scan(&performed, &recorded)
	if err != nil {
		t.Fatalf("inserting the backdated event: %v", err)
	}
	if !performed.Before(recorded) {
		t.Errorf("performed_at %v is not before recorded_at %v", performed, recorded)
	}
	if d := recorded.Sub(performed); d < 13*time.Hour || d > 15*time.Hour {
		t.Errorf("the two timestamps are %v apart, want the fourteen hours inserted", d)
	}
}

// A skip is an event, and its override is what makes "ask again in 2 days" work.
func TestSchema_ASkipCarriesItsOwnInterval(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	plant := seedPlant(t, pool, garden, "Sprout")
	careType := seedCareType(t, pool, garden, "water", "Water")

	_, err := pool.Exec(ctx, `
		INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done, override_interval_days)
		VALUES ($1, $2, $3, $4, now(), false, 2)`, garden, plant, careType, user)
	if err != nil {
		t.Fatalf("inserting the skip: %v", err)
	}
}

func seedPlant(t *testing.T, pool *pgxpool.Pool, gardenID uuid.UUID, nickname string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		"INSERT INTO plant (garden_id, nickname) VALUES ($1, $2) RETURNING id", gardenID, nickname).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the plant %q: %v", nickname, err)
	}
	return id
}

func seedCareType(t *testing.T, pool *pgxpool.Pool, gardenID uuid.UUID, slug, name string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		"INSERT INTO care_type (garden_id, name, slug) VALUES ($1, $2, $3) RETURNING id", gardenID, name, slug).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the care type %q: %v", slug, err)
	}
	return id
}
