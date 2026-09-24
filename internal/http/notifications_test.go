package http

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/store"
)

// The two User-Agent strings the seeded subscriptions were made with. Neither
// names a model, so the Mac's row cannot say MacBook Air.
const (
	safariOniPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"
	chromeOnMac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

// notificationsFixture's handler has a VAPID public key set, so the page
// renders its form.
type notificationsFixture struct {
	*moreFixture
	handler *notifications
}

func openNotifications(t *testing.T) *notificationsFixture {
	t.Helper()

	f := moreGarden(t)
	return &notificationsFixture{
		moreFixture: f,
		handler: &notifications{
			logger:    testLogger,
			queries:   store.New(f.tx),
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
			pushKey:   testPushKey,
		},
	}
}

type stackedRow struct {
	name string
	meta string
	// drop is the URL the row's Remove button posts to, empty on a row with no
	// Remove.
	drop string
}

// stackedRowsOf reads the rows of the passkey list on Passkeys and of
// Subscribed devices on Notifications. A row is a list item in either section:
// its name, the line under the name, and the form holding Remove.
func stackedRowsOf(page string) []stackedRow {
	doc := readHTML(page)
	var out []stackedRow
	for _, section := range []string{passkeysListID, devicesID} {
		for _, item := range doc.byID(section).all(isTag("li")) {
			var runs []string
			for _, c := range item.children {
				if child, ok := c.(*element); ok && child.tag != "form" {
					runs = append(runs, textRuns(child)...)
				}
			}
			row := stackedRow{drop: item.first(isTag("form")).attr("action")}
			if len(runs) > 0 {
				row.name = runs[0]
			}
			if len(runs) > 1 {
				row.meta = runs[1]
			}
			out = append(out, row)
		}
	}
	return out
}

// checkedOn reports whether the checkbox with this id is checked.
func checkedOn(t *testing.T, page, id string) bool {
	t.Helper()

	box := readHTML(page).byID(id)
	if box.attr("type") != "checkbox" {
		t.Fatalf("the page has no %s checkbox:\n%s", id, page)
	}
	return box.has("checked")
}

// hourSelectOf returns the select the digest's hour is chosen with, or nil
// when the page has none.
func hourSelectOf(page string) *element {
	return readHTML(page).byID("hour")
}

func TestNotifications_TheTwoTypesAreCheckedAsTheyAreStored(t *testing.T) {
	f := openNotifications(t)

	page := f.page(t, f.handler.show, notificationsPath)

	if !checkedOn(t, page, "digest") {
		t.Error("the digest is stored on and its box is not checked")
	}
	if checkedOn(t, page, "activity") {
		t.Error("the activity messages are stored off and their box is checked")
	}
}

func TestNotifications_TheHourIsOnThePageOnlyWhileTheDigestIsOn(t *testing.T) {
	f := openNotifications(t)

	hour := hourSelectOf(f.page(t, f.handler.show, notificationsPath))
	if hour == nil {
		t.Fatal("the digest is on and the page offers no hour")
	}
	if got := hour.first(isTag("option"), hasAttr("selected")).attr("value"); got != "8" {
		t.Errorf("the hour selected is %q, want 8", got)
	}

	f.exec(t, "UPDATE notification_preference SET enabled = false WHERE membership_id = $1 AND kind = 'digest'", moreMembershipID)
	if hourSelectOf(f.page(t, f.handler.show, notificationsPath)) != nil {
		t.Error("the digest is off and the page still offers an hour")
	}
}

// hourLabelOf is the text of the digest select's option for an hour.
func hourLabelOf(t *testing.T, page, value string) string {
	t.Helper()

	option := hourSelectOf(page).first(isTag("option"), attrIs("value", value))
	if option == nil {
		t.Fatalf("the digest select has no option for %q:\n%s", value, page)
	}
	return option.text()
}

func TestNotifications_TheDigestSelectReadsMidnightAndNoonAsTwelve(t *testing.T) {
	f := openNotifications(t)

	page := f.page(t, f.handler.show, notificationsPath)

	if got := hourLabelOf(t, page, "0"); got != "12:00am" {
		t.Errorf("midnight reads %q, want %q", got, "12:00am")
	}
	if got := hourLabelOf(t, page, "12"); got != "12:00pm" {
		t.Errorf("noon reads %q, want %q", got, "12:00pm")
	}
}

func TestNotifications_TheHoursNoteNamesTheAccountsTimezone(t *testing.T) {
	f := openNotifications(t)
	f.principal.User.Timezone = "America/New_York"

	page := text(f.page(t, f.handler.show, notificationsPath))

	if !strings.Contains(page, "America/New York time") {
		t.Errorf("the note does not name the account's timezone:\n%s", page)
	}
}

func TestNotifications_EachSubscribedBrowserIsARowWithWhenItLastReceivedOne(t *testing.T) {
	f := openNotifications(t)

	rows := stackedRowsOf(f.page(t, f.handler.show, notificationsPath))

	want := []stackedRow{
		{name: "iPhone · Safari", meta: "Last used today", drop: removeBrowserPath(phonePushID)},
		{name: "Mac · Chrome", meta: "Never used", drop: removeBrowserPath(laptopPushID)},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("the browsers are %v, want %v", rows, want)
	}
}

func TestNotifications_AnAccountWithNoSubscriptionSaysNothingIsBeingDelivered(t *testing.T) {
	f := openNotifications(t)
	f.exec(t, "DELETE FROM push_subscription WHERE user_id = $1", moreUserID)

	page := text(f.page(t, f.handler.show, notificationsPath))

	if !strings.Contains(page, "No devices are subscribed") {
		t.Errorf("the page does not say nothing is being delivered:\n%s", page)
	}
}

func TestNotifications_ASaveSentAsASwapShowsTheHourJustSaved(t *testing.T) {
	f := openNotifications(t)
	form := url.Values{"digest": {"on"}, "hour": {"19"}}

	rec := f.swap(t, f.handler.saveNotifications, notificationsPath, pushFormID, "", "", form)

	body := fragment(t, rec, pushFormID)
	if got := hourSelectOf(body).first(isTag("option"), hasAttr("selected")).attr("value"); got != "19" {
		t.Errorf("the swap shows the hour %q selected, want the 19 just saved", got)
	}
	if got := announcement(body); got != savedAnnouncement {
		t.Errorf("the swap announces %q, want %q", got, savedAnnouncement)
	}
	if savedLine(body) != nil {
		t.Errorf("the swap shows a Saved line under Save changes, want only the announcement:\n%s", body)
	}
}

func TestNotifications_ASaveSentAsASwapGetsTheFormWithoutTheSubscribedDevices(t *testing.T) {
	f := openNotifications(t)
	form := url.Values{"activity": {"on"}}

	rec := f.swap(t, f.handler.saveNotifications, notificationsPath, pushFormID, "", "", form)

	if readHTML(fragment(t, rec, pushFormID)).byID(devicesID) != nil {
		t.Errorf("the swap holds the Subscribed devices section, want the form alone:\n%s", rec.Body.String())
	}
}

func TestNotifications_SavingWritesBothTypesAndTheHour(t *testing.T) {
	f := openNotifications(t)

	rec := f.do(t, f.handler.saveNotifications, notificationsPath, url.Values{
		"activity": {"on"},
		"hour":     {"19"},
	})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != savedURL(notificationsPath) {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, savedURL(notificationsPath))
	}
	if page := f.page(t, f.handler.show, savedURL(notificationsPath)); savedLine(page) == nil {
		t.Errorf("the page after a save does not say Saved:\n%s", text(page))
	}
	if got, want := settingsOf(t, f.moreFixture, moreMembershipID), (notificationSettings{digest: false, activity: true, hour: 19}); got != want {
		t.Errorf("the membership holds %+v, want %+v", got, want)
	}
}

