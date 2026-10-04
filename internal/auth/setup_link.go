package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

// SetupLink is a setup link IssueSetupLink made: the token in the link and the
// row holding its hash.
type SetupLink struct {
	Token string
	Link  store.SetupLink
}

// IssueSetupLink creates a setup link and returns the token that opens it.
// The link sets up one garden even with sign-up off. It expires after
// InviteLifetime, the same as a sign-in link.
func IssueSetupLink(ctx context.Context, q *store.Queries, now time.Time) (SetupLink, error) {
	token := NewInviteToken()
	link, err := q.CreateSetupLink(ctx, HashToken(token), now.Add(InviteLifetime))
	if err != nil {
		return SetupLink{}, fmt.Errorf("create the setup link: %w", err)
	}
	return SetupLink{Token: token, Link: link}, nil
}
