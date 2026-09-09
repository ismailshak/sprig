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

// sharedTx returns a transaction on a database with the app's migrations
// applied. Two tests inserting testGardenID at once would block on the unique
// index, so none of them runs in parallel.
func sharedTx(t *testing.T) pgx.Tx {
	t.Helper()
	return pgtest.Tx(t, migrateSchema)
}

// seedTwoGardens inserts Rosewood and a second garden, Fairview. With only
// one garden in the database a query missing its garden WHERE clause would
// still return the right rows, so every assertion below reads Rosewood and
// checks that nothing from Fairview is included.
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
	// Aloe has no location and no nickname, so it exercises the NULLS LAST and
	// the last coalesce term in the ordering.
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
	// Fern is live but the mist care type is archived. ListCareSchedules must
	// drop this schedule.
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

func TestGetPlant_ReturnsThePlantWithTheGivenID(t *testing.T) {
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

// The overrides in sqlc.yaml decide the generated Go types, and no other test
// checks them. A nullable column becomes a pointer, so NULL and the empty
// string are distinguishable.
func TestQueries_GeneratedColumnTypesMatchTheSqlcOverrides(t *testing.T) {
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

// Another garden's plant returns no row, so a handler has nothing to return a
// 403 for.
func TestGetPlant_AnotherGardensPlantReturnsNoRow(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	_, err := queries.GetPlant(t.Context(), testGardenID, otherPlantID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("reading Fairview's plant from Rosewood returned %v, want pgx.ErrNoRows", err)
	}
}

func TestListPlants_OmitsArchivedPlantsAndOrdersByRoomThenName(t *testing.T) {
	queries, _ := seedTwoGardens(t)

	plants, err := queries.ListPlants(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's plants: %v", err)
	}

	got := make([]uuid.UUID, len(plants))
	for i, p := range plants {
		got[i] = p.ID
	}
	// Bathroom sorts before Living room. The plant with no room sorts last.
	want := []uuid.UUID{fernID, montyID, sprigID}
	if !slices.Equal(got, want) {
		t.Errorf("listed\n\t%v\nwant\n\t%v", got, want)
	}
}

func TestListRooms_ListsEachRoomOnceAndOmitsArchivedPlantsRooms(t *testing.T) {
	queries, tx := seedTwoGardens(t)

	// A second plant in the Bathroom, so the test can show a room two plants are
	// in is listed once.
	if _, err := tx.Exec(t.Context(), "INSERT INTO plant (garden_id, nickname, location) VALUES ($1, 'Ivy', 'Bathroom')", testGardenID); err != nil {
		t.Fatalf("inserting a second plant in the Bathroom: %v", err)
	}

	rooms, err := queries.ListRooms(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's rooms: %v", err)
	}

	// Bedroom is left out because the only plant in it is archived. Aloe has no
	// location. Porch is Fairview's room, not Rosewood's.
	want := []string{"Bathroom", "Living room"}
	if !slices.Equal(rooms, want) {
		t.Errorf("listed\n\t%v\nwant\n\t%v", rooms, want)
	}
}

// The order matches the way the Plants list groups rooms.
func TestListRooms_OrdersARoomSpelledLowercaseByItsLetterNotItsCase(t *testing.T) {
	queries, tx := seedTwoGardens(t)

	if _, err := tx.Exec(t.Context(), "INSERT INTO plant (garden_id, nickname, location) VALUES ($1, 'Kev', 'attic')", testGardenID); err != nil {
		t.Fatalf("inserting a plant in the attic: %v", err)
	}

	rooms, err := queries.ListRooms(t.Context(), testGardenID)
	if err != nil {
		t.Fatalf("listing Rosewood's rooms: %v", err)
	}

	want := []string{"attic", "Bathroom", "Living room"}
	if !slices.Equal(rooms, want) {
		t.Errorf("listed\n\t%v\nwant\n\t%v", rooms, want)
	}
}

func TestListCareTypes_OmitsArchivedTypes(t *testing.T) {
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

func TestListCareSchedules_ReturnsEachScheduleWithItsPlantAndCareType(t *testing.T) {
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

	// Fern's misting is missing because its care type is archived. Departed's
	// watering is missing because the plant is archived. Fairview's is missing
	// because it is another garden.
	want := []line{{fernID, "water"}, {montyID, "water"}, {montyID, "feed"}}
	if !slices.Equal(got, want) {
		t.Errorf("listed\n\t%v\nwant\n\t%v", got, want)
	}
}

func TestListLatestCareEvents_ReturnsOneRowPerPlantAndCareType(t *testing.T) {
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
	// Fern's only event is a skip. A skip still counts as the latest event.
	if fern := latest[fernID]; fern.Done {
		t.Error("Fern's latest event reads as done, and the row seeded was a skip")
	}
	if _, leaked := latest[otherPlantID]; leaked {
		t.Error("Fairview's watering came back in Rosewood's events")
	}
}

// The events query has no joins, so a plant or care type archived after its
// last event still has a row here. ListCareSchedules excludes both, and the
// caller pairing the two results drops events it cannot match.
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

// sqlc returns a nil slice for an empty :many result, so callers check the
// length rather than comparing to nil.
func TestListPlants_AnEmptyGardenReturnsNoRowsAndNoError(t *testing.T) {
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

// insertPhoto inserts a photo of the plant and returns its id. A nil
// squareBytes stores the photo with no square variant.
func insertPhoto(t *testing.T, tx pgx.Tx, plantID uuid.UUID, squareBytes *int64) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := tx.QueryRow(t.Context(),
		`INSERT INTO photo (garden_id, plant_id, uploaded_by, kind, path, width, height, bytes, square_bytes)
		 VALUES ($1, $2, $3, 'image/jpeg', $4, 2048, 1536, 400, $5) RETURNING id`,
		testGardenID, plantID, testUserID, plantID.String()+".jpg", squareBytes).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the photo: %v", err)
	}
	return id
}

func TestSetProfilePhoto_APhotoWithNoSquareVariantIsRefusedAndThePictureIsUnchanged(t *testing.T) {
	ctx := t.Context()
	queries, tx := seedTwoGardens(t)
	squareBytes := int64(40)
	picture := insertPhoto(t, tx, montyID, &squareBytes)
	if _, err := queries.SetProfilePhoto(ctx, SetProfilePhotoParams{PhotoID: &picture, GardenID: testGardenID, PlantID: montyID}); err != nil {
		t.Fatalf("setting the picture: %v", err)
	}
	squareless := insertPhoto(t, tx, montyID, nil)

	_, err := queries.SetProfilePhoto(ctx, SetProfilePhotoParams{PhotoID: &squareless, GardenID: testGardenID, PlantID: montyID})

	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("the update returned %v, want %v", err, pgx.ErrNoRows)
	}
	monty, err := queries.GetPlant(ctx, testGardenID, montyID)
	if err != nil {
		t.Fatalf("reading Monty: %v", err)
	}
	if monty.ProfilePhotoID == nil || *monty.ProfilePhotoID != picture {
		t.Errorf("Monty's picture is %v, want the photo with a square, %s", monty.ProfilePhotoID, picture)
	}
}

// Every query on a table with a garden_id column takes the garden as a
// parameter, so there is no unscoped read for a handler to call. A query that
// reads more than one scoped table must scope each of them, because scoping
// only the driving table leaves a join free to cross gardens.
//
// Four tables are exempt when a query names one of them alone, because they
// are how a request finds out which garden it is on, so the lookup cannot
// take the garden as input. A query on session, invite or api_token alone
// binds @token_hash, and one on membership alone binds @user_id, so every row
// it touches belongs to the caller. A query joining any of them to another
// scoped table binds @garden_id like any other.
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
		// Closing an account and the daily sweep both delete sessions across
		// every garden. Neither runs from a request.
		if (query.name == "DeleteUserSessions" || query.name == "DeleteExpiredSessions") && slices.Equal(touched, []string{"session"}) {
			continue
		}
		if slices.Equal(touched, []string{"session"}) {
			if !strings.Contains(query.sql, "@token_hash") {
				t.Errorf("%s reads session and takes no @token_hash, so it can return another garden's rows", query.name)
			}
			continue
		}
		// The page an invite link opens finds the invite by the hash of the
		// token in the URL, before any garden is known. Every other read of
		// invite binds @garden_id.
		if slices.Equal(touched, []string{"invite"}) && strings.Contains(query.sql, "@token_hash") {
			continue
		}
		// Closing an account and the daily sweep both delete invites across
		// every garden. Neither runs from a request.
		if (query.name == "DeleteUserReenrolmentInvites" || query.name == "DeleteRedeemedAndExpiredInvites") && slices.Equal(touched, []string{"invite"}) {
			continue
		}
		// A bearer token is found by its hash before any garden is known, so
		// the lookup and the last_used_at write beside it bind @token_hash.
		// Every other query on api_token binds @garden_id.
		if slices.Equal(touched, []string{"api_token"}) && strings.Contains(query.sql, "@token_hash") {
			continue
		}
		if slices.Equal(touched, []string{"membership"}) && strings.Contains(query.sql, "@user_id") {
			continue
		}
		// The digest job runs across every garden rather than for one request.
		// Each row it returns names a garden id. Every read the send then
		// makes binds that id.
		if query.name == "ListDigestMembers" && slices.Equal(touched, []string{"membership"}) {
			continue
		}
		// The sweep command compares every photo row with the files on disk.
		// It runs from the command line and never from a request.
		if query.name == "ListPhotoFiles" && slices.Equal(touched, []string{"photo"}) {
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
	queryName = regexp.MustCompile(`(?m)^-- name: (\w+) :\w+$`)
	// lock table is in the alternation so a query that does nothing but take a
	// lock still names a table. A query naming none fails the scope test.
	queryTable = regexp.MustCompile(`(?i)\b(?:from|join|into|update|lock table)\s+([a-z_][a-z0-9_]*)`)
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

// A query's text runs to the next "-- name:" line, so it includes the comments
// written above the following query. Dropping comment lines leaves only SQL,
// so a table named in a comment is not counted as one the query reads.
func stripSQLComments(body string) string {
	var kept []string
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
