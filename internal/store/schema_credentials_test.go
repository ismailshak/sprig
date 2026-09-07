package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Removing someone from a garden must clear garden_id on their sessions at
// once, not on the next page they load. The session row stays, so the account
// can still sign out, accept an invite or set up a garden of its own.
func TestSchema_DeletingAMembershipTakesItsSessionsOffTheGarden(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	membership := seedMembership(t, pool, garden, user, "sitter")

	other := seedGarden(t, pool, "Elsewhere")
	seedMembership(t, pool, other, user, "member")

	seedSession(t, pool, garden, user, "here")
	seedSession(t, pool, other, user, "elsewhere")

	if _, err := pool.Exec(ctx, "DELETE FROM membership WHERE id = $1", membership); err != nil {
		t.Fatalf("deleting the membership: %v", err)
	}

	rows, err := pool.Query(ctx, "SELECT token_hash, garden_id FROM session WHERE user_id = $1 ORDER BY token_hash", user)
	if err != nil {
		t.Fatalf("reading the sessions back: %v", err)
	}
	type row struct {
		TokenHash string
		GardenID  *uuid.UUID
	}
	got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		t.Fatalf("reading the sessions back: %v", err)
	}
	if len(got) != 2 || got[0].TokenHash != "elsewhere" || got[0].GardenID == nil || *got[0].GardenID != other || got[1].TokenHash != "here" || got[1].GardenID != nil {
		t.Errorf("sessions = %+v, want elsewhere still on Elsewhere and here on no garden", got)
	}
}

func TestSchema_ASessionCannotNameAGardenItsUserIsNotIn(t *testing.T) {
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	seedMembership(t, pool, garden, user, "member")
	other := seedGarden(t, pool, "Elsewhere")

	_, err := pool.Exec(t.Context(),
		"INSERT INTO session (token_hash, user_id, garden_id) VALUES ('trespass', $1, $2)", user, other)
	if err == nil {
		t.Error("a session naming a garden its user holds no membership in was accepted")
	}
}

// The schema requires an expiry. The 90-day ceiling is a constant in Go.
func TestSchema_AnAPITokenCannotBeWrittenWithoutAnExpiry(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	insert := `INSERT INTO api_token (garden_id, name, token_hash, prefix, created_by, expires_at)
	           VALUES ($1, 'The display', 'hash', 'sprig_ab12', $2, $3)`
	if _, err := pool.Exec(ctx, insert, garden, user, nil); err == nil {
		t.Error("a token with no expiry was accepted")
	}
	if _, err := pool.Exec(ctx, insert, garden, user, "2026-12-01T00:00:00Z"); err != nil {
		t.Errorf("a token with an expiry was refused: %v", err)
	}
}

