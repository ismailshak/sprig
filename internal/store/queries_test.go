package store

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/pgtest"
)

var (
	otherGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000003")
	otherUserID   = uuid.MustParse("00000000-0000-7000-8000-000000000004")

	montyID         = uuid.MustParse("00000000-0000-7000-8000-000000000011")
	fernID          = uuid.MustParse("00000000-0000-7000-8000-000000000012")
	sprigID         = uuid.MustParse("00000000-0000-7000-8000-000000000013")
	departedID      = uuid.MustParse("00000000-0000-7000-8000-000000000014")
	otherPlantID    = uuid.MustParse("00000000-0000-7000-8000-000000000015")
	waterTypeID     = uuid.MustParse("00000000-0000-7000-8000-000000000021")
	feedTypeID      = uuid.MustParse("00000000-0000-7000-8000-000000000022")
	mistTypeID      = uuid.MustParse("00000000-0000-7000-8000-000000000023")
	otherWaterID    = uuid.MustParse("00000000-0000-7000-8000-000000000024")
	lastWatering    = time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	earlierWatering = time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)
)

// sharedTx is pgtest.Tx over the app's migrations. Two tests inserting
// testGardenID at once would serialise on the unique index, so none of them
// runs in parallel.
func sharedTx(t *testing.T) pgx.Tx {
	t.Helper()
	return pgtest.Tx(t, migrateSchema)
}

// seedTwoGardens fills a transaction with Rosewood and one other garden. With
// a single garden in the database a query that lost its WHERE returns the
// right answer anyway, so every assertion below reads Rosewood and expects
// nothing of Fairview in the answer.
func seedTwoGardens(t *testing.T) (*Queries, pgx.Tx) {
	t.Helper()

	ctx := t.Context()
	tx := sharedTx(t)
	seedGardenAndUser(t, tx)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, sql)
		}
	}

	exec("INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", otherGardenID)
	exec("INSERT INTO app_user (id, display_name, timezone, handle) VALUES ($1, 'Noor', 'Pacific/Auckland', 'noor')", otherUserID)

	exec(`INSERT INTO plant (id, garden_id, nickname, common_name, location) VALUES
		($1, $2, 'Monty',  'Swiss cheese plant', 'Living room'),
		($3, $2, 'Fern',   'Boston fern',        'Bathroom')`,
		montyID, testGardenID, fernID)
	// Aloe has no location and no nickname, so it lands on the last term of
	// both the NULLS LAST and the coalesce.
	exec("INSERT INTO plant (id, garden_id, common_name) VALUES ($1, $2, 'Aloe')", sprigID, testGardenID)
	exec(`INSERT INTO plant (id, garden_id, nickname, location, archived_at)
		VALUES ($1, $2, 'Departed', 'Bedroom', now())`, departedID, testGardenID)
	exec("INSERT INTO plant (id, garden_id, nickname, location) VALUES ($1, $2, 'Kauri', 'Porch')", otherPlantID, otherGardenID)

	exec(`INSERT INTO care_type (id, garden_id, name, slug, created_at) VALUES
		($1, $2, 'Water', 'water', '2026-01-01T00:00:00Z'),
		($3, $2, 'Feed',  'feed',  '2026-01-02T00:00:00Z')`,
		waterTypeID, testGardenID, feedTypeID)
	exec(`INSERT INTO care_type (id, garden_id, name, slug, created_at, archived_at)
		VALUES ($1, $2, 'Mist', 'mist', '2026-01-03T00:00:00Z', now())`, mistTypeID, testGardenID)
	exec("INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", otherWaterID, otherGardenID)

	exec(`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit) VALUES
		($1, $2, $3, 7,  'day'),
		($1, $2, $4, 4,  'week'),
		($1, $5, $3, 3,  'day'),
		($1, $6, $3, 10, 'day')`,
		testGardenID, montyID, waterTypeID, feedTypeID, fernID, departedID)
	// Fern is live and the mist type is archived, which is the pair
	// ListCareSchedules has to drop.
	exec(`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
		VALUES ($1, $2, $3, 2, 'day')`, testGardenID, fernID, mistTypeID)
	exec(`INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
		VALUES ($1, $2, $3, 14, 'day')`, otherGardenID, otherPlantID, otherWaterID)

	exec(`INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done) VALUES
		($1, $2, $3, $4, $5, true),
		($1, $2, $3, $4, $6, true),
		($1, $7, $3, $4, $6, false)`,
		testGardenID, montyID, waterTypeID, testUserID, lastWatering, earlierWatering, fernID)
	exec(`INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done)
		VALUES ($1, $2, $3, $4, $5, true)`,
		otherGardenID, otherPlantID, otherWaterID, otherUserID, lastWatering)

	return New(tx), tx
}

