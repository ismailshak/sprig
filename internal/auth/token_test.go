package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenExpiry(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)

	// The four lifetimes the create form offers, then four it must refuse.
	cases := []struct {
		name     string
		lifetime time.Duration
		want     time.Time
	}{
		{"a week", 7 * 24 * time.Hour, now.AddDate(0, 0, 7)},
		{"thirty days, the default", 30 * 24 * time.Hour, now.AddDate(0, 0, 30)},
		{"sixty days", 60 * 24 * time.Hour, now.AddDate(0, 0, 60)},
		{"ninety days, the last option", MaxTokenLifetime, now.AddDate(0, 0, 90)},
		{"a second past ninety days", MaxTokenLifetime + time.Second, time.Time{}},
		{"a year", 365 * 24 * time.Hour, time.Time{}},
		{"no lifetime at all", 0, time.Time{}},
		{"a lifetime in the past", -time.Hour, time.Time{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := TokenExpiry(now, c.lifetime)
			if c.want.IsZero() {
				if !errors.Is(err, ErrTokenLifetime) {
					t.Fatalf("TokenExpiry(now, %s) error = %v, want ErrTokenLifetime", c.lifetime, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("TokenExpiry(now, %s): %v", c.lifetime, err)
			}
			if !got.Equal(c.want) {
				t.Errorf("TokenExpiry(now, %s) = %s, want %s", c.lifetime, got, c.want)
			}
		})
	}
}

func TestNewAPIToken_TheTokenStartsWithThePrefixTheListWillShow(t *testing.T) {
	token, prefix := NewAPIToken()

	if !strings.HasPrefix(token, prefix+"_") {
		t.Errorf("the token %q does not start with the prefix %q, so a row cannot be matched to the device holding it", token, prefix)
	}
	if len(token) <= len(prefix)+1 {
		t.Errorf("the token %q is its prefix and nothing else, and the prefix is stored in the clear", token)
	}
}

func TestNewAPIToken_TwoTokensDiffer(t *testing.T) {
	first, firstPrefix := NewAPIToken()
	second, secondPrefix := NewAPIToken()

	if first == second {
		t.Error("two tokens are the same value")
	}
	if firstPrefix == secondPrefix {
		t.Error("two tokens carry the same prefix, and the prefix is what tells two rows apart")
	}
}
