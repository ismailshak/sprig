package push

import (
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

// newActivity returns an Activity over db that sends to service. Its clock is
// fixed at noon UTC on 3 September 2026.
func newActivity(t *testing.T, db store.DBTX, service *pushService) *Activity {
	t.Helper()

	activity := NewActivity(slog.New(slog.DiscardHandler), store.New(db), NewSender(testKeys(t), service.Client()), "https://sprig.example.com/")
	activity.now = fixed(utc(2026, time.September, 3, 12, 0, 0))
	return activity
}

func turnActivityOn(t *testing.T, db store.DBTX, memberships ...uuid.UUID) {
	t.Helper()

	_, err := db.Exec(t.Context(), "UPDATE notification_preference SET enabled = true WHERE kind = 'activity' AND membership_id = ANY($1)", memberships)
	if err != nil {
		t.Fatalf("turning activity on: %v", err)
	}
}

var wateredDoris = Notification{Title: "Rosewood", Body: "Ellie watered Doris.", URL: "/plants/doris"}

// lastSentAt returns last_sent_at for the browser subscribed at path, and
// false when it has never been sent anything.
func lastSentAt(t *testing.T, db store.DBTX, service *pushService, path string) (time.Time, bool) {
	t.Helper()

	var sent *time.Time
	if err := db.QueryRow(t.Context(), "SELECT last_sent_at FROM push_subscription WHERE endpoint = $1", service.URL+path).Scan(&sent); err != nil {
		t.Fatalf("reading last_sent_at for %s: %v", path, err)
	}
	if sent == nil {
		return time.Time{}, false
	}
	return *sent, true
}

func TestActivity_SendsToEveryMemberWithActivityNotificationsOnExceptTheOneWhoLoggedTheCare(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	// Ellie, Sam and Robin all have it on. Ellie is the one logging, and Robin
	// has no browser subscribed.
	turnActivityOn(t, tx, ellieMembershipID, samMembershipID, robinMembershipID)
	activity := newActivity(t, tx, service)

	activity.Send(t.Context(), rosewoodID, ellieID, wateredDoris)
	activity.Wait()

	if got := service.received(); !slices.Equal(got, []string{"/sam-phone"}) {
		t.Errorf("the push service received %q, want Sam's phone alone", got)
	}
	if sent, ok := lastSentAt(t, tx, service, "/sam-phone"); !ok || !sent.Equal(activity.now()) {
		t.Errorf("Sam's phone has last_sent_at %v %v, want the time of the send", sent, ok)
	}
	if _, ok := lastSentAt(t, tx, service, "/ellie-phone"); ok {
		t.Error("Ellie's phone has a last_sent_at, want none: she logged the care")
	}
}

func TestActivity_ReachesBothBrowsersAMemberHasSubscribed(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	// Ellie has two browsers subscribed and Sam is the one logging.
	turnActivityOn(t, tx, ellieMembershipID)
	activity := newActivity(t, tx, service)

	activity.Send(t.Context(), rosewoodID, samID, Notification{Title: "Rosewood", Body: "Sam watered Doris.", URL: "/plants/doris"})
	activity.Wait()

	if got, want := service.received(), []string{"/ellie-mac", "/ellie-phone"}; !slices.Equal(got, want) {
		t.Errorf("the push service received %q, want %q", got, want)
	}
}

func TestActivity_SendsNothingToAMemberWithActivityNotificationsOff(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	activity := newActivity(t, tx, service)

	activity.Send(t.Context(), rosewoodID, ellieID, wateredDoris)
	activity.Wait()

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing: Sam has activity off", got)
	}
}

func TestActivity_SendsNothingToAMemberWhoseAccessHasEnded(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	turnActivityOn(t, tx, samMembershipID)
	activity := newActivity(t, tx, service)
	ended := activity.now().Add(-time.Hour)
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET expires_at = $1 WHERE id = $2", ended, samMembershipID); err != nil {
		t.Fatal(err)
	}

	activity.Send(t.Context(), rosewoodID, ellieID, wateredDoris)
	activity.Wait()

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing: Sam's access ended an hour ago", got)
	}
}

func TestActivity_SendsNothingToMembersOfAnotherGarden(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	seedFairview(t, tx)
	turnActivityOn(t, tx, samMembershipID)
	activity := newActivity(t, tx, service)

	activity.Send(t.Context(), fairviewID, ellieID, Notification{Title: "Fairview", Body: "Ellie watered Fern.", URL: "/plants/fern"})
	activity.Wait()

	if got := service.received(); len(got) != 0 {
		t.Errorf("the push service received %q, want nothing: Sam is not in Fairview", got)
	}
}

func TestActivity_DeletesABrowserThePushServiceReportsGone(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	service.statuses["/sam-phone"] = http.StatusGone
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	turnActivityOn(t, tx, samMembershipID)
	activity := newActivity(t, tx, service)

	activity.Send(t.Context(), rosewoodID, ellieID, wateredDoris)
	activity.Wait()

	var remaining []string
	rows, err := tx.Query(t.Context(), "SELECT endpoint FROM push_subscription ORDER BY endpoint")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var endpoint string
		if err := rows.Scan(&endpoint); err != nil {
			t.Fatal(err)
		}
		remaining = append(remaining, endpoint)
	}
	want := []string{service.URL + "/ellie-mac", service.URL + "/ellie-phone"}
	if !slices.Equal(remaining, want) {
		t.Errorf("the subscriptions left are %q, want Ellie's two: Sam's phone was reported gone", remaining)
	}
}

func TestActivity_TheNotificationsPathsAreMadeAbsoluteUnderTheBaseURL(t *testing.T) {
	activity := NewActivity(slog.New(slog.DiscardHandler), nil, nil, "https://sprig.example.com/")

	if got, want := activity.absolute("/plants/doris"), "https://sprig.example.com/plants/doris"; got != want {
		t.Errorf("the notification opens %q, want %q", got, want)
	}
}

func TestActivity_APlantWithNoPictureGivesTheNotificationNoIcon(t *testing.T) {
	activity := NewActivity(slog.New(slog.DiscardHandler), nil, nil, "https://sprig.example.com/")

	if got := activity.absolute(""); got != "" {
		t.Errorf("the icon is %q, want it empty so the service worker uses the app icon", got)
	}
}
