package store

import (
	"maps"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ismailshak/sprig/internal/pgtest"
)

// wantCapabilities is written out rather than read from the migration, so
// granting one takes two edits.
var wantCapabilities = map[string][]string{
	"owner": {
		"care.delete_any", "care.delete_own", "care.edit_any", "care.edit_own",
		"care.log", "care_type.manage", "garden.edit", "member.invite",
		"member.manage", "photo.add", "photo.delete_any", "photo.delete_own",
		"photo.set_profile", "plant.archive", "plant.create", "plant.edit",
		"schedule.edit", "token.manage",
	},
	"member": {
		"care.delete_own", "care.edit_own", "care.log", "photo.add",
		"photo.delete_own", "photo.set_profile", "plant.archive",
		"plant.create", "plant.edit", "schedule.edit", "token.manage",
	},
	"sitter": {"care.delete_own", "care.edit_own", "care.log"},
}

// A capability granted to nobody is invisible to the test below, and a name
// only Go knows is the thing the whole table exists to prevent.
func TestSchema_TheCapabilityNamesAreTheOnesWrittenDown(t *testing.T) {
	pool := migratedPool(t)

	rows, err := pool.Query(t.Context(), "SELECT name FROM capability ORDER BY name")
	if err != nil {
		t.Fatalf("reading capability: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading capability: %v", err)
	}

	want := slices.Sorted(slices.Values(wantCapabilities["owner"]))
	if !slices.Equal(got, want) {
		t.Errorf("capability holds\n\t%v\nwant\n\t%v", got, want)
	}
}

func TestSchema_RolesHoldTheCapabilitiesTheyAreDescribedWith(t *testing.T) {
	pool := migratedPool(t)

	rows, err := pool.Query(t.Context(), "SELECT role, capability FROM role_capability ORDER BY role, capability")
	if err != nil {
		t.Fatalf("reading role_capability: %v", err)
	}
	defer rows.Close()

	got := map[string][]string{}
	for rows.Next() {
		var role, capability string
		if err := rows.Scan(&role, &capability); err != nil {
			t.Fatalf("scanning a grant: %v", err)
		}
		got[role] = append(got[role], capability)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading role_capability: %v", err)
	}

	if roles := slices.Sorted(maps.Keys(got)); !slices.Equal(roles, []string{"member", "owner", "sitter"}) {
		t.Fatalf("roles holding capabilities = %v, want the three", roles)
	}
	for role, want := range wantCapabilities {
		if !slices.Equal(got[role], want) {
			t.Errorf("%s holds\n\t%v\nwant\n\t%v", role, got[role], want)
		}
	}
}

func TestSchema_OneMembershipPerUserAndGarden(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	insert := "INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'member')"
	if _, err := pool.Exec(ctx, insert, garden, user); err != nil {
		t.Fatalf("inserting the first membership: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, garden, user); err == nil {
		t.Error("a second membership for the same user and garden was accepted")
	}
}

// The middleware refuses it at request time, so nothing in the schema may.
func TestSchema_AnExpiredMembershipIsStillARow(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	var id uuid.UUID
	err := pool.QueryRow(ctx,
		"INSERT INTO membership (garden_id, user_id, role, expires_at) VALUES ($1, $2, 'sitter', now() - interval '1 day') RETURNING id",
		garden, user).Scan(&id)
	if err != nil {
		t.Fatalf("inserting an expired membership: %v", err)
	}

	var expired bool
	err = pool.QueryRow(ctx, "SELECT expires_at < now() FROM membership WHERE id = $1", id).Scan(&expired)
	if err != nil {
		t.Fatalf("reading the membership back: %v", err)
	}
	if !expired {
		t.Error("the expired membership did not read back as expired")
	}
}

// The care events they recorded point at the user row.
func TestSchema_AMembershipIsDeletedAndAUserIsNot(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	var id uuid.UUID
	err := pool.QueryRow(ctx,
		"INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'sitter') RETURNING id",
		garden, user).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the membership: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM app_user WHERE id = $1", user); err == nil {
		t.Error("deleting a user who holds a membership was accepted")
	}

	if _, err := pool.Exec(ctx, "DELETE FROM membership WHERE id = $1", id); err != nil {
		t.Fatalf("deleting the membership: %v", err)
	}
	var users int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM app_user WHERE id = $1", user).Scan(&users); err != nil {
		t.Fatalf("counting the user: %v", err)
	}
	if users != 1 {
		t.Errorf("the user row survived deleting the membership: count = %d, want 1", users)
	}
}

func TestSchema_AHandleIsUniqueAcrossTheInstall(t *testing.T) {
	pool := migratedPool(t)
	seedGardenAndUser(t, pool)

	_, err := pool.Exec(t.Context(),
		"INSERT INTO app_user (display_name, timezone, handle) VALUES ('Emma', 'Europe/London', 'emma')")
	if err == nil {
		t.Error("a second user with the handle emma was accepted")
	}
}

func TestSchema_TheDigestHourDefaultsToEight(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	var hour int
	err := pool.QueryRow(ctx,
		"INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'member') RETURNING digest_hour",
		garden, user).Scan(&hour)
	if err != nil {
		t.Fatalf("inserting the membership: %v", err)
	}
	if hour != 8 {
		t.Errorf("digest_hour defaulted to %d, want 8", hour)
	}
}

// role is a table so the two columns that name one cannot disagree.
func TestSchema_ARoleIsOneTheRoleTableNames(t *testing.T) {
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	_, err := pool.Exec(t.Context(),
		"INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'administrator')",
		garden, user)
	if err == nil {
		t.Error("a membership naming a role that does not exist was accepted")
	}
}

// The primary key index only stays append-mostly if the default is v7, and
// nothing else in the app would notice if it quietly became v4.
func TestSchema_AnIdentifierDefaultsToATimeOrderedUUID(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)

	var version int
	var minted, now time.Time
	err := pool.QueryRow(ctx, `
		INSERT INTO garden (name) VALUES ('Rosewood')
		RETURNING uuid_extract_version(id), uuid_extract_timestamp(id), now()`).
		Scan(&version, &minted, &now)
	if err != nil {
		t.Fatalf("inserting a garden without an id: %v", err)
	}
	if version != 7 {
		t.Errorf("the default minted a v%d uuid, want v7", version)
	}
	// Either side of now(), because now() is the transaction's start time and
	// the identifier is minted a few hundred microseconds after it.
	if d := minted.Sub(now).Abs(); d > time.Minute {
		t.Errorf("the identifier's timestamp is %v from now, want the instant it was inserted", d)
	}
}

// migratedPool is an empty database with the app's own migrations applied.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return openPool(t, pgtest.Fresh(t, migrateSchema))
}

// The ids are fixed and obviously synthetic, so a failure names the same row
// every run. Both are well-formed v7 uuids.
var (
	testGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	testUserID   = uuid.MustParse("00000000-0000-7000-8000-000000000002")
)

func seedGardenAndUser(t *testing.T, conn DBTX) (gardenID, userID uuid.UUID) {
	t.Helper()

	ctx := t.Context()
	if _, err := conn.Exec(ctx, "INSERT INTO garden (id, name) VALUES ($1, 'Rosewood')", testGardenID); err != nil {
		t.Fatalf("inserting the garden: %v", err)
	}
	_, err := conn.Exec(ctx,
		"INSERT INTO app_user (id, display_name, timezone, handle) VALUES ($1, 'Emma', 'Europe/London', 'emma')", testUserID)
	if err != nil {
		t.Fatalf("inserting the user: %v", err)
	}
	return testGardenID, testUserID
}
