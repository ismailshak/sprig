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

// Principal is who is behind a request: the signed-in user, or the device
// holding a bearer token. No URL names a garden, so Garden is the only place a
// handler learns which one the request is for. Garden, Membership and
// Capabilities are zero when the account is in no garden. A route that needs a
// garden never runs for such a principal, because the mux renders the "You're
// in no garden" page in its place.
//
// A bearer token resolves to Garden and APIToken alone. Session, User,
// Membership and Capabilities stay zero. Can is therefore false for every
// capability, so a token can only read.
type Principal struct {
	Session      store.Session
	User         store.AppUser
	Garden       store.Garden
	Membership   store.Membership
	Capabilities Capabilities
	// APIToken is nil unless the request presented a bearer token.
	APIToken *store.APIToken
	// SessionTouched is true when resolving the request moved the session's
	// deadline forward.
	SessionTouched bool
}

// Can reports whether the principal's role has capability. Templates and
// handlers both call this, so the two cannot disagree about what a role may do.
func (p Principal) Can(capability Capability) bool {
	return p.Capabilities[capability]
}

// InGarden reports whether the request has a garden. It is false for an
// account with no live membership.
func (p Principal) InGarden() bool {
	return p.Garden.ID != uuid.UUID{}
}

// MembershipEnded reports whether m has ended at now. A membership with no
// expires_at is permanent. One with a date has ended from that instant on.
func MembershipEnded(m store.Membership, now time.Time) bool {
	return m.ExpiresAt != nil && !now.Before(*m.ExpiresAt)
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
// the token has no live session.
//
// Every request re-checks the garden the session row names. A session keeps
// that garden while the account's membership of it is live. Any other session,
// including one that started with no garden at all, is moved to a live
// membership of the account, or has its garden set to NULL when the account
// has none live. An account with no live membership stays signed in, so it can
// set up a garden of its own or accept an invite.
func (r *Resolver) Resolve(ctx context.Context, now time.Time, token string) (Principal, error) {
	session, touched, err := r.sessions.Lookup(ctx, now, token)
	if err != nil {
		return Principal{}, err
	}

	if session.GardenID != nil {
		row, err := r.queries.GetMembershipWithUserAndGarden(ctx, *session.GardenID, session.UserID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, fmt.Errorf("read the membership: %w", err)
		}
		if err == nil && !MembershipEnded(row.Membership, now) {
			return principalOf(session, touched, row), nil
		}
	}

	membership, err := r.startingMembership(ctx, now, session.UserID)
	if errors.Is(err, ErrNoLiveMembership) {
		if session.GardenID != nil {
			if _, err := r.queries.SetSessionGarden(ctx, nil, session.TokenHash); err != nil {
				return Principal{}, fmt.Errorf("take the session off its garden: %w", err)
			}
			session.GardenID = nil
		}
		user, err := r.queries.GetUser(ctx, session.UserID)
		if err != nil {
			return Principal{}, fmt.Errorf("read the account: %w", err)
		}
		return Principal{Session: session, User: user, SessionTouched: touched}, nil
	}
	if err != nil {
		return Principal{}, err
	}
	if _, err := r.queries.SetSessionGarden(ctx, &membership.GardenID, session.TokenHash); err != nil {
		return Principal{}, fmt.Errorf("move the session onto the garden: %w", err)
	}
	session.GardenID = &membership.GardenID
	row, err := r.queries.GetMembershipWithUserAndGarden(ctx, membership.GardenID, session.UserID)
	if err != nil {
		return Principal{}, fmt.Errorf("read the membership: %w", err)
	}
	return principalOf(session, touched, row), nil
}

func principalOf(session store.Session, touched bool, row store.GetMembershipWithUserAndGardenRow) Principal {
	return Principal{
		Session:        session,
		User:           row.AppUser,
		Garden:         row.Garden,
		Membership:     row.Membership,
		Capabilities:   NewCapabilities(row.Capabilities),
		SessionTouched: touched,
	}
}

// startingMembership returns the membership Resolve moves a session to.
func (r *Resolver) startingMembership(ctx context.Context, now time.Time, userID uuid.UUID) (store.Membership, error) {
	return firstLiveMembership(ctx, r.queries, now, userID)
}

// firstLiveMembership returns the account's membership of the garden it last
// switched to, or its oldest live membership when that one has ended.
// ListMembershipsForUser returns the rows in that order. It returns
// ErrNoLiveMembership when every membership has ended at now.
func firstLiveMembership(ctx context.Context, queries *store.Queries, now time.Time, userID uuid.UUID) (store.Membership, error) {
	memberships, err := queries.ListMembershipsForUser(ctx, userID)
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
