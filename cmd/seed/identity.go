package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
)

// The rows the More screens read belong to Ellie and Home, so signing in as
// Ellie shows every one of those screens with something on it.

// An API token is sent to /api/chores as a bearer token. An invite is opened
// as /invite/<token>.
//
//nolint:gosec // These are development constants that only ever reach a database on this machine.
const (
	kitchenDisplayToken = "sprg_7c1f-development-kitchen-display"
	spareDisplayToken   = "sprg_2ea8-development-spare-display"
	sitterInviteToken   = "development-sitter-invite"
)

type passkey struct {
	id          uuid.UUID
	owner       *person
	name        string
	transports  []string
	daysOld     int
	usedDaysAgo int
}

// passkeys returns two credentials for Ellie. The public key is a text label
// rather than a COSE key and there is no private key, so neither can sign
// anyone in. Code that parses a stored key fails on both, so passkey flows are
// tested with a credential the browser registered.
func passkeys() []passkey {
	return []passkey{
		{id: seedID(tablePasskeyCredential, 1), owner: &ellie, name: "iPhone", transports: []string{"internal", "hybrid"}, daysOld: ellie.daysOld},
		{id: seedID(tablePasskeyCredential, 2), owner: &ellie, name: "MacBook Air", transports: []string{"internal"}, daysOld: 600, usedDaysAgo: 4},
	}
}

type invite struct {
	id            uuid.UUID
	token         string
	role          string
	createdBy     *person
	daysOld       int
	expiresInDays int
}

type apiToken struct {
	id          uuid.UUID
	name        string
	token       string
	prefix      string
	createdBy   *person
	daysOld     int
	usedDaysAgo int
	// Negative means the token has already expired.
	expiresInDays int
}

type recoveryBatch struct {
	owner   *person
	daysOld int
	codes   []string
	// One entry per used code, applied to codes in order from the start.
	usedDaysAgo []int
}

// recovery returns Ellie's ten recovery codes, two of them used.
func recovery() recoveryBatch {
	return recoveryBatch{
		owner:       &ellie,
		daysOld:     34,
		usedDaysAgo: []int{21, 7},
		codes: []string{
			"k4rt-9wme-3xqd", "h72p-vc8n-md4s", "b9xa-3fkt-7rjw", "q5nd-hs2y-4vbc",
			"t8mw-r6ep-9zla", "j3cv-8dxu-2ntk", "z7bq-4may-6shf", "v2ke-nw9r-5tpd",
			"y6hs-2jlc-8wam", "f4dz-7ntq-3bkr",
		},
	}
}

type browser struct {
	id    uuid.UUID
	owner *person
	slug  string
	// The Notifications page derives the row's label from this, so it is a
	// real User-Agent string rather than a name.
	userAgent   string
	daysOld     int
	sentDaysAgo int
}

// browsers returns two push subscriptions for Ellie. Their endpoints are under
// the .invalid domain, which never resolves, so a digest job run against a
// seeded database fails at DNS rather than sending anything.
func browsers() []browser {
	return []browser{
		{
			id:          seedID(tablePushSubscription, 1),
			owner:       &ellie,
			slug:        "iphone-safari",
			userAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1",
			daysOld:     200,
			sentDaysAgo: 0,
		},
		{
			id:          seedID(tablePushSubscription, 2),
			owner:       &ellie,
			slug:        "macbook-air-chrome",
			userAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36",
			daysOld:     90,
			sentDaysAgo: 11,
		},
	}
}

func (b *browser) endpoint() string {
	return "https://push.invalid/development/" + b.slug
}

