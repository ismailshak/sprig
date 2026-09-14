package http

import (
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// ownedGarden is a garden, the account that owns it and the garden's care
// types. Most handler fixtures insert these rows before their own.
type ownedGarden struct {
	id    uuid.UUID
	name  string
	owner store.AppUser
	// membershipID is the id of the owner's membership. A zero id lets the
	// column generate one.
	membershipID uuid.UUID
	// careTypes are inserted with their ID, Name and Slug. A zero ID lets the
	// column generate one.
	careTypes []store.CareType
}

// insert writes the garden, the owner's account, the owner's membership with
// the digest at 08:00, and the care types.
func (g ownedGarden) insert(t *testing.T, tx pgx.Tx) {
	t.Helper()

	mustExec(t, tx, "INSERT INTO garden (id, name) VALUES ($1, $2)", g.id, g.name)
	mustExec(t, tx, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, $2, $3, $4)",
		g.owner.ID, g.owner.DisplayName, g.owner.Handle, g.owner.Timezone)
	mustExec(t, tx, "INSERT INTO membership (id, garden_id, user_id, role, digest_hour) VALUES (coalesce($1, uuidv7()), $2, $3, 'owner', 8)",
		generatedIfZero(g.membershipID), g.id, g.owner.ID)
	for _, ct := range g.careTypes {
		mustExec(t, tx, "INSERT INTO care_type (id, garden_id, name, slug) VALUES (coalesce($1, uuidv7()), $2, $3, $4)",
			generatedIfZero(ct.ID), g.id, ct.Name, ct.Slug)
	}
}

// principal is the owner signed in to the garden with the capabilities given.
// It panics when membershipID is zero, because the id the column generated is
// not known here.
func (g ownedGarden) principal(capabilities ...auth.Capability) auth.Principal {
	if g.membershipID == (uuid.UUID{}) {
		panic("ownedGarden.principal needs membershipID set")
	}
	granted := auth.Capabilities{}
	for _, c := range capabilities {
		granted[c] = true
	}
	return auth.Principal{
		User:         g.owner,
		Garden:       store.Garden{ID: g.id, Name: g.name},
		Membership:   store.Membership{ID: g.membershipID, GardenID: g.id, UserID: g.owner.ID, Role: "owner", DigestHour: 8},
		Capabilities: granted,
	}
}

// generatedIfZero returns nil for the zero id. The insert then writes NULL and
// coalesce generates an id.
func generatedIfZero(id uuid.UUID) *uuid.UUID {
	if id == (uuid.UUID{}) {
		return nil
	}
	return &id
}

// mustExec fails the test when the statement returns an error.
func mustExec(t *testing.T, tx pgx.Tx, sql string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("seeding: %v\n%s", err, sql)
	}
}
