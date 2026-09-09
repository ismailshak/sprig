package auth

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

var samUserID = uuid.MustParse("00000000-0000-7000-8000-000000000003")

var sweptAt = signedInAt

// twoAccountsOnTx returns queries over a transaction holding Rosewood, Emma
// as its owner, and Sam, whose membership of it ended the day before
// signedInAt.
func twoAccountsOnTx(t *testing.T) (*store.Queries, pgx.Tx) {
	t.Helper()

	_, tx := sessionsOnTx(t)
	ctx := t.Context()
	seed := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO app_user (id, display_name, timezone, handle) VALUES ($1, 'Sam', 'Europe/London', 'sam')", []any{samUserID}},
		{"INSERT INTO membership (garden_id, user_id, role, digest_hour, expires_at) VALUES ($1, $2, 'sitter', 8, $3)", []any{testGardenID, samUserID, signedInAt.Add(-24 * time.Hour)}},
	}
	for _, row := range seed {
		if _, err := tx.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}
	return store.New(tx), tx
}

// sweepOnTx adds to twoAccountsOnTx one row of each kind the sweep deletes and
// one of each it keeps.
func sweepOnTx(t *testing.T) (*store.Queries, pgx.Tx) {
	t.Helper()

	q, tx := twoAccountsOnTx(t)
	ctx := t.Context()
	day := 24 * time.Hour
	seed := []struct {
		sql  string
		args []any
	}{
		// Sessions: one last seen exactly a TTL ago, one a second later, one an hour ago.
		{"INSERT INTO session (token_hash, user_id, last_seen_at) VALUES ('at-the-deadline', $1, $2), ('a-second-inside', $1, $3), ('an-hour-ago', $1, $4)",
			[]any{testUserID, sweptAt.Add(-testTTL), sweptAt.Add(-testTTL + time.Second), sweptAt.Add(-time.Hour)}},
		// Invites: redeemed 31 and 29 days ago, a re-enrolment link and a join invite both expired 31 days ago, and an open re-enrolment link.
		{`INSERT INTO invite (token_hash, garden_id, role, user_id, created_by, expires_at, redeemed_at) VALUES
			('redeemed-31d', $1, 'member', NULL, $2, $3, $4),
			('redeemed-29d', $1, 'member', NULL, $2, $3, $5),
			('reenrol-expired-31d', $1, 'owner', $2, $2, $4, NULL),
			('join-expired-31d', $1, 'sitter', NULL, $2, $4, NULL),
			('reenrol-open', $1, 'owner', $2, $2, $6, NULL)`,
			[]any{testGardenID, testUserID, sweptAt.Add(-40 * day), sweptAt.Add(-31 * day), sweptAt.Add(-29 * day), sweptAt.Add(5 * day)}},
		// Emma holds a batch from ten days ago and one from yesterday, each with a used code. Sam holds one batch.
		{`INSERT INTO recovery_code (user_id, code_hash, generated_at, used_at) VALUES
			($1, 'emma-old-1', $3, NULL), ($1, 'emma-old-2', $3, $3),
			($1, 'emma-new-1', $4, NULL), ($1, 'emma-new-2', $4, $4),
			($2, 'sam-1', $3, NULL), ($2, 'sam-2', $3, $3)`,
			[]any{testUserID, samUserID, sweptAt.Add(-10 * day), sweptAt.Add(-day)}},
		{"INSERT INTO api_token (garden_id, name, token_hash, prefix, created_by, expires_at) VALUES ($1, 'Kitchen display', 'expired-token', 'sprg_abcd', $2, $3)",
			[]any{testGardenID, testUserID, sweptAt.Add(-day)}},
	}
	for _, row := range seed {
		if _, err := tx.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}
	return q, tx
}