// writeIdentity writes the rows that belong to a user: passkeys, recovery codes
// and push subscriptions. Invites and tokens belong to a garden, so writeGarden
// writes those.
func writeIdentity(ctx context.Context, tx pgx.Tx, ref time.Time) error {
	for _, k := range passkeys() {
		if _, err := tx.Exec(ctx,
			`INSERT INTO passkey_credential (id, user_id, credential_id, name, public_key, transports, created_at, last_used_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			k.id, k.owner.id, base64.RawURLEncoding.EncodeToString(k.id[:]), k.name,
			[]byte("development passkey with no private half: "+k.name), k.transports,
			ref.AddDate(0, 0, -k.daysOld), ref.AddDate(0, 0, -k.usedDaysAgo),
		); err != nil {
			return fmt.Errorf("writing %s's passkey %s: %w", k.owner.handle, k.name, err)
		}
	}

	batch := recovery()
	generated := ref.AddDate(0, 0, -batch.daysOld)
	for i, code := range batch.codes {
		var used *time.Time
		if i < len(batch.usedDaysAgo) {
			at := ref.AddDate(0, 0, -batch.usedDaysAgo[i])
			used = &at
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO recovery_code (id, user_id, code_hash, generated_at, used_at)
				VALUES ($1, $2, $3, $4, $5)`,
			seedID(tableRecoveryCode, i+1), batch.owner.id, auth.HashToken(code), generated, used,
		); err != nil {
			return fmt.Errorf("writing %s's recovery code %d: %w", batch.owner.handle, i+1, err)
		}
	}

	for _, b := range browsers() {
		p256dh := base64.RawURLEncoding.EncodeToString([]byte("development p256dh key: " + b.slug))
		authKey := base64.RawURLEncoding.EncodeToString([]byte("development auth key: " + b.slug))
		if _, err := tx.Exec(ctx,
			`INSERT INTO push_subscription (id, user_id, endpoint, p256dh_key, auth_key, user_agent, created_at, last_sent_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			b.id, b.owner.id, b.endpoint(), p256dh, authKey, b.userAgent,
			ref.AddDate(0, 0, -b.daysOld), ref.AddDate(0, 0, -b.sentDaysAgo),
		); err != nil {
			return fmt.Errorf("writing %s's subscription %s: %w", b.owner.handle, b.slug, err)
		}
	}
	return nil
}

func writeInvites(ctx context.Context, tx pgx.Tx, g *garden, ref time.Time) error {
	for _, inv := range g.invites {
		if _, err := tx.Exec(ctx,
			`INSERT INTO invite (id, garden_id, token_hash, role, created_by, created_at, expires_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			inv.id, g.id, auth.HashToken(inv.token), inv.role, inv.createdBy.id,
			ref.AddDate(0, 0, -inv.daysOld), ref.AddDate(0, 0, inv.expiresInDays),
		); err != nil {
			return fmt.Errorf("writing the %s invite to %s: %w", inv.role, g.name, err)
		}
	}
	return nil
}

func writeTokens(ctx context.Context, tx pgx.Tx, g *garden, ref time.Time) error {
	for _, tok := range g.tokens {
		if _, err := tx.Exec(ctx,
			`INSERT INTO api_token (id, garden_id, name, token_hash, prefix, created_by, created_at, expires_at, last_used_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			tok.id, g.id, tok.name, auth.HashToken(tok.token), tok.prefix, tok.createdBy.id,
			ref.AddDate(0, 0, -tok.daysOld), ref.AddDate(0, 0, tok.expiresInDays), ref.AddDate(0, 0, -tok.usedDaysAgo),
		); err != nil {
			return fmt.Errorf("writing the token %s: %w", tok.name, err)
		}
	}
	return nil
}

// writePreferences writes a row for both notification kinds, as the app does
// when it creates a membership, so the Notifications page always finds one.
func writePreferences(ctx context.Context, tx pgx.Tx, g *garden, m *membership) error {
	kinds := []struct {
		kind    string
		enabled bool
	}{{"digest", !m.digestOff}, {"activity", m.activity}}
	for _, k := range kinds {
		if _, err := tx.Exec(ctx,
			"INSERT INTO notification_preference (membership_id, kind, enabled) VALUES ($1, $2, $3)",
			m.id, k.kind, k.enabled,
		); err != nil {
			return fmt.Errorf("writing %s's %s preference for %s: %w", m.person.handle, k.kind, g.name, err)
		}
	}
	return nil
}