// savedLine returns the Saved line under Save changes, or nil when the page has
// none. It skips the hidden Saved beside each control.
func savedLine(markup string) *element {
	return readHTML(markup).first(textIs("Saved"), func(e *element) bool { return !e.has("hidden") })
}

func countingWake(calls *int) wakeJobs {
	return func() { *calls++ }
}

func TestNotifications_SavingTheSettingsWakesTheDigestJob(t *testing.T) {
	f := openNotifications(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.do(t, f.handler.saveNotifications, notificationsPath, url.Values{"digest": {"on"}, "hour": {"19"}})

	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, so the new hour waits for the job's timer", woken)
	}
}

func TestNotifications_SavingWithTheDigestOffKeepsTheHourItArrivedAt(t *testing.T) {
	f := openNotifications(t)

	// The select is not on the page while the digest is off, so the form that
	// turns it back on posts no hour.
	f.do(t, f.handler.saveNotifications, notificationsPath, url.Values{})

	var hour int16
	if err := f.tx.QueryRow(t.Context(), "SELECT digest_hour FROM membership WHERE id = $1", moreMembershipID).Scan(&hour); err != nil {
		t.Fatalf("reading the hour back: %v", err)
	}
	if hour != 8 {
		t.Errorf("the hour is %d, want the stored 8", hour)
	}
}

