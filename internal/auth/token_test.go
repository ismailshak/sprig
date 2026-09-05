package auth

import (
	"errors"
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
