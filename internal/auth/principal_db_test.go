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
	token, _, err := r.sessions.Create(t.Context(), signedInAt, userID, &gardenID, nil, "")
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

func sessionGarden(t *testing.T, tx pgx.Tx, token string) *uuid.UUID {
	t.Helper()
	var id *uuid.UUID
	if err := tx.QueryRow(t.Context(), "SELECT garden_id FROM session WHERE token_hash = $1", HashToken(token)).Scan(&id); err != nil {
		t.Fatalf("reading the session's garden: %v", err)
	}
	return id
}

func TestResolver_AnEndedMembershipMovesTheSessionToTheAccountsOtherLiveGarden(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)
	token := signIn(t, r, otherUserID, testGardenID)

	if _, err := r.Resolve(ctx, endsAt.Add(-time.Second), token); err != nil {
		t.Fatalf("Resolve a second before the end: %v", err)
	}

	// Noor's Rosewood membership has ended and the Fairview one is live.
	principal, err := r.Resolve(ctx, endsAt, token)
	if err != nil {
		t.Fatalf("Resolve at the end: %v", err)
	}

	if !principal.InGarden() || principal.Garden.ID != otherGardenID || principal.Membership.Role != "owner" || !principal.Can(PlantCreate) {
		t.Errorf("resolved to %s as %s, want Fairview as owner", principal.Garden.Name, principal.Membership.Role)
	}
	if got := sessionGarden(t, tx, token); got == nil || *got != otherGardenID {
		t.Errorf("the session row is on %v, want Fairview", got)
	}
}

func endEveryMembership(t *testing.T, tx pgx.Tx, userID uuid.UUID, endsAt time.Time) {
	t.Helper()
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET expires_at = $1 WHERE user_id = $2", endsAt, userID); err != nil {
		t.Fatalf("ending the memberships: %v", err)
	}
}

func TestResolver_AnAccountWithNoLiveMembershipIsResolvedOnNoGarden(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)
	token := signIn(t, r, otherUserID, testGardenID)
	endEveryMembership(t, tx, otherUserID, endsAt)

	principal, err := r.Resolve(ctx, endsAt.Add(time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve with every membership ended: %v", err)
	}

	if principal.InGarden() || principal.Garden.ID != (uuid.UUID{}) || principal.Membership.Role != "" || len(principal.Capabilities) != 0 {
		t.Errorf("resolved to %+v, want an account in no garden with no capabilities", principal)
	}
	if principal.User.Handle != "noor" || principal.Session.UserID != otherUserID {
		t.Errorf("resolved to %s, want noor with her session", principal.User.Handle)
	}
	if got := sessionGarden(t, tx, token); got != nil {
		t.Errorf("the session row is on %v, want no garden", got)
	}
}

func TestResolver_ARenewedMembershipPutsTheSessionBackOnTheGardenWithNoSignIn(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)
	token := signIn(t, r, otherUserID, testGardenID)
	endEveryMembership(t, tx, otherUserID, endsAt)
	if _, err := r.Resolve(ctx, endsAt.Add(time.Minute), token); err != nil {
		t.Fatalf("Resolve with every membership ended: %v", err)
	}

	if _, err := tx.Exec(ctx, "UPDATE membership SET expires_at = NULL WHERE garden_id = $1 AND user_id = $2", otherGardenID, otherUserID); err != nil {
		t.Fatalf("renewing Fairview: %v", err)
	}
	principal, err := r.Resolve(ctx, endsAt.Add(2*time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve after the renewal: %v", err)
	}

	if !principal.InGarden() || principal.Garden.ID != otherGardenID {
		t.Errorf("resolved to %q, want Fairview", principal.Garden.Name)
	}
	if got := sessionGarden(t, tx, token); got == nil || *got != otherGardenID {
		t.Errorf("the session row is on %v, want Fairview", got)
	}
}