func TestGetPlant_ReadsThePlantAsked(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	plant, err := queries.GetPlant(t.Context(), testGardenID, montyID)
	if err != nil {
		t.Fatalf("reading Monty: %v", err)
	}
	if plant.ID != montyID {
		t.Errorf("read plant %v, want %v", plant.ID, montyID)
	}
	if plant.Nickname == nil {
		t.Fatal("nickname is nil, and Monty was seeded with one")
	}
	if *plant.Nickname != "Monty" {
		t.Errorf("nickname = %q, want Monty", *plant.Nickname)
	}
}

// The overrides in sqlc.yaml decide the generated types, and no other test
// here asserts them. A nullable column arrives as a pointer, so a column
// holding nothing and one holding the empty string stay apart.
func TestQueries_TheColumnTypesAreTheOnesTheOverridesAskFor(t *testing.T) {
	ctx := t.Context()
	queries, _ := seedTwoGardens(t)

	monty, err := queries.GetPlant(ctx, testGardenID, montyID)
	if err != nil {
		t.Fatalf("reading Monty: %v", err)
	}
	if monty.ID != montyID {
		t.Errorf("the identifier came back as %v, want the %v it was written with", monty.ID, montyID)
	}
	if monty.BotanicalName != nil {
		t.Errorf("botanical name = %q, want nil for a column nobody filled in", *monty.BotanicalName)
	}
	if monty.ArchivedAt != nil {
		t.Errorf("archived at = %v, want nil for a living plant", *monty.ArchivedAt)
	}
	if monty.CreatedAt.IsZero() {
		t.Error("created at is the zero time, so the default did not reach Go")
	}

	departed, err := queries.GetPlant(ctx, testGardenID, departedID)
	if err != nil {
		t.Fatalf("reading Departed: %v", err)
	}
	if departed.ArchivedAt == nil {
		t.Error("archived at is nil on the plant that was archived")
	}
}

// Another garden's plant is no row at all, so a handler has nothing it could
// answer with a 403.
func TestGetPlant_AnotherGardensPlantIsNoRow(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	_, err := queries.GetPlant(t.Context(), testGardenID, otherPlantID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("reading Fairview's plant from Rosewood returned %v, want pgx.ErrNoRows", err)
	}
}

func TestListPlants_LeavesOutArchivedAndOrdersLikeTheRoster(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	plants, err := queries.ListPlants(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's plants: %v", err)
	}

	got := make([]uuid.UUID, len(plants))
	for i, p := range plants {
		got[i] = p.ID
	}
	// Bathroom sorts before Living room, and the plant with no room comes
	// after both.
	want := []uuid.UUID{fernID, montyID, sprigID}
	if !slices.Equal(got, want) {
		t.Errorf("listed\n\t%v\nwant\n\t%v", got, want)
	}
}

func TestListCareTypes_LeavesOutTheArchivedOne(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	types, err := queries.ListCareTypes(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's care types: %v", err)
	}

	got := make([]string, len(types))
	for i, ct := range types {
		got[i] = ct.Slug
	}
	if want := []string{"water", "feed"}; !slices.Equal(got, want) {
		t.Errorf("listed %v, want %v in the order they were created", got, want)
	}
}

func TestListCareSchedules_CarriesThePlantAndTheCareTypeItBelongsTo(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	rows, err := queries.ListCareSchedules(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's schedules: %v", err)
	}

	type line struct {
		plant    uuid.UUID
		careType string
	}
	got := make([]line, len(rows))
	for i, row := range rows {
		if row.CareSchedule.PlantID != row.Plant.ID || row.CareSchedule.CareTypeID != row.CareType.ID {
			t.Fatalf("row %d joined schedule %v to plant %v and type %v",
				i, row.CareSchedule.ID, row.Plant.ID, row.CareType.ID)
		}
		got[i] = line{row.Plant.ID, row.CareType.Slug}
	}

	// Fern's misting is absent because the type is archived, Departed's watering
	// because the plant is, and Fairview's because it is another garden's.
	want := []line{{fernID, "water"}, {montyID, "water"}, {montyID, "feed"}}
	if !slices.Equal(got, want) {
		t.Errorf("listed\n\t%v\nwant\n\t%v", got, want)
	}
}

func TestListLatestCareEvents_IsOneRowPerPlantAndCareType(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	events, err := queries.ListLatestCareEvents(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's latest events: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("read %d events, want one for Monty's watering and one for Fern's", len(events))
	}
	latest := map[uuid.UUID]CareEvent{}
	for _, e := range events {
		latest[e.PlantID] = e
	}
	monty, ok := latest[montyID]
	if !ok {
		t.Fatal("Monty has no latest watering")
	}
	if !monty.PerformedAt.Equal(lastWatering) {
		t.Errorf("Monty was last watered at %v, want the newer of the two events at %v",
			monty.PerformedAt, lastWatering)
	}
	// Fern's only event is a skip, which is still its latest.
	if fern := latest[fernID]; fern.Done {
		t.Error("Fern's latest event reads as done, and the row seeded was a skip")
	}
	if _, leaked := latest[otherPlantID]; leaked {
		t.Error("Fairview's watering came back in Rosewood's events")
	}
}

