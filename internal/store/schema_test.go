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

// wantCapabilities is written out here rather than read from the migration,
// so granting a capability takes a deliberate edit in two places.
var wantCapabilities = map[string][]string{
	"owner": {
		"calendar_note.manage", "care.delete_any", "care.delete_own", "care.edit_any", "care.edit_own",
		"care.log", "care_type.manage", "garden.delete", "garden.edit", "member.invite",
		"member.manage", "photo.add", "photo.delete_any", "photo.delete_own",
		"photo.set_profile", "plant.archive", "plant.create", "plant.edit",
		"schedule.edit", "sitting.view", "token.manage",
	},
	"member": {
		"calendar_note.manage", "care.delete_own", "care.edit_own", "care.log", "photo.add",
		"photo.delete_own", "photo.set_profile", "plant.archive",
		"plant.create", "plant.edit", "schedule.edit", "sitting.view", "token.manage",
	},
	"sitter": {"care.delete_own", "care.edit_own", "care.log"},
}

// The test below only sees capabilities granted to some role. This one checks
// the full list, so a capability named only in Go and never inserted is caught.
func TestSchema_TheCapabilityTableHoldsExactlyTheExpectedNames(t *testing.T) {
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

func TestSchema_EachRoleHasItsExpectedCapabilities(t *testing.T) {
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

	insert := "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'member', 8)"
	if _, err := pool.Exec(ctx, insert, garden, user); err != nil {
		t.Fatalf("inserting the first membership: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, garden, user); err == nil {
		t.Error("a second membership for the same user and garden was accepted")
	}
}

// Expiry is enforced by the middleware at request time, so the schema must
// keep the row.
func TestSchema_AnExpiredMembershipIsStillARow(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	var id uuid.UUID
	err := pool.QueryRow(ctx,
		"INSERT INTO membership (garden_id, user_id, role, expires_at, digest_hour) VALUES ($1, $2, 'sitter', now() - interval '1 day', 8) RETURNING id",
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

// Care events point at the user row, so removing someone from a garden deletes
// the membership and keeps the user.
func TestSchema_RemovingAMemberDeletesTheMembershipAndKeepsTheUser(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	var id uuid.UUID
	err := pool.QueryRow(ctx,
		"INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'sitter', 8) RETURNING id",
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

func TestSchema_AHandleIsUniqueAcrossAllGardens(t *testing.T) {
	pool := migratedPool(t)
	seedGardenAndUser(t, pool)

	_, err := pool.Exec(t.Context(),
		"INSERT INTO app_user (display_name, timezone, handle) VALUES ('Emma', 'Europe/London', 'emma')")
	if err == nil {
		t.Error("a second user with the handle emma was accepted")
	}
}

// role is a table so that membership.role and role_capability.role cannot name
// a role that does not exist.
func TestSchema_ARoleMustExistInTheRoleTable(t *testing.T) {
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	_, err := pool.Exec(t.Context(),
		"INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'administrator', 8)",
		garden, user)
	if err == nil {
		t.Error("a membership naming a role that does not exist was accepted")
	}
}

// Primary key indexes only stay append-mostly if the default is a v7 UUID, and
// nothing else in the app would notice if it silently became v4.
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
	// Allow a window either side of now(), because now() is the transaction's
	// start time and the id is generated a few hundred microseconds later.
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
// every run. Both are well-formed v7 UUIDs.
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

// seedPlantWithPhoto inserts a plant in the garden and one photo of it, and
// returns both ids.
func seedPlantWithPhoto(t *testing.T, conn DBTX, gardenID, userID uuid.UUID) (plantID, photoID uuid.UUID) {
	t.Helper()

	ctx := t.Context()
	err := conn.QueryRow(ctx, "INSERT INTO plant (garden_id, nickname) VALUES ($1, 'Big Fella') RETURNING id", gardenID).Scan(&plantID)
	if err != nil {
		t.Fatalf("inserting the plant: %v", err)
	}
	err = conn.QueryRow(ctx,
		`INSERT INTO photo (garden_id, plant_id, uploaded_by, kind, path, width, height, bytes, square_bytes)
		 VALUES ($1, $2, $3, 'image/jpeg', 'a.jpg', 3, 2, 100, 40) RETURNING id`,
		gardenID, plantID, userID).Scan(&photoID)
	if err != nil {
		t.Fatalf("inserting the photo: %v", err)
	}
	return plantID, photoID
}

func TestSchema_DeletingAPlantsPictureClearsThePictureAndKeepsThePlant(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	plant, photo := seedPlantWithPhoto(t, pool, garden, user)
	if _, err := pool.Exec(ctx, "UPDATE plant SET profile_photo_id = $1 WHERE id = $2", photo, plant); err != nil {
		t.Fatalf("setting the picture: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM photo WHERE id = $1", photo); err != nil {
		t.Fatalf("deleting the photo: %v", err)
	}

	var gardenAfter *uuid.UUID
	var picture *uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT garden_id, profile_photo_id FROM plant WHERE id = $1", plant).Scan(&gardenAfter, &picture); err != nil {
		t.Fatalf("reading the plant: %v", err)
	}
	if picture != nil {
		t.Errorf("profile_photo_id = %s, want NULL", *picture)
	}
	if gardenAfter == nil || *gardenAfter != garden {
		t.Errorf("garden_id = %v, want %s: the delete nulled more than the picture", gardenAfter, garden)
	}
}

func TestSchema_AnotherPlantsPhotoIsRefusedAsAPlantsPicture(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	plant, _ := seedPlantWithPhoto(t, pool, garden, user)
	_, othersPhoto := seedPlantWithPhoto(t, pool, garden, user)

	_, err := pool.Exec(ctx, "UPDATE plant SET profile_photo_id = $1 WHERE id = $2", othersPhoto, plant)

	if err == nil {
		t.Error("another plant's photo was accepted as this plant's picture")
	}
}