func TestResolver_ASessionStartedOnNoGardenIsPutOnTheAccountsOldestLiveGarden(t *testing.T) {
	ctx := t.Context()
	r, tx := resolverOnTx(t, nil)
	token, _, err := r.sessions.Create(ctx, signedInAt, otherUserID, nil, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	principal, err := r.Resolve(ctx, signedInAt.Add(time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Fairview is Noor's oldest membership.
	if !principal.InGarden() || principal.Garden.ID != otherGardenID || principal.Membership.Role != "owner" {
		t.Errorf("resolved to %s as %s, want Fairview as owner", principal.Garden.Name, principal.Membership.Role)
	}
	if got := sessionGarden(t, tx, token); got == nil || *got != otherGardenID {
		t.Errorf("the session row is on %v, want Fairview", got)
	}
}

func TestResolver_AMembershipDeletedUnderASessionTakesTheSessionOffTheGarden(t *testing.T) {
	ctx := t.Context()
	r, tx := resolverOnTx(t, nil)
	token := signIn(t, r, testUserID, testGardenID)
	if _, err := r.Resolve(ctx, signedInAt.Add(time.Minute), token); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM membership WHERE garden_id = $1 AND user_id = $2", testGardenID, testUserID); err != nil {
		t.Fatalf("removing Emma: %v", err)
	}

	if got := sessionGarden(t, tx, token); got != nil {
		t.Errorf("the session row is on %v, want no garden", got)
	}
	principal, err := r.Resolve(ctx, signedInAt.Add(2*time.Minute), token)
	if err != nil {
		t.Fatalf("Resolve after the removal: %v", err)
	}
	if principal.InGarden() || principal.User.Handle != "emma" {
		t.Errorf("resolved to %s on %q, want emma in no garden", principal.User.Handle, principal.Garden.Name)
	}
}

func TestResolver_AnUnknownTokenIsNoSession(t *testing.T) {
	r, _ := resolverOnTx(t, nil)
	if _, err := r.Resolve(t.Context(), signedInAt, NewSessionToken()); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve = %v, want ErrNoSession", err)
	}
}

func TestResolver_StartingMembershipIsTheOldestLiveOne(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)

	// Fairview is the older membership, so it is chosen while both are live.
	got, err := r.startingMembership(ctx, signedInAt, otherUserID)
	if err != nil {
		t.Fatalf("startingMembership: %v", err)
	}
	if got.GardenID != otherGardenID {
		t.Errorf("picked garden %v, want Fairview %v", got.GardenID, otherGardenID)
	}

	if _, err := tx.Exec(ctx, "UPDATE membership SET expires_at = $1 WHERE garden_id = $2 AND user_id = $3", signedInAt.AddDate(0, 0, -1), otherGardenID, otherUserID); err != nil {
		t.Fatalf("ending the Fairview membership: %v", err)
	}
	got, err = r.startingMembership(ctx, signedInAt, otherUserID)
	if err != nil {
		t.Fatalf("startingMembership with Fairview ended: %v", err)
	}
	if got.GardenID != testGardenID {
		t.Errorf("picked garden %v, want Rosewood %v", got.GardenID, testGardenID)
	}

	if _, err := r.startingMembership(ctx, endsAt, otherUserID); !errors.Is(err, ErrNoLiveMembership) {
		t.Errorf("startingMembership with both ended = %v, want ErrNoLiveMembership", err)
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

func TestResolver_StartingMembershipIsTheGardenLastSwitchedToWhileItIsLive(t *testing.T) {
	ctx := t.Context()
	endsAt := signedInAt.AddDate(0, 0, 7)
	r, tx := resolverOnTx(t, &endsAt)
	if _, err := tx.Exec(ctx, "UPDATE app_user SET last_garden_id = $1 WHERE id = $2", testGardenID, otherUserID); err != nil {
		t.Fatalf("recording the last garden: %v", err)
	}

	// Rosewood is the newer membership and the one last switched to.
	got, err := r.startingMembership(ctx, signedInAt, otherUserID)
	if err != nil {
		t.Fatalf("startingMembership: %v", err)
	}
	if got.GardenID != testGardenID {
		t.Errorf("picked garden %v, want Rosewood %v", got.GardenID, testGardenID)
	}

	if _, err := tx.Exec(ctx, "UPDATE membership SET expires_at = $1 WHERE garden_id = $2 AND user_id = $3", signedInAt.AddDate(0, 0, -1), testGardenID, otherUserID); err != nil {
		t.Fatalf("ending the Rosewood membership: %v", err)
	}
	got, err = r.startingMembership(ctx, signedInAt, otherUserID)
	if err != nil {
		t.Fatalf("startingMembership with Rosewood ended: %v", err)
	}
	if got.GardenID != otherGardenID {
		t.Errorf("picked garden %v, want Fairview %v", got.GardenID, otherGardenID)
	}
}
