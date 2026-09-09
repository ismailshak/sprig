package main

import (
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ismailshak/sprig/internal/auth"
)

// The Tokens page shows the prefix, so the token must start with it for a
// person to match one to a device.
func TestSeed_EachTokenStartsWithItsStoredPrefix(t *testing.T) {
	for _, g := range []garden{home(), upstairs()} {
		for _, tok := range g.tokens {
			if !strings.HasPrefix(tok.token, tok.prefix) {
				t.Errorf("%s is %q, which does not start with its prefix %q", tok.name, tok.token, tok.prefix)
			}
		}
	}
}

// The 90-day cap is a Go constant, so the fixture is the one place a token
// could be written past it.
func TestSeed_NoTokenExpiresLaterThanTheCap(t *testing.T) {
	ceiling := int(auth.MaxTokenLifetime / (24 * time.Hour))
	for _, g := range []garden{home(), upstairs()} {
		for _, tok := range g.tokens {
			if life := tok.daysOld + tok.expiresInDays; life > ceiling {
				t.Errorf("%s lives %d days, and the cap is %d", tok.name, life, ceiling)
			}
		}
	}
}

// A developer using one of the token constants must reach the row it belongs to.
func TestSeed_EachSecretConstantHashesToItsRow(t *testing.T) {
	pool := seeded(t)
	ctx := t.Context()

	var prefix string
	if err := pool.QueryRow(ctx, "SELECT prefix FROM api_token WHERE token_hash = $1", auth.HashToken(kitchenDisplayToken)).Scan(&prefix); err != nil {
		t.Errorf("the kitchen display's token resolves to no row: %v", err)
	} else if prefix != "sprg_7c1f" {
		t.Errorf("the kitchen display's token resolves to the row with prefix %s", prefix)
	}

	var role string
	if err := pool.QueryRow(ctx, "SELECT role FROM invite WHERE token_hash = $1 AND redeemed_at IS NULL", auth.HashToken(sitterInviteToken)).Scan(&role); err != nil {
		t.Errorf("the sitter invite resolves to no unredeemed row: %v", err)
	} else if role != "sitter" {
		t.Errorf("the sitter invite admits a %s", role)
	}

	batch := recovery()
	for i, code := range batch.codes {
		var owner uuid.UUID
		if err := pool.QueryRow(ctx, "SELECT user_id FROM recovery_code WHERE code_hash = $1", auth.HashToken(code)).Scan(&owner); err != nil {
			t.Errorf("recovery code %d resolves to no row: %v", i+1, err)
		} else if owner != batch.owner.id {
			t.Errorf("recovery code %d belongs to %v, want %s", i+1, owner, batch.owner.handle)
		}
	}
}

func TestSeed_NoSecretIsStoredInTheClear(t *testing.T) {
	pool := seeded(t)
	ctx := t.Context()

	columns := []string{
		"api_token.token_hash", "invite.token_hash", "recovery_code.code_hash",
	}
	for _, column := range columns {
		table, name, _ := strings.Cut(column, ".")
		rows, err := pool.Query(ctx, "SELECT "+name+" FROM "+table)
		if err != nil {
			t.Fatalf("reading %s: %v", column, err)
		}
		for rows.Next() {
			var stored string
			if err := rows.Scan(&stored); err != nil {
				rows.Close()
				t.Fatalf("scanning %s: %v", column, err)
			}
			if len(stored) != 64 || strings.Trim(stored, "0123456789abcdef") != "" {
				t.Errorf("%s holds %q, which is not a hex SHA-256", column, stored)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", column, err)
		}
	}
}

func TestSeed_EllieHasAnIPhonePasskeyAndAMacBookAirPasskey(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)

	type row struct {
		Name     string
		LastUsed time.Time
	}
	rows := collect[row](t, pool, `
		SELECT name, last_used_at FROM passkey_credential
		WHERE user_id = $1 ORDER BY last_used_at DESC`, ellie.id)

	want := []struct {
		name     string
		usedDays int
	}{{"iPhone", 0}, {"MacBook Air", 4}}
	if len(rows) != len(want) {
		t.Fatalf("%s holds %d passkeys, want %d", ellie.handle, len(rows), len(want))
	}
	for i, w := range want {
		if rows[i].Name != w.name {
			t.Errorf("passkey %d is %s, want %s", i, rows[i].Name, w.name)
		}
		if got := daysBetween(rows[i].LastUsed, ref); got != w.usedDays {
			t.Errorf("%s was last used %d days ago, want %d", w.name, got, w.usedDays)
		}
	}
}

func TestSeed_OneSitterInviteIsPending(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)

	type row struct {
		Role    string
		Created time.Time
		Expires time.Time
		Joins   bool
	}
	rows := collect[row](t, pool, `
		SELECT role, created_at, expires_at, user_id IS NULL FROM invite
		WHERE garden_id = $1 AND redeemed_at IS NULL`, home().id)
	if len(rows) != 1 {
		t.Fatalf("%d invites are pending, want 1", len(rows))
	}
	inv := rows[0]
	if inv.Role != "sitter" {
		t.Errorf("the invite admits a %s, want a sitter", inv.Role)
	}
	if got := daysBetween(inv.Created, ref); got != 2 {
		t.Errorf("the invite was made %d days ago, want 2", got)
	}
	if got := daysBetween(ref, inv.Expires); got != 5 {
		t.Errorf("the invite expires in %d days, want 5", got)
	}
	if !inv.Joins {
		t.Error("the invite names a user, so it re-enrols rather than admits")
	}
}

