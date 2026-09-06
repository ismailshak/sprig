package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
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
