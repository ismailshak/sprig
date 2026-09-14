package auth

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	testGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000001")
	testUserID   = uuid.MustParse("00000000-0000-7000-8000-000000000002")

	signedInAt = time.Date(2026, time.September, 4, 9, 0, 0, 0, time.UTC)
	testTTL    = 30 * 24 * time.Hour
	testCookie = CookieSettings{Name: "__Host-sprig_session", Secure: true}
)

func migrateSchema(ctx context.Context, databaseURL string) error {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	// CREATE DATABASE refuses a template another session is connected to.
	defer pool.Close()

	return store.Migrate(ctx, pool, db.Migrations, slog.New(slog.DiscardHandler))
}

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

// sessionsOnTx returns Sessions running inside a transaction that holds one
// garden, one user and the membership joining them. The transaction is rolled
// back when the test ends. Every test inserts the same ids, so none runs in
// parallel.
func sessionsOnTx(t *testing.T) (*Sessions, pgx.Tx) {
	t.Helper()

	ctx := t.Context()
	tx := pgtest.Tx(t, migrateSchema)

	seed := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO garden (id, name) VALUES ($1, 'Rosewood')", []any{testGardenID}},
		{"INSERT INTO app_user (id, display_name, timezone, handle) VALUES ($1, 'Emma', 'Europe/London', 'emma')", []any{testUserID}},
		{"INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", []any{testGardenID, testUserID}},
	}
	for _, row := range seed {
		if _, err := tx.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}

	return NewSessions(store.New(tx), testTTL, testCookie), tx
}

// Sessions holds nothing in memory, so a second instance over the same table
// behaves like a restarted process.
func TestSessions_ATokenResolvesFromTheTableAlone(t *testing.T) {
	ctx := t.Context()
	sessions, tx := sessionsOnTx(t)

	token, created, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "Safari on iPhone")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	restarted := NewSessions(store.New(tx), testTTL, testCookie)
	got, _, err := restarted.Lookup(ctx, signedInAt.Add(time.Minute), token)
	if err != nil {
		t.Fatalf("Lookup through a second Sessions: %v", err)
	}
	if got.ID != created.ID || got.UserID != testUserID || got.GardenID == nil || *got.GardenID != testGardenID {
		t.Errorf("Lookup resolved session %v for user %v on garden %v, want %v for %v on %v",
			got.ID, got.UserID, got.GardenID, created.ID, testUserID, testGardenID)
	}
	if got.UserAgent == nil || *got.UserAgent != "Safari on iPhone" {
		t.Errorf("user agent = %v, want the label the session was started with", got.UserAgent)
	}
	if got.TokenHash == token || got.TokenHash != HashToken(token) {
		t.Errorf("the row holds %q, want the hash of the token and never the token", got.TokenHash)
	}
}

func TestSessions_AnUnknownTokenIsNoSession(t *testing.T) {
	sessions, _ := sessionsOnTx(t)

	_, _, err := sessions.Lookup(t.Context(), signedInAt, NewSessionToken())
	if !errors.Is(err, ErrNoSession) {
		t.Fatalf("Lookup of a token never issued = %v, want ErrNoSession", err)
	}
}

func TestSessions_LookupMovesTheDeadlineForward(t *testing.T) {
	ctx := t.Context()
	sessions, _ := sessionsOnTx(t)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	dayTwentyNine := signedInAt.AddDate(0, 0, 29)
	got, _, err := sessions.Lookup(ctx, dayTwentyNine, token)
	if err != nil {
		t.Fatalf("Lookup on day 29: %v", err)
	}
	if !got.LastSeenAt.Equal(dayTwentyNine) {
		t.Errorf("last seen = %s after a lookup at %s", got.LastSeenAt, dayTwentyNine)
	}
	if !got.CreatedAt.Equal(signedInAt) {
		t.Errorf("created at moved to %s, want it left at %s", got.CreatedAt, signedInAt)
	}

	// Day 45 is past the TTL counted from sign-in but inside it counted from
	// the day 29 lookup.
	if _, _, err := sessions.Lookup(ctx, signedInAt.AddDate(0, 0, 45), token); err != nil {
		t.Errorf("Lookup on day 45, sixteen days after the last use: %v", err)
	}
}