// The events query joins nothing, so a plant or care type archived after its
// last event keeps its row here. ListCareSchedules excludes both, and the
// caller pairing the two drops what it cannot match.
func TestListLatestCareEvents_KeepsTheEventsOfAnArchivedPlantAndCareType(t *testing.T) {
	ctx := t.Context()
	queries, tx := seedTwoGardens(t)

	_, err := tx.Exec(ctx, `
		INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done) VALUES
			($1, $2, $3, $4, $5, true),
			($1, $6, $7, $4, $5, true)`,
		testGardenID, departedID, waterTypeID, testUserID, lastWatering, fernID, mistTypeID)
	if err != nil {
		t.Fatalf("seeding events on the archived plant and the archived care type: %v", err)
	}

	events, err := queries.ListLatestCareEvents(ctx, testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's latest events: %v", err)
	}

	type pair struct{ plant, careType uuid.UUID }
	got := map[pair]bool{}
	for _, e := range events {
		got[pair{e.PlantID, e.CareTypeID}] = true
	}
	if !got[pair{departedID, waterTypeID}] {
		t.Error("the archived plant's last watering is missing")
	}
	if !got[pair{fernID, mistTypeID}] {
		t.Error("the archived care type's last event is missing")
	}
}

// sqlc returns a nil slice for an empty :many, so a caller asks for the length
// rather than comparing the slice to nil.
func TestListPlants_AnEmptyGardenIsNoRowsAndNoError(t *testing.T) {
	ctx := t.Context()
	queries, tx := seedTwoGardens(t)

	var empty uuid.UUID
	if err := tx.QueryRow(ctx, "INSERT INTO garden (name) VALUES ('Hollow') RETURNING id").Scan(&empty); err != nil {
		t.Fatalf("inserting the empty garden: %v", err)
	}

	plants, err := queries.ListPlants(ctx, empty)
	if err != nil {
		t.Fatalf("listing a garden with no plants: %v", err)
	}
	if len(plants) != 0 {
		t.Errorf("listed %d plants in a garden that has none", len(plants))
	}
}

// A query touching a table that hangs off a garden takes the garden as a
// parameter, so there is no unscoped read for a handler to call. A query that
// reads more than one scoped table scopes each of them, because binding the
// garden on the driving table alone leaves a join free to cross.
func TestQueries_EveryQueryOnAGardenScopedTableBindsTheGarden(t *testing.T) {
	tx := sharedTx(t)

	rows, err := tx.Query(t.Context(), `
		SELECT table_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'garden_id'`)
	if err != nil {
		t.Fatalf("reading the garden-scoped tables: %v", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading the garden-scoped tables: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no table in the schema has a garden_id, so this test checks nothing")
	}
	scoped := map[string]bool{}
	for _, name := range names {
		scoped[name] = true
	}

	for _, query := range readQueries(t) {
		if len(query.tables) == 0 {
			t.Errorf("%s names no table, so this test cannot tell what it reads", query.name)
			continue
		}

		var touched []string
		for _, table := range query.tables {
			if scoped[table] && !slices.Contains(touched, table) {
				touched = append(touched, table)
			}
		}
		if len(touched) == 0 {
			continue
		}
		if !strings.Contains(query.sql, "@garden_id") {
			t.Errorf("%s reads %s and takes no @garden_id, so it can return another garden's rows",
				query.name, strings.Join(touched, ", "))
			continue
		}
		if len(touched) == 1 {
			continue
		}
		for _, table := range touched {
			if !strings.Contains(query.sql, table+".garden_id") {
				t.Errorf("%s reads %s and has no predicate on %s.garden_id, so the join can cross gardens",
					query.name, table, table)
			}
		}
	}
}

type namedQuery struct {
	name   string
	sql    string
	tables []string
}

var (
	queryName  = regexp.MustCompile(`(?m)^-- name: (\w+) :\w+$`)
	queryTable = regexp.MustCompile(`(?i)\b(?:from|join|into|update)\s+([a-z_][a-z0-9_]*)`)
)

func readQueries(t *testing.T) []namedQuery {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	if err != nil {
		t.Fatalf("looking for the query files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("db/queries holds no .sql files")
	}

	var queries []namedQuery
	for _, file := range files {
		//nolint:gosec // the path came from a glob of the repository's own db/queries
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		text := string(content)
		starts := queryName.FindAllStringSubmatchIndex(text, -1)
		for i, start := range starts {
			end := len(text)
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}
			body := stripSQLComments(text[start[0]:end])
			var tables []string
			for _, match := range queryTable.FindAllStringSubmatch(body, -1) {
				tables = append(tables, strings.ToLower(match[1]))
			}
			queries = append(queries, namedQuery{
				name:   text[start[2]:start[3]],
				sql:    body,
				tables: tables,
			})
		}
	}
	return queries
}

// A query's text runs to the next -- name: line, which sweeps up the comments
// written above the query after it. Dropping the comment lines leaves the SQL,
// so a table named in prose is not read as one the query touches.
func stripSQLComments(body string) string {
	var kept []string
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