func TestNotifications_AnHourTheSelectDoesNotOfferIsRefused(t *testing.T) {
	f := openNotifications(t)

	rec := f.do(t, f.handler.saveNotifications, notificationsPath, url.Values{"digest": {"on"}, "hour": {"24"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var digest bool
	if err := f.tx.QueryRow(t.Context(),
		"SELECT enabled FROM notification_preference WHERE membership_id = $1 AND kind = 'digest'", moreMembershipID).Scan(&digest); err != nil {
		t.Fatalf("reading the digest back: %v", err)
	}
	if !digest {
		t.Error("the refused post changed the settings")
	}
}

func TestNotifications_SubscribingABrowserWakesTheDigestJob(t *testing.T) {
	f := openNotifications(t)
	woken := 0
	f.handler.wake = countingWake(&woken)
	p256dh, secret := browserKeys(t)

	f.subscribe(t, "https://push.invalid/new-phone", chromeOnMac, p256dh, secret)

	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, so the new browser waits for the job's timer", woken)
	}
}

func TestNotifications_RemovingABrowserDeletesTheSubscription(t *testing.T) {
	f := openNotifications(t)

	rec := f.remove(t, f.handler.removeBrowser, "browser", phonePushID, removeBrowserPath(phonePushID))

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != notificationsPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, notificationsPath)
	}
	var left int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM push_subscription WHERE user_id = $1", moreUserID).Scan(&left); err != nil {
		t.Fatalf("counting the subscriptions: %v", err)
	}
	if left != 1 {
		t.Errorf("%d subscriptions are left, want 1", left)
	}
}