func TestSessions_ALookupWithinTheTouchIntervalLeavesLastSeenAtUnchanged(t *testing.T) {
	ctx := t.Context()
	sessions, tx := sessionsOnTx(t)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, touched, err := sessions.Lookup(ctx, signedInAt.Add(touchInterval-time.Second), token)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if touched || !got.LastSeenAt.Equal(signedInAt) {
		t.Errorf("Lookup returned touched = %v and last seen %s, want false and %s", touched, got.LastSeenAt, signedInAt)
	}

	var stored time.Time
	if err := tx.QueryRow(ctx, "SELECT last_seen_at FROM session WHERE token_hash = $1", HashToken(token)).Scan(&stored); err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if !stored.Equal(signedInAt) {
		t.Errorf("the row's last_seen_at = %s, want it left at %s", stored, signedInAt)
	}
}

func TestSessions_ALookupAtTheTouchIntervalMovesTheDeadline(t *testing.T) {
	ctx := t.Context()
	sessions, _ := sessionsOnTx(t)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := signedInAt.Add(touchInterval)
	got, touched, err := sessions.Lookup(ctx, at, token)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !touched || !got.LastSeenAt.Equal(at) {
		t.Errorf("Lookup returned touched = %v and last seen %s, want true and %s", touched, got.LastSeenAt, at)
	}
}

// A one-minute TTL is shorter than the touch interval.
func TestSessions_ASessionUsedEveryTenSecondsUnderAOneMinuteTTLStaysLive(t *testing.T) {
	ctx := t.Context()
	_, tx := sessionsOnTx(t)
	sessions := NewSessions(store.New(tx), time.Minute, testCookie)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for at := signedInAt.Add(10 * time.Second); at.Before(signedInAt.Add(5 * time.Minute)); at = at.Add(10 * time.Second) {
		if _, _, err := sessions.Lookup(ctx, at, token); err != nil {
			t.Fatalf("Lookup %s after sign-in: %v", at.Sub(signedInAt), err)
		}
	}
}

func TestSessions_AClockEarlierThanTheRowDoesNotMoveTheDeadlineBack(t *testing.T) {
	ctx := t.Context()
	sessions, _ := sessionsOnTx(t)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _, err := sessions.Lookup(ctx, signedInAt.Add(-time.Hour), token)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !got.LastSeenAt.Equal(signedInAt) {
		t.Errorf("last seen = %s, want it left at %s", got.LastSeenAt, signedInAt)
	}
}

func TestSessions_AnExpiredSessionIsRefusedAndItsRowDeleted(t *testing.T) {
	ctx := t.Context()
	sessions, tx := sessionsOnTx(t)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, _, err = sessions.Lookup(ctx, signedInAt.Add(testTTL), token)
	if !errors.Is(err, ErrNoSession) {
		t.Fatalf("Lookup at the deadline = %v, want ErrNoSession", err)
	}

	var left int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM session WHERE token_hash = $1", HashToken(token)).Scan(&left); err != nil {
		t.Fatalf("counting the row: %v", err)
	}
	if left != 0 {
		t.Errorf("the expired row is still there")
	}

	if _, _, err := sessions.Lookup(ctx, signedInAt, token); !errors.Is(err, ErrNoSession) {
		t.Errorf("Lookup after expiry with an earlier clock = %v, want ErrNoSession", err)
	}
}

func TestSessions_DeleteEndsTheSessionServerSide(t *testing.T) {
	ctx := t.Context()
	sessions, _ := sessionsOnTx(t)

	token, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := sessions.Delete(ctx, token); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := sessions.Lookup(ctx, signedInAt.Add(time.Minute), token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Lookup after Delete = %v, want ErrNoSession", err)
	}
	if err := sessions.Delete(ctx, token); err != nil {
		t.Errorf("Delete of a session already ended: %v, want nil", err)
	}
}

// The two sessions share a user and a garden, so only the token hash
// distinguishes them.
func TestSessions_DeleteEndsOneSessionAndLeavesTheOthers(t *testing.T) {
	ctx := t.Context()
	sessions, _ := sessionsOnTx(t)

	phone, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "Safari on iPhone")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	laptop, _, err := sessions.Create(ctx, signedInAt, testUserID, &testGardenID, nil, "Firefox on macOS")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := sessions.Delete(ctx, phone); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := sessions.Lookup(ctx, signedInAt.Add(time.Minute), phone); !errors.Is(err, ErrNoSession) {
		t.Errorf("Lookup of the deleted session = %v, want ErrNoSession", err)
	}
	got, _, err := sessions.Lookup(ctx, signedInAt.Add(time.Minute), laptop)
	if err != nil {
		t.Fatalf("Lookup of the other session after Delete: %v", err)
	}
	if got.UserAgent == nil || *got.UserAgent != "Firefox on macOS" {
		t.Errorf("the surviving session is %v, want the laptop's", got.UserAgent)
	}
}
