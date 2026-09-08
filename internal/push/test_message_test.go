package push

import (
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

var testAtNoon = utc(2026, time.September, 3, 12, 0, 0)

func newTestMessage(t *testing.T, db store.DBTX, service *pushService) *TestMessage {
	t.Helper()

	message := NewTestMessage(store.New(db), NewSender(testKeys(t), service.Client()), "https://sprig.example.com/")
	message.now = fixed(testAtNoon)
	return message
}

func subscriptionAt(t *testing.T, db store.DBTX, service *pushService, path string) store.PushSubscription {
	t.Helper()

	subscription, err := store.New(db).GetPushSubscriptionByEndpoint(t.Context(), ellieID, service.URL+path)
	if err != nil {
		t.Fatalf("reading the subscription at %s: %v", path, err)
	}
	return subscription
}

var notificationsAreWorking = Notification{Title: "Notifications are working", Body: "This is a test.", URL: "/more/notifications"}

func TestTestMessage_SendsToTheBrowserAndRecordsTheSend(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	message := newTestMessage(t, tx, service)

	err := message.Send(t.Context(), subscriptionAt(t, tx, service, "/ellie-phone"), notificationsAreWorking)

	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := service.received(); !slices.Equal(got, []string{"/ellie-phone"}) {
		t.Errorf("the push service received %q, want the phone alone", got)
	}
	if sent, ok := lastSentAt(t, tx, service, "/ellie-phone"); !ok || !sent.Equal(testAtNoon) {
		t.Errorf("last_sent_at = %v, %v, want noon", sent, ok)
	}
}

func TestTestMessage_ABrowserThePushServiceReportsGoneIsDeleted(t *testing.T) {
	service := newPushService(t, http.StatusGone)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	message := newTestMessage(t, tx, service)

	err := message.Send(t.Context(), subscriptionAt(t, tx, service, "/ellie-phone"), notificationsAreWorking)

	if !errors.Is(err, ErrGone) {
		t.Fatalf("Send: %v, want ErrGone", err)
	}
	var remaining int
	if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM push_subscription WHERE endpoint = $1", service.URL+"/ellie-phone").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Error("the phone's row is still there after the push service reported it gone")
	}
}

func TestTestMessage_ARefusalIsReturnedAndTheBrowserIsKept(t *testing.T) {
	service := newPushService(t, http.StatusInternalServerError)
	tx := pgtest.Tx(t, migrateSchema)
	seedRosewood(t, tx, service)
	message := newTestMessage(t, tx, service)

	err := message.Send(t.Context(), subscriptionAt(t, tx, service, "/ellie-phone"), notificationsAreWorking)

	if err == nil || errors.Is(err, ErrGone) {
		t.Fatalf("Send: %v, want the push service's refusal", err)
	}
	if _, ok := lastSentAt(t, tx, service, "/ellie-phone"); ok {
		t.Error("a refused send was recorded as a send")
	}
}
