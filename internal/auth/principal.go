package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// Principal is the signed-in user behind a request. No URL names a garden, so
// Garden is the only place a handler learns which one the request is for.
type Principal struct {
	Session      store.Session
	User         store.AppUser
	Garden       store.Garden
	Membership   store.Membership
	Capabilities Capabilities
}

// Can reports whether the principal's role has capability. Templates and
// handlers both call this, so the two cannot disagree about what a role may do.
func (p Principal) Can(capability Capability) bool {
	return p.Capabilities[capability]
}

// MembershipEnded reports whether m has ended at now. A membership with no
// expires_at is permanent. One with a date has ended from that instant on.
func MembershipEnded(m store.Membership, now time.Time) bool {
	return m.ExpiresAt != nil && !now.Before(*m.ExpiresAt)
}

// MembershipEndedError is returned when a session's membership has ended. It
// holds the user and the garden because the page shown for it names both.
type MembershipEndedError struct {
	User    store.AppUser
	Garden  store.Garden
	EndedAt time.Time
}

func (e *MembershipEndedError) Error() string {
	return fmt.Sprintf("the membership of %s on garden %s ended at %s", e.User.Handle, e.Garden.ID, e.EndedAt.Format(time.RFC3339))
}

// ErrNoLiveMembership is returned for a user with no membership live at now.
var ErrNoLiveMembership = errors.New("no live membership")

// Resolver looks up the Principal for a session token.
type Resolver struct {
	sessions *Sessions
	queries  *store.Queries
}

// NewResolver returns a Resolver over sessions and queries.
func NewResolver(sessions *Sessions, queries *store.Queries) *Resolver {
	return &Resolver{sessions: sessions, queries: queries}
}

// Resolve returns the Principal for token at now. It returns ErrNoSession when
// the token has no live session, and a *MembershipEndedError when the session's
// membership has expired, after deleting that session. The garden comes from
// the session row, so an account with two memberships is on one garden per
// session.
func (r *Resolver) Resolve(ctx context.Context, now time.Time, token string) (Principal, error) {
	session, err := r.sessions.Lookup(ctx, now, token)
	if err != nil {
		return Principal{}, err
	}

	row, err := r.queries.GetMembershipWithUserAndGarden(ctx, session.GardenID, session.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		// The membership was deleted between the session lookup and this
		// read, and the cascade took the session with it.
		return Principal{}, ErrNoSession
	}
	if err != nil {
		return Principal{}, fmt.Errorf("read the membership: %w", err)
	}

	if MembershipEnded(row.Membership, now) {
		if err := r.sessions.Delete(ctx, token); err != nil {
			return Principal{}, err
		}
		return Principal{}, &MembershipEndedError{User: row.AppUser, Garden: row.Garden, EndedAt: *row.Membership.ExpiresAt}
	}

	names, err := r.queries.ListRoleCapabilities(ctx, row.Membership.Role)
	if err != nil {
		return Principal{}, fmt.Errorf("read the role's capabilities: %w", err)
	}

	return Principal{
		Session:      session,
		User:         row.AppUser,
		Garden:       row.Garden,
		Membership:   row.Membership,
		Capabilities: NewCapabilities(names),
	}, nil
}

// StartingMembership returns the membership a new session for userID starts
// on: the garden the person last switched to while that membership is live at
// now, and otherwise their oldest membership that is. It returns
// ErrNoLiveMembership when none is live.
func (r *Resolver) StartingMembership(ctx context.Context, now time.Time, userID uuid.UUID) (store.Membership, error) {
	memberships, err := r.queries.ListMembershipsForUser(ctx, userID)
	if err != nil {
		return store.Membership{}, fmt.Errorf("read the memberships: %w", err)
	}
	for _, m := range memberships {
		if !MembershipEnded(m, now) {
			return m, nil
		}
	}
	return store.Membership{}, ErrNoLiveMembership
}
