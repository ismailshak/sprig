package http

import (
	"context"
	"testing"

	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/store"
)

// userNotified is one call a handler made to the hook that notifies one
// person.
type userNotified struct {
	user store.AppUser
	n    push.Notification
}

// captureUserNotifications replaces hook with one that records each call in
// the returned slice.
func captureUserNotifications(hook *notifyUser) *[]userNotified {
	var got []userNotified
	*hook = func(_ context.Context, user store.AppUser, n push.Notification) {
		got = append(got, userNotified{user: user, n: n})
	}
	return &got
}

func TestInviteAcceptedNotification_NamesWhoJoinedAndAsWhatAndOpensPeople(t *testing.T) {
	got := inviteAcceptedNotification("Ellie’s Rosewood", store.AppUser{DisplayName: "Robin"}, "sitter")

	want := push.Notification{Title: "Invite accepted", Body: "Robin joined Ellie’s Rosewood as a sitter.", URL: PeoplePath}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestRoleChangedNotification_NamesTheGardenAndTheNewRoleAndOpensToday(t *testing.T) {
	got := roleChangedNotification("Ellie’s Rosewood", "sitter")

	want := push.Notification{Title: "Role changed", Body: "You’re now a sitter in Ellie’s Rosewood.", URL: todayPath}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestMembershipRemovedNotification_NamesTheGardenAndOpensNothing(t *testing.T) {
	got := membershipRemovedNotification("Ellie’s Rosewood")

	want := push.Notification{Title: "Removed from a garden", Body: "You’ve been removed from Ellie’s Rosewood."}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestStorageNotification_SaysHowMuchIsUsedAndOpensThePlantsPhotos(t *testing.T) {
	usage := photo.Usage{Used: 920_000_000, Quota: 1_000_000_000}

	got := storageNotification("Rosewood", usage, store.Plant{ID: dorisID})

	want := push.Notification{Title: "Photo storage nearly full", Body: "920 MB of 1 GB used in Rosewood. Delete photos to make room.", URL: photosPath(dorisID)}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestNearlyFullReached_IsTrueFromNinetyPercentOfTheQuotaUp(t *testing.T) {
	if nearlyFullReached(photo.Usage{Used: 899, Quota: 1000}) {
		t.Error("899 of 1000 counts as nearly full")
	}
	if !nearlyFullReached(photo.Usage{Used: 900, Quota: 1000}) {
		t.Error("900 of 1000 does not count as nearly full")
	}
}

func TestStorageKey_ChangesWhenTheQuotaDoes(t *testing.T) {
	before := storageKey(photo.Usage{Quota: 1_000_000_000})
	after := storageKey(photo.Usage{Quota: 4_000_000_000})

	if before == after {
		t.Errorf("the key is %q under both quotas, so a raised quota would never be sent for", before)
	}
}
