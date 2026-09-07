package http

import (
	"context"
	"fmt"
	"time"

	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// digestHourDefault is the hour a new membership's digest is sent, read in the
// member's timezone. The column has no default, so every membership the app
// creates is given this hour here.
const digestHourDefault = 8

// newMembership is what a caller of createMembership chooses. The digest hour
// and the two notification preferences are not in it, because they are the
// same for every new membership.
type newMembership struct {
	GardenID uuid.UUID
	UserID   uuid.UUID
	Role     string
	// InvitedBy is nil for the person who created the garden.
	InvitedBy *uuid.UUID
	// ExpiresAt is nil for a membership with no end date.
	ExpiresAt *time.Time
}

// createMembership writes the membership and one notification_preference row
// per kind. A new membership starts with the daily digest on and the
// notification about somebody else's care off. Neither row sends anything on
// its own, because a push also needs a subscription the browser has granted.
//
// The rows are written through q, so a caller that needs them in the same
// transaction as the user or the garden passes a Queries bound to it.
func createMembership(ctx context.Context, q *store.Queries, m newMembership) (store.Membership, error) {
	membership, err := q.CreateMembership(ctx, store.CreateMembershipParams{
		GardenID:   m.GardenID,
		UserID:     m.UserID,
		Role:       m.Role,
		InvitedBy:  m.InvitedBy,
		ExpiresAt:  m.ExpiresAt,
		DigestHour: digestHourDefault,
	})
	if err != nil {
		return store.Membership{}, fmt.Errorf("writing the membership: %w", err)
	}
	starts := []struct {
		kind    string
		enabled bool
	}{{digestKind, true}, {activityKind, false}}
	for _, s := range starts {
		err := q.SetNotificationPreference(ctx, store.SetNotificationPreferenceParams{
			MembershipID: membership.ID,
			Kind:         s.kind,
			Enabled:      s.enabled,
		})
		if err != nil {
			return store.Membership{}, fmt.Errorf("writing the %s preference: %w", s.kind, err)
		}
	}
	return membership, nil
}

// renewMembership sets the role, the inviter and the end date on the
// membership row for the garden and account m names. The row keeps its id and
// its digest hour. Notification preferences are rows against the membership
// id, so they survive as well.
func renewMembership(ctx context.Context, q *store.Queries, m newMembership) (store.Membership, error) {
	membership, err := q.RenewMembership(ctx, store.RenewMembershipParams{
		GardenID:  m.GardenID,
		UserID:    m.UserID,
		Role:      m.Role,
		InvitedBy: m.InvitedBy,
		ExpiresAt: m.ExpiresAt,
	})
	if err != nil {
		return store.Membership{}, fmt.Errorf("renewing the membership: %w", err)
	}
	return membership, nil
}