func TestNotifications_AnotherAccountsBrowserIsNotFound(t *testing.T) {
	f := openNotifications(t)

	rec := f.remove(t, f.handler.removeBrowser, "browser", strangerPushID, removeBrowserPath(strangerPushID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var left int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM push_subscription WHERE user_id = $1", otherUserID).Scan(&left); err != nil {
		t.Fatalf("counting the subscriptions: %v", err)
	}
	if left != 1 {
		t.Errorf("the other account has %d subscriptions, want 1", left)
	}
}

func TestBrowserName_NamesTheDeviceAndTheBrowserTheSubscriptionCameFrom(t *testing.T) {
	cases := []struct {
		userAgent string
		want      string
	}{
		{safariOniPhone, "iPhone · Safari"},
		{chromeOnMac, "Mac · Chrome"},
		{"Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36", "Android · Chrome"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0", "Windows · Edge"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:143.0) Gecko/20100101 Firefox/143.0", "Linux · Firefox"},
		{"something else entirely", "Unknown device"},
	}
	for _, c := range cases {
		if got := browserName(&c.userAgent); got != c.want {
			t.Errorf("%s is %q, want %q", c.userAgent, got, c.want)
		}
	}
	if got := browserName(nil); got != "Unknown device" {
		t.Errorf("a subscription with no user agent is %q, want %q", got, "Unknown device")
	}
}

// The form is found by its action because a subscription posted to the page's
// own action would be saved as settings with both types off.
func TestNotifications_AddThisDevicePostsToTheSubscribeURLWithThePublicKey(t *testing.T) {
	f := openNotifications(t)

	page := f.page(t, f.handler.show, notificationsPath)

	form := readHTML(page).first(isTag("form"), attrIs("action", subscribePath))
	if got := form.attr("data-key"); got != testPushKey {
		t.Errorf("the data-key of a form posting to %s is %q, want the configured public key", subscribePath, got)
	}
}

func TestNotifications_WithPushOffThePageSaysNotificationsAreNotSetUpAndOffersNoSwitches(t *testing.T) {
	f := openNotifications(t)
	f.handler.pushKey = ""

	page := f.page(t, f.handler.show, notificationsPath)

	if !strings.Contains(text(page), "Notifications aren’t enabled on this server") {
		t.Errorf("the page does not say notifications are not set up:\n%s", page)
	}
	if readHTML(page).first(isTag("input"), attrIs("type", "checkbox")) != nil {
		t.Errorf("push is off and the page still offers a switch:\n%s", page)
	}
	if len(stackedRowsOf(page)) != 0 {
		t.Errorf("push is off and the page still lists browsers:\n%s", page)
	}
}

// browserKeys returns a subscription's two keys in the form the push API
// encodes them: a point on P-256 and a 16-byte secret, base64url without
// padding. Each call returns a different pair.
func browserKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating the browser's key: %v", err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generating the browser's secret: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(secret)
}

func (f *notificationsFixture) subscribe(t *testing.T, endpoint, userAgent, p256dh, auth string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{"endpoint": {endpoint}, "p256dh": {p256dh}, "auth": {auth}}
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, subscribePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	rec := httptest.NewRecorder()
	f.handler.subscribeBrowser(rec, req)
	return rec
}

// subscriptionRow is the push_subscription row for one endpoint, read back
// after a post.
type subscriptionRow struct {
	userID    uuid.UUID
	userAgent string
	p256dh    string
	neverSent bool
}

func subscriptionOf(t *testing.T, f *moreFixture, endpoint string) subscriptionRow {
	t.Helper()

	var row subscriptionRow
	var userAgent *string
	var lastSentAt *time.Time
	err := f.tx.QueryRow(t.Context(), "SELECT user_id, user_agent, p256dh_key, last_sent_at FROM push_subscription WHERE endpoint = $1", endpoint).
		Scan(&row.userID, &userAgent, &row.p256dh, &lastSentAt)
	if err != nil {
		t.Fatalf("reading the subscription for %s: %v", endpoint, err)
	}
	if userAgent != nil {
		row.userAgent = *userAgent
	}
	row.neverSent = lastSentAt == nil
	return row
}

func TestSubscribe_WritesTheBrowsersSubscriptionForTheSignedInAccount(t *testing.T) {
	f := openNotifications(t)
	p256dh, auth := browserKeys(t)

	rec := f.subscribe(t, "https://push.example.com/send/new", chromeOnMac, p256dh, auth)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	got := subscriptionOf(t, f.moreFixture, "https://push.example.com/send/new")
	want := subscriptionRow{userID: moreUserID, userAgent: chromeOnMac, p256dh: p256dh, neverSent: true}
	if got != want {
		t.Errorf("the row holds %+v, want %+v", got, want)
	}
}

