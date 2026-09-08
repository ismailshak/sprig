package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// MaxTokenLifetime is the longest an API token may be valid for. It is a
// constant rather than a setting, because a cap that can be raised is not a
// cap.
const MaxTokenLifetime = 90 * 24 * time.Hour

// InviteLifetime is how long an invite link works for. A link that adds a
// device to an account already in the garden gets the same lifetime, because
// both are a single-use secret sent in a message and opened later.
const InviteLifetime = 7 * 24 * time.Hour

// ErrTokenLifetime is returned for a lifetime that is not positive or is longer
// than MaxTokenLifetime.
var ErrTokenLifetime = errors.New("a token lifetime is positive and at most ninety days")

// TokenExpiry returns when a token created at now with lifetime expires. Every
// api_token.expires_at is computed here, so no token can outlive the cap.
func TokenExpiry(now time.Time, lifetime time.Duration) (time.Time, error) {
	if lifetime <= 0 || lifetime > MaxTokenLifetime {
		return time.Time{}, fmt.Errorf("%w: got %s", ErrTokenLifetime, lifetime)
	}
	return now.Add(lifetime), nil
}

// apiTokenScheme starts every API token, so a value pasted into the wrong box
// is recognisable as one of sprig's.
const apiTokenScheme = "sprg"

const (
	// apiTokenLabelLength is how many characters follow the scheme in the
	// prefix. The prefix is stored in the clear and shown on the Tokens list,
	// so two rows with the same name can be told apart.
	apiTokenLabelLength = 4
	// apiTokenSecretLength is how many hexadecimal characters follow the
	// prefix, 128 bits of randomness.
	apiTokenSecretLength = 32
)

// NewAPIToken returns a token for a machine to send as a bearer token, and the
// prefix to store beside its hash. The prefix is the first two parts of the
// token, so a person reading a row can match it against what is on the device.
func NewAPIToken() (token, prefix string) {
	prefix = apiTokenScheme + "_" + randomHex(apiTokenLabelLength)
	return prefix + "_" + randomHex(apiTokenSecretLength), prefix
}

// NewInviteToken returns the token in an invite link. It is base64url like a
// session token, since it travels in a URL.
func NewInviteToken() string {
	return NewSessionToken()
}

// randomHex returns n hexadecimal characters of randomness.
func randomHex(n int) string {
	raw := make([]byte, (n+1)/2)
	// crypto/rand.Read never returns an error. It stops the process instead if
	// the system source of randomness fails.
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)[:n]
}

// ErrNoAPIToken is returned for a bearer token with no live row. A token that
// was never issued, one revoked and one past its expiry all get this error, so
// a caller cannot tell which tokens were once real.
var ErrNoAPIToken = errors.New("no live token")

// APITokenLive reports whether token still works at now. A revoked token is
// never live, whatever now is. Any other is live strictly before its
// expires_at.
func APITokenLive(token store.APIToken, now time.Time) bool {
	return token.RevokedAt == nil && now.Before(token.ExpiresAt)
}

// APITokens looks up the Principal for a bearer token.
type APITokens struct {
	queries *store.Queries
}

// NewAPITokens returns APITokens over queries.
func NewAPITokens(queries *store.Queries) *APITokens {
	return &APITokens{queries: queries}
}

// Resolve returns the Principal for a bearer token at now and sets the row's
// last_used_at to now. It returns ErrNoAPIToken when the token matches no row,
// or matches one that is revoked or expired.
//
// The Principal holds the token's garden and the token row and nothing else.
// It has no capabilities, so a route that checks one refuses the request.
func (t *APITokens) Resolve(ctx context.Context, now time.Time, token string) (Principal, error) {
	row, err := t.queries.GetAPITokenByHash(ctx, HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNoAPIToken
	}
	if err != nil {
		return Principal{}, fmt.Errorf("read the token: %w", err)
	}
	if !APITokenLive(row, now) {
		return Principal{}, ErrNoAPIToken
	}
	if err := t.queries.TouchAPIToken(ctx, now, row.TokenHash); err != nil {
		return Principal{}, fmt.Errorf("record the token's use: %w", err)
	}
	garden, err := t.queries.GetGarden(ctx, row.GardenID)
	if err != nil {
		return Principal{}, fmt.Errorf("read the token's garden: %w", err)
	}
	return Principal{Garden: garden, APIToken: &row}, nil
}
