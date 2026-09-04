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

// Principal is who a request is from. No URL names a garden, so Garden is the
// only place a handler learns which one the request is on.
type Principal struct {
	Session      store.Session
	User         store.AppUser
	Garden       store.Garden
	Membership   store.Membership
	Capabilities Capabilities
}

// Can reports whether the principal's role holds capability. A template asks
// the same function a handler asks, so the two cannot disagree about what a
// role may do.
func (p Principal) Can(capability Capability) bool {
	return p.Capabilities[capability]
}

// MembershipEnded reports whether m has ended at now. A membership with no
// expires_at is permanent, and one with a date has ended from that instant.
func MembershipEnded(m store.Membership, now time.Time) bool {
	return m.ExpiresAt != nil && !now.Before(*m.ExpiresAt)
}

// MembershipEndedError reports a session whose membership has ended. It
// carries the user and the garden because the page that answers names both.
type MembershipEndedError struct {
	User    store.AppUser
	Garden  store.Garden
	EndedAt time.Time
}

func (e *MembershipEndedError) Error() string {
	return fmt.Sprintf("the membership of %s on garden %s ended at %s", e.User.Handle, e.Garden.ID, e.EndedAt.Format(time.RFC3339))
}

// ErrNoLiveMembership reports a user with no membership that is live at now.
var ErrNoLiveMembership = errors.New("no live membership")

// Resolver turns the token a request presented into the Principal behind it.
type Resolver struct {
	sessions *Sessions
	queries  *store.Queries
}

// NewResolver returns a Resolver over sessions and queries.
func NewResolver(sessions *Sessions, queries *store.Queries) *Resolver {
	return &Resolver{sessions: sessions, queries: queries}
}

// Resolve returns the Principal behind token at now. It reports ErrNoSession
// when the token resolves to no live session, and a *MembershipEndedError when
// the session's membership has an expires_at that has passed, after deleting
// the session behind it. The garden is the one on the session row, so an
// account holding two memberships is on one garden per request.
func (r *Resolver) Resolve(ctx context.Context, now time.Time, token string) (Principal, error) {
	session, err := r.sessions.Lookup(ctx, now, token)
	if err != nil {
		return Principal{}, err
	}

	row, err := r.queries.GetMembershipWithUserAndGarden(ctx, session.GardenID, session.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		// The membership was deleted between the session lookup and this
		// read, and the cascade deleted the session with it.
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

// OldestLiveMembership returns the oldest membership of userID that has not
// ended at now, which is the garden a new session starts on. It reports
// ErrNoLiveMembership when none is live.
func (r *Resolver) OldestLiveMembership(ctx context.Context, now time.Time, userID uuid.UUID) (store.Membership, error) {
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
