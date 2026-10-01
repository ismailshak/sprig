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

func TestStorageNotification_IsSentAndClearedAfterTheClientClosesTheConnection(t *testing.T) {
	f := plantFormOn(t)
	got := captureUserNotifications(&f.handler.notify)
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "DELETE FROM photo WHERE garden_id = $1", rosewoodID)
	givePhoto(t, f.tx, bigFellaID, readerID, thursday)
	usage, err := f.handler.photos.Usage(t.Context(), store.New(f.tx), rosewoodID)
	if err != nil {
		t.Fatal(err)
	}
	f.photosWithRoomFor(t, int(usage.Used))
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	f.handler.notifyStorage(cancelled, f.principal, plant)
	if len(*got) != 1 {
		t.Fatalf("notified %+v, want Ellie once with the garden full", *got)
	}

	f.exec(t, "DELETE FROM photo WHERE garden_id = $1", rosewoodID)
	f.handler.clearStorageNotification(cancelled, f.principal)
	givePhoto(t, f.tx, bigFellaID, readerID, thursday)
	f.handler.notifyStorage(t.Context(), f.principal, plant)
	if len(*got) != 2 {
		t.Errorf("notified %+v, want Ellie a second time after the garden was emptied and filled again", *got)
	}
}
