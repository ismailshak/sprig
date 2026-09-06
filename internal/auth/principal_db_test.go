package auth

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

var (
	// Fairview is the second garden and Noor owns it.
	otherGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000003")
	otherUserID   = uuid.MustParse("00000000-0000-7000-8000-000000000004")
)

// resolverOnTx returns a Resolver running inside a transaction that holds two
// gardens. Emma owns Rosewood. Noor owns Fairview and is a sitter on Rosewood
// from a week before signedInAt until endsAt. Noor's Fairview membership is
// the older one.
func resolverOnTx(t *testing.T, endsAt *time.Time) (*Resolver, pgx.Tx) {
	t.Helper()

	sessions, tx := sessionsOnTx(t)
	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, sql)
		}
	}
	exec("INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", otherGardenID)
	exec("INSERT INTO app_user (id, display_name, timezone, handle) VALUES ($1, 'Noor', 'Pacific/Auckland', 'noor')", otherUserID)
	exec("INSERT INTO membership (garden_id, user_id, role, created_at, digest_hour) VALUES ($1, $2, 'owner', $3, 8)",
		otherGardenID, otherUserID, signedInAt.AddDate(0, -1, 0))
	exec("INSERT INTO membership (garden_id, user_id, role, invited_by, created_at, expires_at, digest_hour) VALUES ($1, $2, 'sitter', $3, $4, $5, 8)",
		testGardenID, otherUserID, testUserID, signedInAt.AddDate(0, 0, -7), endsAt)

	return NewResolver(sessions, store.New(tx)), tx
}