func TestSubscribe_TheSameBrowserAgainKeepsOneRowAndItsDatesAndTakesItsNewKeys(t *testing.T) {
	f := openNotifications(t)
	// A browser makes new keys when it subscribes again. A message encrypted
	// to the old ones cannot be decrypted, so the row has to take the new
	// pair.
	p256dh, auth := browserKeys(t)

	// The phone's row was last sent to today. Subscribing the same browser
	// again keeps that date.
	rec := f.subscribe(t, "https://push.invalid/phone", safariOniPhone, p256dh, auth)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	var count int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM push_subscription WHERE user_id = $1", moreUserID).Scan(&count); err != nil {
		t.Fatalf("counting the subscriptions: %v", err)
	}
	if count != 2 {
		t.Errorf("the account has %d subscriptions, want the 2 it had", count)
	}
	got := subscriptionOf(t, f.moreFixture, "https://push.invalid/phone")
	if got.neverSent {
		t.Error("subscribing the same browser again forgot when it was last sent to")
	}
	if got.p256dh != p256dh {
		t.Errorf("the row holds the p256dh key %s, want the one just posted, %s", got.p256dh, p256dh)
	}
}

func TestSubscribe_ABrowserAnotherAccountSubscribedInMovesToTheSignedInAccount(t *testing.T) {
	f := openNotifications(t)
	f.exec(t, "UPDATE push_subscription SET last_sent_at = now() WHERE id = $1", strangerPushID)
	p256dh, auth := browserKeys(t)

	rec := f.subscribe(t, "https://push.invalid/stranger", chromeOnMac, p256dh, auth)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	got := subscriptionOf(t, f.moreFixture, "https://push.invalid/stranger")
	if got.userID != moreUserID {
		t.Errorf("the browser still belongs to %s, want the account signed in", got.userID)
	}
	if !got.neverSent {
		t.Error("the browser carried the other account's last send over")
	}
	var others int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM push_subscription WHERE user_id = $1", otherUserID).Scan(&others); err != nil {
		t.Fatalf("counting the other account's subscriptions: %v", err)
	}
	if others != 0 {
		t.Errorf("the other account still has %d subscriptions, want 0", others)
	}
}

func TestSubscribe_AnEndpointOrKeyOfTheWrongFormIsRefusedAndWritesNoRow(t *testing.T) {
	f := openNotifications(t)
	p256dh, auth := browserKeys(t)
	cases := []struct {
		name string
		form url.Values
	}{
		{"an http endpoint", url.Values{"endpoint": {"http://push.example.com/send"}, "p256dh": {p256dh}, "auth": {auth}}},
		{"an endpoint that is not a URL", url.Values{"endpoint": {"push"}, "p256dh": {p256dh}, "auth": {auth}}},
		{"a p256dh key of the wrong length", url.Values{"endpoint": {"https://push.example.com/send"}, "p256dh": {auth}, "auth": {auth}}},
		{"a p256dh key that is 65 bytes and not a point", url.Values{"endpoint": {"https://push.example.com/send"}, "p256dh": {base64.RawURLEncoding.EncodeToString(make([]byte, 65))}, "auth": {auth}}},
		{"an auth key that is not base64url", url.Values{"endpoint": {"https://push.example.com/send"}, "p256dh": {p256dh}, "auth": {"not/base64url!"}}},
		{"no keys", url.Values{"endpoint": {"https://push.example.com/send"}}},
	}
	for _, c := range cases {
		rec := f.do(t, f.handler.subscribeBrowser, subscribePath, c.form)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s got %d, want %d", c.name, rec.Code, http.StatusBadRequest)
		}
	}
	var count int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM push_subscription WHERE user_id = $1", moreUserID).Scan(&count); err != nil {
		t.Fatalf("counting the subscriptions: %v", err)
	}
	if count != 2 {
		t.Errorf("a refused post wrote a row: the account has %d, want 2", count)
	}
}

