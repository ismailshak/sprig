package auth

import (
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

func TestMembershipEnded(t *testing.T) {
	now := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	lastDay := now
	tomorrow := now.Add(24 * time.Hour)
	yesterday := now.Add(-24 * time.Hour)

	cases := []struct {
		name      string
		expiresAt *time.Time
		want      bool
	}{
		{"no expiry is permanent", nil, false},
		{"a day away is live", &tomorrow, false},
		{"the instant itself has ended", &lastDay, true},
		{"a day ago has ended", &yesterday, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MembershipEnded(store.Membership{ExpiresAt: c.expiresAt}, now); got != c.want {
				t.Errorf("MembershipEnded = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPrincipal_CanAsksTheSetAndNothingElse(t *testing.T) {
	sitter := Principal{Membership: store.Membership{Role: "owner"}, Capabilities: NewCapabilities([]string{"care.log"})}
	if !sitter.Can(CareLog) {
		t.Error("a principal holding care.log cannot log care")
	}
	// The row says owner and the set lacks plant.create, and the set wins.
	if sitter.Can(PlantCreate) {
		t.Error("a principal whose set lacks plant.create can create a plant")
	}
	if (Principal{}).Can(CareLog) {
		t.Error("a principal with no set can log care")
	}
}