func signIn(t *testing.T, r *Resolver, userID, gardenID uuid.UUID) string {
	t.Helper()
	token, _, err := r.sessions.Create(t.Context(), signedInAt, userID, gardenID, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return token
}

func TestResolver_AnOwnerResolvesWithEveryCapability(t *testing.T) {
	ctx := t.Context()
	r, tx := resolverOnTx(t, nil)
	token := signIn(t, r, testUserID, testGardenID)

	principal, err := r.Resolve(ctx, signedInAt.Add(time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if principal.User.Handle != "emma" || principal.Garden.Name != "Rosewood" || principal.Membership.Role != "owner" {
		t.Errorf("resolved %s on %s as %s, want emma on Rosewood as owner", principal.User.Handle, principal.Garden.Name, principal.Membership.Role)
	}
	if principal.Session.UserID != testUserID {
		t.Errorf("the session carried is for %v, want %v", principal.Session.UserID, testUserID)
	}

	rows, err := tx.Query(ctx, "SELECT name FROM capability ORDER BY name")
	if err != nil {
		t.Fatalf("reading the capability table: %v", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading the capability table: %v", err)
	}
	for _, name := range names {
		if !principal.Can(Capability(name)) {
			t.Errorf("the owner cannot %s", name)
		}
	}
}

func TestResolver_ASitterResolvesWithOnlyTheSitterRolesCapabilities(t *testing.T) {
	r, _ := resolverOnTx(t, nil)
	token := signIn(t, r, otherUserID, testGardenID)

	principal, err := r.Resolve(t.Context(), signedInAt.Add(time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if principal.Membership.Role != "sitter" {
		t.Fatalf("role = %s, want sitter", principal.Membership.Role)
	}
	for _, c := range []Capability{CareLog, CareEditOwn, CareDeleteOwn} {
		if !principal.Can(c) {
			t.Errorf("the sitter cannot %s", c)
		}
	}
	for _, c := range []Capability{PlantCreate, PlantEdit, ScheduleEdit, CareEditAny, PhotoAdd, MemberInvite, TokenManage} {
		if principal.Can(c) {
			t.Errorf("the sitter can %s", c)
		}
	}
}

func TestResolver_GardenComesFromTheSessionRow(t *testing.T) {
	ctx := t.Context()
	r, tx := resolverOnTx(t, nil)
	token := signIn(t, r, otherUserID, testGardenID)

	principal, err := r.Resolve(ctx, signedInAt.Add(time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if principal.Garden.ID != testGardenID || principal.Membership.Role != "sitter" || principal.Can(PlantCreate) {
		t.Fatalf("resolved to %s as %s, want Rosewood as sitter", principal.Garden.Name, principal.Membership.Role)
	}

	if _, err := tx.Exec(ctx, "UPDATE session SET garden_id = $1 WHERE token_hash = $2", otherGardenID, HashToken(token)); err != nil {
		t.Fatalf("moving the session: %v", err)
	}
	principal, err = r.Resolve(ctx, signedInAt.Add(2*time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve after the move: %v", err)
	}
	if principal.Garden.ID != otherGardenID || principal.Membership.Role != "owner" || !principal.Can(PlantCreate) {
		t.Errorf("resolved to %s as %s, want Fairview as owner", principal.Garden.Name, principal.Membership.Role)
	}
	if principal.User.ID != otherUserID {
		t.Errorf("the user changed to %v", principal.User.ID)
	}
}

func TestResolver_AnEndedMembershipIsRefusedAndTheSessionDeleted(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)
	token := signIn(t, r, otherUserID, testGardenID)

	if _, err := r.Resolve(ctx, endsAt.Add(-time.Second), token); err != nil {
		t.Fatalf("Resolve a second before the end: %v", err)
	}

	_, err := r.Resolve(ctx, endsAt, token)
	var ended *MembershipEndedError
	if !errors.As(err, &ended) {
		t.Fatalf("Resolve at the end = %v, want a *MembershipEndedError", err)
	}
	if ended.User.Handle != "noor" || ended.Garden.Name != "Rosewood" || !ended.EndedAt.Equal(endsAt) {
		t.Errorf("the error names %s on %s ending %s, want noor on Rosewood ending %s", ended.User.Handle, ended.Garden.Name, ended.EndedAt, endsAt)
	}

	var left int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM session WHERE token_hash = $1", HashToken(token)).Scan(&left); err != nil {
		t.Fatalf("counting the row: %v", err)
	}
	if left != 0 {
		t.Error("the session behind the ended membership is still there")
	}
	if _, err := r.Resolve(ctx, endsAt.Add(time.Minute), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("Resolve after the sign-out = %v, want ErrNoSession", err)
	}
}

func TestResolver_AnUnknownTokenIsNoSession(t *testing.T) {
	r, _ := resolverOnTx(t, nil)
	if _, err := r.Resolve(t.Context(), signedInAt, NewSessionToken()); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve = %v, want ErrNoSession", err)
	}
}

func TestResolver_OldestLiveMembershipSkipsAnEndedOne(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)

	// Fairview is the older membership, so it is chosen while both are live.
	got, err := r.OldestLiveMembership(ctx, signedInAt, otherUserID)
	if err != nil {
		t.Fatalf("OldestLiveMembership: %v", err)
	}
	if got.GardenID != otherGardenID {
		t.Errorf("picked garden %v, want Fairview %v", got.GardenID, otherGardenID)
	}

	if _, err := tx.Exec(ctx, "UPDATE membership SET expires_at = $1 WHERE garden_id = $2 AND user_id = $3", signedInAt.AddDate(0, 0, -1), otherGardenID, otherUserID); err != nil {
		t.Fatalf("ending the Fairview membership: %v", err)
	}
	got, err = r.OldestLiveMembership(ctx, signedInAt, otherUserID)
	if err != nil {
		t.Fatalf("OldestLiveMembership with Fairview ended: %v", err)
	}
	if got.GardenID != testGardenID {
		t.Errorf("picked garden %v, want Rosewood %v", got.GardenID, testGardenID)
	}

	if _, err := r.OldestLiveMembership(ctx, endsAt, otherUserID); !errors.Is(err, ErrNoLiveMembership) {
		t.Errorf("OldestLiveMembership with both ended = %v, want ErrNoLiveMembership", err)
	}
}

func TestCapability_TheConstantsMatchTheCapabilityTable(t *testing.T) {
	_, tx := sessionsOnTx(t)

	rows, err := tx.Query(t.Context(), "SELECT name FROM capability ORDER BY name")
	if err != nil {
		t.Fatalf("reading the capability table: %v", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading the capability table: %v", err)
	}

	constants := make([]string, 0, len(allCapabilities))
	for _, c := range allCapabilities {
		constants = append(constants, string(c))
	}
	slices.Sort(constants)
	if !slices.Equal(constants, names) {
		t.Errorf("the constants are %v\nthe table holds %v", constants, names)
	}
}
