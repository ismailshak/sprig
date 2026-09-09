package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// ErrUnknownHandle is returned by IssueSignInLink for a handle no open
// account holds.
var ErrUnknownHandle = errors.New("no account has that handle")

// SignInLink is a sign-in link IssueSignInLink made: the token in the link,
// the invite row holding its hash, and the account it is for.
type SignInLink struct {
	Token  string
	Invite store.Invite
	User   store.AppUser
}

// IssueSignInLink creates a sign-in link for the account with handle and
// returns the token that opens it. Opening the link adds a passkey to that
// account and creates no account and no membership.
//
// The invite is issued on the account's first live membership. It returns
// ErrNoLiveMembership when the account has none, because a link naming a
// garden the person has left is refused when it is opened. created_by is the
// account itself, because no other account created it. Any unredeemed
// sign-in link for that account and garden is deleted first, so only one link
// works at a time.
func IssueSignInLink(ctx context.Context, q *store.Queries, now time.Time, handle string) (SignInLink, error) {
	user, err := q.GetUserByHandle(ctx, handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return SignInLink{}, ErrUnknownHandle
	}
	if err != nil {
		return SignInLink{}, fmt.Errorf("read the account: %w", err)
	}
	membership, err := firstLiveMembership(ctx, q, now, user.ID)
	if err != nil {
		return SignInLink{}, err
	}

	token := NewInviteToken()
	var invite store.Invite
	err = q.InTx(ctx, func(q *store.Queries) error {
		if _, err := q.DeleteReenrolmentInvites(ctx, membership.GardenID, &user.ID); err != nil {
			return err
		}
		var err error
		invite, err = q.CreateInvite(ctx, store.CreateInviteParams{
			GardenID:  membership.GardenID,
			TokenHash: HashToken(token),
			Role:      membership.Role,
			UserID:    &user.ID,
			CreatedBy: user.ID,
			ExpiresAt: now.Add(InviteLifetime),
		})
		return err
	})
	if err != nil {
		return SignInLink{}, fmt.Errorf("create the invite: %w", err)
	}
	return SignInLink{Token: token, Invite: invite, User: user}, nil
}