func TestSeed_TheKitchenDisplayTokenIsLiveAndTheSpareHasExpired(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)

	type row struct {
		Name, Prefix           string
		Created, Used, Expires time.Time
		Unrevoked              bool
	}
	rows := collect[row](t, pool, `
		SELECT name, prefix, created_at, last_used_at, expires_at, revoked_at IS NULL FROM api_token
		WHERE garden_id = $1 ORDER BY expires_at DESC`, home().id)

	want := []struct {
		name, prefix                  string
		madeDays, usedDays, expiresIn int
	}{
		{"The kitchen display", "sprg_7c1f", 40, 0, 50},
		{"The spare display", "sprg_2ea8", 96, 74, -6},
	}
	if len(rows) != len(want) {
		t.Fatalf("%s holds %d tokens, want %d", home().name, len(rows), len(want))
	}
	for i, w := range want {
		tok := rows[i]
		if tok.Name != w.name || tok.Prefix != w.prefix {
			t.Errorf("token %d is %s %s, want %s %s", i, tok.Name, tok.Prefix, w.name, w.prefix)
		}
		if got := daysBetween(tok.Created, ref); got != w.madeDays {
			t.Errorf("%s was made %d days ago, want %d", w.name, got, w.madeDays)
		}
		if got := daysBetween(tok.Used, ref); got != w.usedDays {
			t.Errorf("%s was last used %d days ago, want %d", w.name, got, w.usedDays)
		}
		if got := daysBetween(ref, tok.Expires); got != w.expiresIn {
			t.Errorf("%s expires in %d days, want %d", w.name, got, w.expiresIn)
		}
		if !tok.Unrevoked {
			t.Errorf("%s was revoked, want a token that ran out on its own", w.name)
		}
	}
}

func TestSeed_EightOfTenRecoveryCodesAreLeft(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)

	var total, left int
	var generated time.Time
	err := pool.QueryRow(t.Context(), `
		SELECT count(*), count(*) FILTER (WHERE used_at IS NULL), min(generated_at)
		FROM recovery_code WHERE user_id = $1`, ellie.id).Scan(&total, &left, &generated)
	if err != nil {
		t.Fatalf("counting %s's recovery codes: %v", ellie.handle, err)
	}
	if total != 10 || left != 8 {
		t.Errorf("%s holds %d codes with %d left, want 10 with 8 left", ellie.handle, total, left)
	}
	if got := daysBetween(generated, ref); got != 34 {
		t.Errorf("the batch was made %d days ago, want 34", got)
	}

	var batches int
	if err := pool.QueryRow(t.Context(), "SELECT count(DISTINCT generated_at) FROM recovery_code WHERE user_id = $1", ellie.id).Scan(&batches); err != nil {
		t.Fatalf("counting the batches: %v", err)
	}
	if batches != 1 {
		t.Errorf("%s holds %d batches, and the table holds one live batch or none", ellie.handle, batches)
	}
}

func TestSeed_EllieGetsTheDigestAtEightInTwoBrowsers(t *testing.T) {
	pool := seeded(t)
	ref := testReference(t)
	ctx := t.Context()

	var digest, activity bool
	var hour int
	err := pool.QueryRow(ctx, `
		SELECT d.enabled, a.enabled, m.digest_hour
		FROM membership m
		JOIN notification_preference d ON d.membership_id = m.id AND d.kind = 'digest'
		JOIN notification_preference a ON a.membership_id = m.id AND a.kind = 'activity'
		WHERE m.garden_id = $1 AND m.user_id = $2`, home().id, ellie.id).Scan(&digest, &activity, &hour)
	if err != nil {
		t.Fatalf("reading %s's preferences: %v", ellie.handle, err)
	}
	if !digest || activity || hour != 8 {
		t.Errorf("digest=%v activity=%v hour=%d, want the digest on at eight and activity off", digest, activity, hour)
	}

	// Every membership has a row for both kinds, so the page never has to
	// decide what a missing row means.
	var memberships, preferences int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM membership), (SELECT count(*) FROM notification_preference)").Scan(&memberships, &preferences); err != nil {
		t.Fatalf("counting the preferences: %v", err)
	}
	if preferences != 2*memberships {
		t.Errorf("%d memberships hold %d preference rows, want two each", memberships, preferences)
	}

	type row struct {
		Agent string
		Sent  time.Time
	}
	rows := collect[row](t, pool, `
		SELECT user_agent, last_sent_at FROM push_subscription
		WHERE user_id = $1 ORDER BY last_sent_at DESC`, ellie.id)
	want := []struct {
		agent    string
		sentDays int
	}{{"iPhone", 0}, {"Macintosh", 11}}
	if len(rows) != len(want) {
		t.Fatalf("%s has %d browsers subscribed, want %d", ellie.handle, len(rows), len(want))
	}
	for i, w := range want {
		if !strings.Contains(rows[i].Agent, w.agent) {
			t.Errorf("browser %d reports %q, want a %s", i, rows[i].Agent, w.agent)
		}
		if got := daysBetween(rows[i].Sent, ref); got != w.sentDays {
			t.Errorf("the %s was last sent to %d days ago, want %d", w.agent, got, w.sentDays)
		}
	}
}

// collect scans rows into T by column position, so a column of the wrong type
// fails here instead of comparing as a zero value.
func collect[T any](t *testing.T, pool *pgxpool.Pool, sql string, args ...any) []T {
	t.Helper()

	rows, err := pool.Query(t.Context(), sql, args...)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[T])
	if err != nil {
		t.Fatalf("scanning: %v", err)
	}
	return out
}
