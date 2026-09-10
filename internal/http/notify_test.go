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
	got := inviteAcceptedNotification("Rosewood", store.AppUser{DisplayName: "Robin"}, "sitter")

	want := push.Notification{Title: "Rosewood", Body: "Robin joined as a sitter.", URL: PeoplePath}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestRoleChangedNotification_NamesTheGardenAndTheNewRoleAndOpensToday(t *testing.T) {
	got := roleChangedNotification("Rosewood", "sitter")

	want := push.Notification{Title: "Rosewood", Body: "Your role in Rosewood is now sitter.", URL: todayPath}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestMembershipRemovedNotification_NamesTheGardenAndOpensNothing(t *testing.T) {
	got := membershipRemovedNotification("Rosewood")

	want := push.Notification{Title: "Rosewood", Body: "You’ve been removed from Rosewood."}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestStorageNotification_SaysHowMuchIsUsedAndOpensThePlantsPhotos(t *testing.T) {
	usage := photo.Usage{Used: 920_000_000, Quota: 1_000_000_000}

	got := storageNotification("Rosewood", usage, store.Plant{ID: dorisID})

	want := push.Notification{Title: "Rosewood", Body: "920 MB of 1 GB of photo storage used. Delete photos to make room.", URL: photosPath(dorisID)}
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
