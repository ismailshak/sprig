package auth

import (
	"errors"
	"fmt"
	"time"
)

// MaxTokenLifetime is the longest an API token may be valid for. It is a
// constant rather than a setting, because a cap that can be raised is not a
// cap.
const MaxTokenLifetime = 90 * 24 * time.Hour

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