func TestSubscribe_WithPushOffTheRouteIsNotFound(t *testing.T) {
	f := openNotifications(t)
	f.handler.pushKey = ""
	p256dh, auth := browserKeys(t)

	rec := f.subscribe(t, "https://push.example.com/send/new", chromeOnMac, p256dh, auth)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// recordedSends is a sendTest that keeps what it was asked to send and
// returns err.
type recordedSends struct {
	subscriptions []store.PushSubscription
	notifications []push.Notification
	err           error
}

func (r *recordedSends) send(_ context.Context, subscription store.PushSubscription, n push.Notification) error {
	r.subscriptions = append(r.subscriptions, subscription)
	r.notifications = append(r.notifications, n)
	return r.err
}

func TestNotifications_SendATestIsOfferedOnlyWhileABrowserIsSubscribed(t *testing.T) {
	f := openNotifications(t)

	sendTest := func() *element {
		return readHTML(f.page(t, f.handler.show, notificationsPath)).first(isTag("form"), attrIs("action", sendTestPath))
	}

	if sendTest() == nil {
		t.Error("two browsers are subscribed and the page offers no Send a test")
	}

	f.exec(t, "DELETE FROM push_subscription WHERE user_id = $1", moreUserID)
	if sendTest() != nil {
		t.Error("no browser is subscribed and the page still offers Send a test")
	}
}

// testOutcome posts the Send test notification form with endpoint and returns
// the value of the test parameter on the redirect.
func (f *notificationsFixture) testOutcome(t *testing.T, endpoint string) string {
	t.Helper()

	rec := f.do(t, f.handler.sendTestNotification, sendTestPath, url.Values{"endpoint": {endpoint}})
	location, err := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || err != nil || location.Path != notificationsPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, notificationsPath)
	}
	return location.Query().Get(testResultParam)
}

// resultPage renders the Notifications page with outcome in the query string,
// the way the redirect after a test does. It returns the text with the tags
// taken out.
func (f *notificationsFixture) resultPage(t *testing.T, outcome string) string {
	t.Helper()

	return text(f.page(t, f.handler.show, notificationsPath+"?"+testResultParam+"="+outcome))
}

func TestSendTest_OnlyTheBrowserWhoseEndpointWasPostedIsSentTheTest(t *testing.T) {
	f := openNotifications(t)
	recorder := &recordedSends{}
	f.handler.test = recorder.send

	outcome := f.testOutcome(t, phoneEndpoint)

	if outcome != testSent {
		t.Errorf("the outcome is %q, want %q", outcome, testSent)
	}
	if len(recorder.subscriptions) != 1 || recorder.subscriptions[0].ID != phonePushID {
		t.Fatalf("sent to %v, want the phone alone", recorder.subscriptions)
	}
	if got := recorder.notifications[0]; got.URL != notificationsPath || got.Title == "" || got.Body == "" {
		t.Errorf("the message sent is %+v, want a title, a body and this page's path", got)
	}
	if page := f.resultPage(t, testSent); !strings.Contains(page, testResultLines[testSent]) {
		t.Errorf("the page does not say the test was sent:\n%s", page)
	}
}

func TestSendTest_ABrowserThePushServiceHasDroppedSaysItHasBeenRemoved(t *testing.T) {
	f := openNotifications(t)
	f.handler.test = (&recordedSends{err: push.ErrGone}).send

	if outcome := f.testOutcome(t, phoneEndpoint); outcome != testGone {
		t.Errorf("the outcome is %q, want %q", outcome, testGone)
	}
	if page := f.resultPage(t, testGone); !strings.Contains(page, testResultLines[testGone]) {
		t.Errorf("the page does not say the browser has been removed:\n%s", page)
	}
}

func TestSendTest_ARefusedSendSaysTheTestCouldNotBeSent(t *testing.T) {
	f := openNotifications(t)
	f.handler.test = (&recordedSends{err: errors.New("the push service answered 500")}).send

	if outcome := f.testOutcome(t, phoneEndpoint); outcome != testFailed {
		t.Errorf("the outcome is %q, want %q", outcome, testFailed)
	}
	if page := f.resultPage(t, testFailed); !strings.Contains(page, testResultLines[testFailed]) {
		t.Errorf("the page does not say the send failed:\n%s", page)
	}
}