func TestSchema_OneNotificationPreferencePerMembershipAndKind(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	membership := seedMembership(t, pool, garden, user, "member")

	insert := "INSERT INTO notification_preference (membership_id, kind, enabled) VALUES ($1, $2, $3)"
	if _, err := pool.Exec(ctx, insert, membership, "digest", true); err != nil {
		t.Fatalf("inserting the digest preference: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, membership, "activity", false); err != nil {
		t.Fatalf("inserting the activity preference: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, membership, "digest", false); err == nil {
		t.Error("a second digest preference for the same membership was accepted")
	}
	if _, err := pool.Exec(ctx, insert, membership, "misting", true); err == nil {
		t.Error("a preference for a kind nobody named was accepted")
	}

	if _, err := pool.Exec(ctx, "DELETE FROM membership WHERE id = $1", membership); err != nil {
		t.Fatalf("deleting the membership: %v", err)
	}
	var left int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notification_preference").Scan(&left); err != nil {
		t.Fatalf("counting the preferences: %v", err)
	}
	if left != 0 {
		t.Errorf("%d preferences outlived the membership, want 0", left)
	}
}

// A second attempt on the same day has to collide with the row the first one
// wrote.
func TestSchema_ADigestIsClaimedOncePerMembershipAndLocalDate(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)
	membership := seedMembership(t, pool, garden, user, "member")

	insert := "INSERT INTO notification_send (membership_id, kind, local_date) VALUES ($1, 'digest', $2)"
	if _, err := pool.Exec(ctx, insert, membership, "2026-09-03"); err != nil {
		t.Fatalf("claiming today's digest: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, membership, "2026-09-03"); err == nil {
		t.Error("today's digest was claimed twice")
	}
	if _, err := pool.Exec(ctx, insert, membership, "2026-09-04"); err != nil {
		t.Errorf("tomorrow's digest was refused: %v", err)
	}
}

func TestSchema_TheNotificationKindTableHoldsExactlyTheTwoKinds(t *testing.T) {
	pool := migratedPool(t)

	rows, err := pool.Query(t.Context(), "SELECT name FROM notification_kind ORDER BY name")
	if err != nil {
		t.Fatalf("reading notification_kind: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading notification_kind: %v", err)
	}
	if want := []string{"activity", "digest"}; !slices.Equal(got, want) {
		t.Errorf("notification_kind holds %v, want %v", got, want)
	}
}

// Each of these is shown once, so what is stored is a hash and never the token.
func TestSchema_AnIssuedTokenIsStoredOnlyAsAHash(t *testing.T) {
	pool := migratedPool(t)

	rows, err := pool.Query(t.Context(), `
		SELECT table_name || '.' || column_name, coalesce(collation_name, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN ('session', 'invite', 'api_token', 'recovery_code')
		  AND column_name IN ('token_hash', 'code_hash', 'token', 'secret', 'plaintext')
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("reading the columns: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var column, collation string
		if err := rows.Scan(&column, &collation); err != nil {
			t.Fatalf("scanning a column: %v", err)
		}
		if collation != "C" {
			t.Errorf("%s is collated %q, want C", column, collation)
		}
		got = append(got, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the columns: %v", err)
	}

	want := []string{"api_token.token_hash", "invite.token_hash", "recovery_code.code_hash", "session.token_hash"}
	if !slices.Equal(got, want) {
		t.Errorf("the columns holding a token are\n\t%v\nwant\n\t%v", got, want)
	}
}

// Without invite.user_id, re-enrolling a member creates a second app_user row,
// and their care events stay on the first.
func TestSchema_AReEnrolmentInviteNamesTheUserItAdmits(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	insert := `INSERT INTO invite (garden_id, token_hash, role, created_by, expires_at, user_id)
	           VALUES ($1, $2, 'sitter', $3, now() + interval '1 hour', $4)`
	if _, err := pool.Exec(ctx, insert, garden, "re-enrol", user, user); err != nil {
		t.Fatalf("inserting the re-enrolment invite: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, garden, "join", user, nil); err != nil {
		t.Fatalf("inserting the join invite: %v", err)
	}

	const claim = `UPDATE invite
	               SET redeemed_at = now()
	               WHERE token_hash = $1 AND redeemed_at IS NULL AND expires_at > now()
	               RETURNING user_id`

	var named *uuid.UUID
	if err := pool.QueryRow(ctx, claim, "re-enrol").Scan(&named); err != nil {
		t.Fatalf("claiming the re-enrolment invite: %v", err)
	}
	if named == nil || *named != user {
		t.Errorf("the re-enrolment invite named %v, want the user %v it was issued against", named, user)
	}

	if err := pool.QueryRow(ctx, claim, "join").Scan(&named); err != nil {
		t.Fatalf("claiming the join invite: %v", err)
	}
	if named != nil {
		t.Errorf("the join invite named %v, want nobody", named)
	}

	if err := pool.QueryRow(ctx, claim, "re-enrol").Scan(&named); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("claiming the invite a second time returned %v, want no row", err)
	}
}

func TestSchema_DeletingAUserDeletesTheirRecoveryCodesAndInvites(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	garden, user := seedGardenAndUser(t, pool)

	_, err := pool.Exec(ctx, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'hash')", user)
	if err != nil {
		t.Fatalf("inserting the recovery code: %v", err)
	}
	owner := seedUser(t, pool, "Owner", "owner")
	_, err = pool.Exec(ctx, `
		INSERT INTO invite (garden_id, token_hash, role, created_by, expires_at, user_id)
		VALUES ($1, 're-enrol', 'sitter', $2, now() + interval '1 hour', $3)`, garden, owner, user)
	if err != nil {
		t.Fatalf("inserting the re-enrolment invite: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM app_user WHERE id = $1", user); err != nil {
		t.Fatalf("deleting the user: %v", err)
	}
	for _, table := range []string{"recovery_code", "invite"} {
		var left int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&left); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if left != 0 {
			t.Errorf("%d rows in %s outlived the user, want 0", left, table)
		}
	}
}

func TestSchema_ARecoveryCodeRecordsWhenItsBatchWasMadeAndWhenItWasUsed(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	_, user := seedGardenAndUser(t, pool)

	insert := "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, $2) RETURNING generated_at, used_at"
	var generated time.Time
	var used *time.Time
	if err := pool.QueryRow(ctx, insert, user, "first").Scan(&generated, &used); err != nil {
		t.Fatalf("inserting the first code: %v", err)
	}
	if used != nil {
		t.Errorf("a new code reads as used at %v, want unused", used)
	}
	if d := time.Since(generated).Abs(); d > time.Minute {
		t.Errorf("the batch instant is %v from now, want the moment it was written", d)
	}
}

// Redemption looks a code up by hash alone, so a shared hash would let one
// code open two accounts.
func TestSchema_TwoRecoveryCodesCannotShareAHash(t *testing.T) {
	ctx := t.Context()
	pool := migratedPool(t)
	_, user := seedGardenAndUser(t, pool)

	const insert = "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'hash')"
	if _, err := pool.Exec(ctx, insert, user); err != nil {
		t.Fatalf("inserting the code: %v", err)
	}
	if _, err := pool.Exec(ctx, insert, user); err == nil {
		t.Error("two codes hashing to the same value were accepted")
	}
}

func seedUser(t *testing.T, pool *pgxpool.Pool, displayName, handle string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		"INSERT INTO app_user (display_name, timezone, handle) VALUES ($1, 'Europe/London', $2) RETURNING id",
		displayName, handle).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the user %q: %v", displayName, err)
	}
	return id
}

func seedGarden(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(), "INSERT INTO garden (name) VALUES ($1) RETURNING id", name).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the garden %q: %v", name, err)
	}
	return id
}

func seedMembership(t *testing.T, pool *pgxpool.Pool, gardenID, userID uuid.UUID, role string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		"INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, $3, 8) RETURNING id",
		gardenID, userID, role).Scan(&id)
	if err != nil {
		t.Fatalf("inserting the %s membership: %v", role, err)
	}
	return id
}

func seedSession(t *testing.T, pool *pgxpool.Pool, gardenID, userID uuid.UUID, tokenHash string) {
	t.Helper()

	_, err := pool.Exec(t.Context(),
		"INSERT INTO session (token_hash, user_id, garden_id) VALUES ($1, $2, $3)",
		tokenHash, userID, gardenID)
	if err != nil {
		t.Fatalf("inserting the session %q: %v", tokenHash, err)
	}
}
