package http

import (
	"testing"
	"time"

	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// notificationSettings is the enabled flag of a membership's two
// notification_preference rows, with the digest hour off the membership.
type notificationSettings struct {
	digest   bool
	activity bool
	hour     int16
}

func settingsOf(t *testing.T, f *moreFixture, membershipID uuid.UUID) notificationSettings {
	t.Helper()
	var settings notificationSettings
	err := f.tx.QueryRow(t.Context(), `
		SELECT d.enabled, a.enabled, m.digest_hour
		FROM membership m
		JOIN notification_preference d ON d.membership_id = m.id AND d.kind = 'digest'
		JOIN notification_preference a ON a.membership_id = m.id AND a.kind = 'activity'
		WHERE m.id = $1`, membershipID).Scan(&settings.digest, &settings.activity, &settings.hour)
	if err != nil {
		t.Fatalf("reading the membership's notification settings: %v", err)
	}
	return settings
}

func TestCreateMembership_StartsWithTheDigestOnAtEightAndTheActivityNotificationOff(t *testing.T) {
	f := moreGarden(t)
	queries := store.New(f.tx)

	membership, err := createMembership(t.Context(), queries, newMembership{
		GardenID: otherGardenID, UserID: moreUserID, Role: "sitter", InvitedBy: &otherUserID,
	})
	if err != nil {
		t.Fatalf("creating the membership: %v", err)
	}

	if got, want := settingsOf(t, f, membership.ID), (notificationSettings{digest: true, activity: false, hour: 8}); got != want {
		t.Errorf("the membership starts as %+v, want %+v", got, want)
	}
}

func TestCreateMembership_AMembershipGivenNoEndDateIsPermanent(t *testing.T) {
	f := moreGarden(t)
	queries := store.New(f.tx)

	membership, err := createMembership(t.Context(), queries, newMembership{
		GardenID: otherGardenID, UserID: moreUserID, Role: "sitter", InvitedBy: &otherUserID,
	})
	if err != nil {
		t.Fatalf("creating the membership: %v", err)
	}
	if membership.ExpiresAt != nil {
		t.Errorf("the membership ends at %v, want no end date", membership.ExpiresAt)
	}
}

func TestCreateMembership_KeepsTheEndDateItWasGiven(t *testing.T) {
	f := moreGarden(t)
	queries := store.New(f.tx)
	ends := time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC)

	membership, err := createMembership(t.Context(), queries, newMembership{
		GardenID: otherGardenID, UserID: moreUserID, Role: "sitter", InvitedBy: &otherUserID, ExpiresAt: &ends,
	})
	if err != nil {
		t.Fatalf("creating the membership: %v", err)
	}
	if membership.ExpiresAt == nil || !membership.ExpiresAt.Equal(ends) {
		t.Errorf("the membership ends at %v, want %v", membership.ExpiresAt, ends)
	}
}
