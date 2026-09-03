package auth

import (
	"errors"
	"fmt"
	"time"
)

// MaxTokenLifetime is the furthest ahead an API token may be set to expire. It
// is a constant rather than a setting, because a cap that can be raised is not
// a cap.
const MaxTokenLifetime = 90 * 24 * time.Hour

// ErrTokenLifetime reports a requested lifetime that is not positive or reaches
// past MaxTokenLifetime.
var ErrTokenLifetime = errors.New("a token lifetime is positive and at most ninety days")

// TokenExpiry is when a token created at now and given lifetime stops working.
// It is the only way to arrive at api_token.expires_at, so nothing can mint a
// token that outlives the cap.
func TokenExpiry(now time.Time, lifetime time.Duration) (time.Time, error) {
	if lifetime <= 0 || lifetime > MaxTokenLifetime {
		return time.Time{}, fmt.Errorf("%w: got %s", ErrTokenLifetime, lifetime)
	}
	return now.Add(lifetime), nil
}