func TestSendTest_AnEmptyOrAnotherAccountsEndpointSendsNothingAndSaysThisBrowserIsNotSubscribed(t *testing.T) {
	f := openNotifications(t)
	recorder := &recordedSends{}
	f.handler.test = recorder.send

	for _, endpoint := range []string{"", strangerEndpoint} {
		if outcome := f.testOutcome(t, endpoint); outcome != testNone {
			t.Errorf("endpoint %q: the outcome is %q, want %q", endpoint, outcome, testNone)
		}
	}
	if len(recorder.subscriptions) != 0 {
		t.Errorf("sent to %v, want nothing sent", recorder.subscriptions)
	}
	if page := f.resultPage(t, testNone); !strings.Contains(page, testResultLines[testNone]) {
		t.Errorf("the page does not say the browser is not subscribed:\n%s", page)
	}
}

func TestSendTest_AnUnknownOutcomeInTheQueryShowsNoLine(t *testing.T) {
	f := openNotifications(t)

	page := f.resultPage(t, "anything")
	for outcome, line := range testResultLines {
		if strings.Contains(page, line) {
			t.Errorf("a made-up outcome rendered the %s line:\n%s", outcome, page)
		}
	}
}

func TestSendTest_WithPushOffTheRouteIsNotFound(t *testing.T) {
	f := openNotifications(t)
	f.handler.pushKey = ""

	if rec := f.do(t, f.handler.sendTestNotification, sendTestPath, url.Values{"endpoint": {phoneEndpoint}}); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestNotifications_ATestSentAsASwapGetsTheDevicesWithTheOutcomeUnderTheButton(t *testing.T) {
	f := openNotifications(t)
	f.handler.test = (&recordedSends{}).send

	rec := f.swap(t, f.handler.sendTestNotification, sendTestPath, devicesID, "", "", url.Values{"endpoint": {""}})

	body := fragment(t, rec, devicesID)
	if !strings.Contains(text(body), testResultLines[testNone]) {
		t.Errorf("the response does not say this device is not subscribed:\n%s", text(body))
	}
}

func TestNotifications_ARemoveSentAsASwapGetsTheDevicesWithoutThatRow(t *testing.T) {
	f := openNotifications(t)

	rec := f.swap(t, f.handler.removeBrowser, removeBrowserPath(phonePushID), devicesID, "browser", phonePushID.String(), url.Values{})

	body := fragment(t, rec, devicesID)
	if readHTML(body).first(isTag("form"), attrIs("action", removeBrowserPath(phonePushID))) != nil {
		t.Errorf("the removed browser is still listed:\n%s", text(body))
	}
}

// addDevice posts a new subscription the way Add this device sends it.
func (f *notificationsFixture) addDevice(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	p256dh, auth := browserKeys(t)
	form := url.Values{"endpoint": {"https://push.example.com/this-device"}, "p256dh": {p256dh}, "auth": {auth}}
	return f.swap(t, f.handler.subscribeBrowser, subscribePath, devicesID, "", "", form)
}

func TestSubscribe_AddThisDeviceGetsTheDevicesWithTheNewRow(t *testing.T) {
	f := openNotifications(t)

	body := fragment(t, f.addDevice(t), devicesID)

	if rows := stackedRowsOf(body); len(rows) != 3 {
		t.Errorf("the devices after adding one are %v, want the two already subscribed and the new one", rows)
	}
	if got := announcement(body); got != "Device added." {
		t.Errorf("the swap announces %q, want %q", got, "Device added.")
	}
}

func TestNotifications_SendTestNotificationIsMarkedAutofocusOnlyInTheResponseToAddThisDevice(t *testing.T) {
	f := openNotifications(t)

	sendTest := func(body string) *element {
		return readHTML(body).first(isTag("button"), textIs("Send test notification"))
	}

	if !sendTest(fragment(t, f.addDevice(t), devicesID)).has("autofocus") {
		t.Error("the response to Add this device does not put focus on Send test notification")
	}
	page := f.page(t, f.handler.show, notificationsPath)
	if button := sendTest(page); button == nil || button.has("autofocus") {
		t.Errorf("the page opened on its own has Send test notification as %v, want it without autofocus", button)
	}
}