// hashes returns the token_hash or code_hash values left in table, in order.
func hashes(t *testing.T, tx pgx.Tx, table, column string) []string {
	t.Helper()

	rows, err := tx.Query(t.Context(), "SELECT "+column+" FROM "+table+" ORDER BY "+column)
	if err != nil {
		t.Fatalf("reading %s: %v", table, err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading %s: %v", table, err)
	}
	return got
}

func sweepOnce(t *testing.T, q *store.Queries) SweepReport {
	t.Helper()

	report, err := Sweep(t.Context(), q, sweptAt, testTTL)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return report
}

func TestSweep_ASessionUnusedForTheTTLIsDeletedAndOneUsedSinceIsKept(t *testing.T) {
	q, tx := sweepOnTx(t)

	report := sweepOnce(t, q)

	if report.Sessions != 1 {
		t.Errorf("the report counts %d sessions, want 1", report.Sessions)
	}
	got := hashes(t, tx, "session", "token_hash")
	if len(got) != 2 || got[0] != "a-second-inside" || got[1] != "an-hour-ago" {
		t.Errorf("the sessions left are %v, want the two used within the TTL", got)
	}
}

func TestSweep_AnInviteNoPageListsIsDeletedAfterAMonthAndAnExpiredJoinInviteIsKept(t *testing.T) {
	q, tx := sweepOnTx(t)

	report := sweepOnce(t, q)

	if report.Invites != 2 {
		t.Errorf("the report counts %d invites, want 2", report.Invites)
	}
	got := hashes(t, tx, "invite", "token_hash")
	want := []string{"join-expired-31d", "redeemed-29d", "reenrol-open"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("the invites left are %v, want %v: the expired join invite is on the People page", got, want)
	}
}

func TestSweep_CodesFromAReplacedBatchAreDeletedAndTheNewestBatchIsKeptWhole(t *testing.T) {
	q, tx := sweepOnTx(t)

	report := sweepOnce(t, q)

	if report.RecoveryCodes != 2 {
		t.Errorf("the report counts %d recovery codes, want 2", report.RecoveryCodes)
	}
	got := hashes(t, tx, "recovery_code", "code_hash")
	want := []string{"emma-new-1", "emma-new-2", "sam-1", "sam-2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("the codes left are %v, want %v: each account's newest batch, used codes included", got, want)
	}
}

func TestSweep_AnExpiredTokenAndAnEndedMembershipAreKept(t *testing.T) {
	q, tx := sweepOnTx(t)

	sweepOnce(t, q)

	var tokens, memberships int
	if err := tx.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM api_token), (SELECT count(*) FROM membership)").Scan(&tokens, &memberships); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if tokens != 1 {
		t.Errorf("%d tokens are left, want the expired one: the Tokens page lists it", tokens)
	}
	if memberships != 2 {
		t.Errorf("%d memberships are left, want both: People lists the ended one", memberships)
	}
}

func TestSweep_ASecondSweepDeletesNothing(t *testing.T) {
	q, _ := sweepOnTx(t)
	sweepOnce(t, q)

	report := sweepOnce(t, q)

	if report != (SweepReport{}) {
		t.Errorf("the second sweep reports %+v, want nothing deleted", report)
	}
}

func TestSweeper_RunSweepsAtStartupAndAgainAfterTheInterval(t *testing.T) {
	// A pgx transaction cannot be used from two goroutines at once, so this
	// test opens a database of its own instead of taking the shared
	// transaction the other tests run on.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pool, err := store.Open(ctx, pgtest.Fresh(t, migrateSchema))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	exec("INSERT INTO app_user (id, display_name, timezone, handle) VALUES ($1, 'Emma', 'Europe/London', 'emma')", testUserID)
	exec("INSERT INTO session (token_hash, user_id, last_seen_at) VALUES ('expired', $1, $2)", testUserID, sweptAt.Add(-testTTL))
	sessions := func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM session").Scan(&n); err != nil {
			t.Fatalf("counting: %v", err)
		}
		return n
	}
	sweeper := NewSweeper(slog.New(slog.DiscardHandler), store.New(pool), testTTL)
	sweeper.interval = 20 * time.Millisecond
	sweeper.now = func() time.Time { return sweptAt }

	done := make(chan struct{})
	go func() {
		defer close(done)
		sweeper.Run(ctx)
	}()
	waitFor(t, func() bool { return sessions() == 0 })

	// A session that expires after the first sweep is deleted by a later one.
	exec("INSERT INTO session (token_hash, user_id, last_seen_at) VALUES ('expired-later', $1, $2)", testUserID, sweptAt.Add(-testTTL))
	waitFor(t, func() bool { return sessions() == 0 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx ending")
	}
}

// waitFor fails the test unless cond becomes true within two seconds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("the sweep did not run within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
