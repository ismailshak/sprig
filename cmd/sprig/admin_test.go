package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ismailshak/sprig/db"
	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}

func migrateSchema(ctx context.Context, databaseURL string) error {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	// CREATE DATABASE refuses a template another session is connected to.
	defer pool.Close()

	return store.Migrate(ctx, pool, db.Migrations, slog.New(slog.DiscardHandler))
}

var (
	adminGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000401")
	adminUserID   = uuid.MustParse("00000000-0000-7000-8000-000000000402")
)

// adminDatabase is a database holding Rosewood and Emma, its owner, with the
// environment sprig admin reads to reach it.
type adminDatabase struct {
	pool *pgxpool.Pool
	env  map[string]string
}

func adminFixture(t *testing.T) *adminDatabase {
	t.Helper()

	ctx := t.Context()
	databaseURL := pgtest.Fresh(t, migrateSchema)
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(pool.Close)
	seed := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO garden (id, name) VALUES ($1, 'Rosewood')", []any{adminGardenID}},
		{"INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Emma', 'emma', 'Europe/London')", []any{adminUserID}},
		{"INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", []any{adminGardenID, adminUserID}},
	}
	for _, row := range seed {
		if _, err := pool.Exec(ctx, row.sql, row.args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, row.sql)
		}
	}
	return &adminDatabase{
		pool: pool,
		env: map[string]string{
			"SPRIG_DATABASE_URL": databaseURL,
			"SPRIG_BASE_URL":     "https://sprig.example.com",
		},
	}
}

func (d *adminDatabase) getenv(k string) string { return d.env[k] }

func (d *adminDatabase) counts(t *testing.T) (users, memberships, invites int) {
	t.Helper()

	if err := d.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM app_user), (SELECT count(*) FROM membership), (SELECT count(*) FROM invite)").Scan(&users, &memberships, &invites); err != nil {
		t.Fatalf("counting: %v", err)
	}
	return users, memberships, invites
}

func TestAdminInvite_PrintsASignInLinkForTheAccount(t *testing.T) {
	d := adminFixture(t)
	var stdout bytes.Buffer

	err := subcommand(t.Context(), []string{"admin", "invite", "--user", "emma"}, d.getenv, &stdout)
	if err != nil {
		t.Fatalf("admin invite: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "https://sprig.example.com/invite/") {
		t.Fatalf("admin invite printed:\n%s\nwant the link on the first line and one sentence after it", stdout.String())
	}
	token := strings.TrimPrefix(lines[0], "https://sprig.example.com/invite/")
	invite, err := store.New(d.pool).GetInviteByTokenHash(t.Context(), auth.HashToken(token))
	if err != nil {
		t.Fatalf("no invite row has the hash of the printed token: %v", err)
	}
	if invite.Invite.UserID == nil || *invite.Invite.UserID != adminUserID || invite.Invite.GardenID != adminGardenID {
		t.Errorf("the link is for %v on %v, want Emma on Rosewood", invite.Invite.UserID, invite.Invite.GardenID)
	}
	if !strings.Contains(lines[1], "Emma") {
		t.Errorf("the sentence %q does not name the account", lines[1])
	}
	if users, memberships, invites := d.counts(t); users != 1 || memberships != 1 || invites != 1 {
		t.Errorf("the database holds %d accounts, %d memberships and %d invites, want 1, 1 and 1", users, memberships, invites)
	}
}

func TestAdminInvite_AnUnknownHandleIsRefusedByNameAndWritesNothing(t *testing.T) {
	d := adminFixture(t)
	var stdout bytes.Buffer

	err := subcommand(t.Context(), []string{"admin", "invite", "--user", "emily"}, d.getenv, &stdout)

	if err == nil || !strings.Contains(err.Error(), "emily") {
		t.Errorf("err = %v, want one naming the handle", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout holds %q, want nothing", stdout.String())
	}
	if users, memberships, invites := d.counts(t); users != 1 || memberships != 1 || invites != 0 {
		t.Errorf("the database holds %d accounts, %d memberships and %d invites, want 1, 1 and 0", users, memberships, invites)
	}
}

func TestAdmin_AnythingOtherThanInviteWithAHandleIsRefusedBeforeTheConfigIsRead(t *testing.T) {
	for _, args := range [][]string{
		{"admin"},
		{"admin", "invite"},
		{"admin", "invite", "emma"},
		{"admin", "invite", "--user", "emma", "extra"},
		{"admin", "remove", "--user", "emma"},
	} {
		if err := subcommand(t.Context(), args, noEnv, &bytes.Buffer{}); err == nil {
			t.Errorf("sprig %s ran without an error", strings.Join(args, " "))
		} else if strings.Contains(err.Error(), "SPRIG_DATABASE_URL") {
			t.Errorf("sprig %s read the config before refusing the arguments: %v", strings.Join(args, " "), err)
		}
	}
}
