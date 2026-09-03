package store

import (
	"slices"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Removing somebody has to take effect at once rather than on whatever page
// they load next.
func TestSchema_ASessionGoesWithTheMembershipItBelongsTo(t *testing.T) {
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

	rows, err := pool.Query(ctx, "SELECT token_hash FROM session WHERE user_id = $1 ORDER BY token_hash", user)
	if err != nil {
		t.Fatalf("reading the sessions back: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading the sessions back: %v", err)
	}
	if want := []string{"elsewhere"}; !slices.Equal(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
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

// That there is an expiry at all is the schema's rule. The ninety-day ceiling on
// top of it is a constant in Go.
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

func TestSchema_TheNotificationKindsAreTheTwoWrittenDown(t *testing.T) {
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
		  AND table_name IN ('session', 'invite', 'api_token')
		  AND column_name IN ('token_hash', 'token', 'secret', 'plaintext')
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

	want := []string{"api_token.token_hash", "invite.token_hash", "session.token_hash"}
	if !slices.Equal(got, want) {
		t.Errorf("the columns holding a token are\n\t%v\nwant\n\t%v", got, want)
	}
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
		"INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, $3) RETURNING id",
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
