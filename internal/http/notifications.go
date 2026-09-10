package http

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/store"
)

// The two rows notification_kind holds.
const (
	digestKind   = "digest"
	activityKind = "activity"
)

// wakeJobs has the digest and deadlines jobs work out their next send again.
// A handler calls it after committing a change to when either job next sends:
// a digest type switched on or off, the digest hour, an account's timezone, a
// browser subscribed, a garden deleted, an account closed, a token created or
// revoked, a membership's end date set or moved. Without it a job finds the
// change only when its timer next fires. It is nil when push is off.
type wakeJobs func()

func (w wakeJobs) call() {
	if w != nil {
		w()
	}
}

// notifyUser sends one person a push notification on every browser they have
// subscribed. It is nil when push is off. The notifications sent through it
// have no switch on the Notifications page, so a person stops them by removing
// the browser there.
type notifyUser func(ctx context.Context, user store.AppUser, n push.Notification)

func (f notifyUser) call(ctx context.Context, user store.AppUser, n push.Notification) {
	if f != nil {
		f(ctx, user, n)
	}
}

// subscribePath is the URL the page's script posts a new push subscription
// to. There is no form for it, because only the browser's push API can make a
// subscription.
const subscribePath = notificationsPath + "/browsers"

const sendTestPath = notificationsPath + "/test"

// sendTest sends one push message to one browser and returns any send error
// unchanged. A subscription the push service no longer has is deleted and
// push.ErrGone returned. It is nil when push is off.
type sendTest func(ctx context.Context, subscription store.PushSubscription, n push.Notification) error

// testResultParam is the query parameter the redirect after a test puts the
// outcome in.
const testResultParam = "test"

const (
	testSent   = "sent"
	testGone   = "gone"
	testFailed = "failed"
	testNone   = "none"
)

// testResultLines maps each outcome to the line shown under the Send test
// notification button.
var testResultLines = map[string]string{
	testSent:   "Test sent. If it didn’t arrive, check this device’s notification settings.",
	testGone:   "This device’s subscription had expired and has been removed. Turn on a notification and save to subscribe again.",
	testFailed: "The test couldn’t be sent. Try again in a minute.",
	testNone:   "This device isn’t subscribed.",
}

// testMessage is the notification Send test notification delivers. Its URL is
// this page's path, made absolute before it is sent.
var testMessage = push.Notification{
	Title: "Test notification",
	Body:  "Notifications are working.",
	URL:   notificationsPath,
}

func removeBrowserPath(subscriptionID uuid.UUID) string {
	return notificationsPath + "/browsers/" + subscriptionID.String() + "/remove"
}

type notificationsPage struct {
	Bar    topbar
	Action string
	// Key is the VAPID public key the browser subscribes with. When it is
	// empty the page is one line saying notifications are not enabled, with no
	// checkboxes and no device list.
	Key string
	// Subscribe is the URL the script posts the browser's subscription to.
	Subscribe string
	// Install is the URL of the Install sprig link. The script shows the link
	// in place of the form on an iPhone that has not put sprig on its Home
	// Screen.
	Install string
	// Digest is one digest a day of what is due. Activity is a message when
	// somebody else in the garden logs care.
	Digest   bool
	Activity bool
	// Hours is the select for the hour the digest arrives. It is empty when the
	// digest is off, because the hour is that digest's and there is nothing
	// else it could time.
	Hours []option
	// Zone is the timezone the hour is read in, spelled as the Account page
	// spells it.
	Zone string
	// Account is the URL of the link in the note under the hour.
	Account  string
	Browsers []browserRow
	// Test is the URL the Send test notification form posts to. The form is on
	// the page only while a device is subscribed.
	Test string
	// TestResult is the line under the Send test notification button saying how
	// it went. It is empty unless the query string holds a known outcome.
	TestResult string
	// Saved is true on the page a save redirects to. "Saved" is shown beside
	// the button.
	Saved bool
}

// browserRow is one push subscription, shown as a row under Subscribed
// devices. A subscription belongs to a browser rather than to an account, so
// one person has a row for each browser they turned notifications on in.
type browserRow struct {
	Name string
	Used string
	// Endpoint is the push service URL of the subscription. The script
	// compares it with this browser's own, so Remove on this browser's row
	// unsubscribes it from the push service as well.
	Endpoint string
	// Remove is the URL the row's Remove button posts to.
	Remove string
}

// devicesID is both the HTML id of the Subscribed devices section and the
// name of the template that renders it. Remove and Send test notification
// swap it.
const devicesID = "devices"

func (h *more) notifications(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	h.renderNotifications(w, r, principal, principal.Membership.DigestHour, notificationsState{
		testResult: testResultLines[r.URL.Query().Get(testResultParam)],
		saved:      saved(r),
	})
}

// notificationsID is both the HTML id of the page under the top bar and the
// name of the template that renders it. Save changes swaps it.
const notificationsID = "notifications"

// notificationsState is what one response adds to the saved settings: the
// line under Send test notification, whether Saved is shown under Save
// changes, and the sentence a swap puts in the live region.
type notificationsState struct {
	testResult string
	saved      bool
	announce   string
}

// renderNotifications writes the page. hour is the digest hour to show. It is
// passed in rather than read from the principal because after a save the
// principal on the request still holds the hour from before it.
func (h *more) renderNotifications(w http.ResponseWriter, r *http.Request, principal auth.Principal, hour int16, state notificationsState) {
	preferences, err := h.queries.ListNotificationPreferences(r.Context(), principal.Membership.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "read the notification preferences", err)
		return
	}
	subscriptions, err := h.queries.ListPushSubscriptions(r.Context(), principal.User.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the push subscriptions", err)
		return
	}

	now := h.now().In(locationFor(principal.User))
	page := newNotificationsPage(principal.User.Timezone, notificationOn(preferences, digestKind),
		notificationOn(preferences, activityKind), hour, subscriptions, now)
	page.Key = h.pushKey
	page.TestResult = state.testResult
	page.Saved = state.saved
	v := view{page: "notifications", announce: state.announce}
	switch r.Header.Get("HX-Target") {
	case devicesID:
		v.fragment = devicesID
	case notificationsID:
		v.fragment = notificationsID
	}
	h.templates.render(w, r, v, page)
}

// sendTestNotification sends the test message to the browser whose endpoint
// the form posted. A swap gets the Subscribed devices section with the
// outcome under the button, and a plain post is redirected to the
// Notifications page with the outcome in the query string. An endpoint that
// is empty or another account's matches no row. The outcome is then
// testNone.
func (h *more) sendTestNotification(w http.ResponseWriter, r *http.Request) {
	// With push off no browser can be subscribed, so the route is a 404.
	if h.pushKey == "" || h.test == nil {
		h.templates.notFound(w, r)
		return
	}
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	result := testSent
	subscription, err := h.queries.GetPushSubscriptionByEndpoint(r.Context(), principal.User.ID, r.PostForm.Get("endpoint"))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		result = testNone
	case err != nil:
		h.templates.serverError(h.logger, w, r, "find the browser to test", err)
		return
	default:
		switch err := h.test(r.Context(), subscription, testMessage); {
		case errors.Is(err, push.ErrGone):
			result = testGone
		case err != nil:
			h.logger.ErrorContext(r.Context(), "test notification not sent", slog.Any("error", err))
			result = testFailed
		}
	}
	if isHTMX(r) {
		state := notificationsState{testResult: testResultLines[result], announce: testResultLines[result]}
		h.renderNotifications(w, r, principal, principal.Membership.DigestHour, state)
		return
	}
	http.Redirect(w, r, notificationsPath+"?"+testResultParam+"="+result, http.StatusSeeOther)
}

// subscribeBrowser stores the endpoint and two keys the browser's push API
// produced. The row belongs to the signed-in account, so a browser another
// account subscribed in moves to this one and its dates start over.
func (h *more) subscribeBrowser(w http.ResponseWriter, r *http.Request) {
	// With push off no browser can have a subscription to post, so the route
	// is a 404.
	if h.pushKey == "" {
		h.templates.notFound(w, r)
		return
	}
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	endpoint := r.PostForm.Get("endpoint")
	p256dh, auth := r.PostForm.Get("p256dh"), r.PostForm.Get("auth")
	if !isPushEndpoint(endpoint) || !isPushPoint(p256dh) || !isPushKey(auth, authLength) {
		h.templates.badRequest(w, r)
		return
	}
	var userAgent *string
	if ua := r.UserAgent(); ua != "" {
		userAgent = &ua
	}
	err := h.queries.UpsertPushSubscription(r.Context(), store.UpsertPushSubscriptionParams{
		UserID:    principal.User.ID,
		Endpoint:  endpoint,
		P256dhKey: p256dh,
		AuthKey:   auth,
		UserAgent: userAgent,
	})
	if err != nil {
		h.templates.serverError(h.logger, w, r, "save the push subscription", err)
		return
	}
	h.wake.call()
	w.WriteHeader(http.StatusNoContent)
}

// authLength is the length in bytes of a subscription's auth secret.
const authLength = 16

// isPushEndpoint reports whether v is an absolute https URL. Every push
// service is reached over https.
func isPushEndpoint(v string) bool {
	parsed, err := url.Parse(v)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

// isPushPoint reports whether v is base64url for a point on P-256. A
// subscription's p256dh key has that form, and a message cannot be encrypted
// to anything else.
func isPushPoint(v string) bool {
	decoded, err := decodePushKey(v)
	if err != nil {
		return false
	}
	_, err = ecdh.P256().NewPublicKey(decoded)
	return err == nil
}

// isPushKey reports whether v is base64url for exactly length bytes.
func isPushKey(v string, length int) bool {
	decoded, err := decodePushKey(v)
	return err == nil && len(decoded) == length
}

// decodePushKey decodes one of a subscription's keys. The browser encodes
// without padding, and a padded key is accepted too.
func decodePushKey(v string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(v, "="))
}

// saveNotifications writes both types and the digest's hour. The hour is
// absent from the form when the digest is off, and the stored hour is kept.
func (h *more) saveNotifications(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	hour := principal.Membership.DigestHour
	if r.PostForm.Has("hour") {
		posted, ok := offeredHour(r.PostForm.Get("hour"))
		if !ok {
			h.templates.badRequest(w, r)
			return
		}
		hour = posted
	}
	digest, activity := r.PostForm.Has(digestKind), r.PostForm.Has(activityKind)

	// Both writes are in one transaction, so the types and the hour cannot end
	// up disagreeing with what the form said.
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		kinds := []struct {
			name    string
			enabled bool
		}{{digestKind, digest}, {activityKind, activity}}
		for _, kind := range kinds {
			params := store.SetNotificationPreferenceParams{MembershipID: principal.Membership.ID, Kind: kind.name, Enabled: kind.enabled}
			if err := q.SetNotificationPreference(r.Context(), params); err != nil {
				return err
			}
		}
		return q.SetDigestHour(r.Context(), store.SetDigestHourParams{
			DigestHour: hour,
			GardenID:   principal.Garden.ID,
			UserID:     principal.User.ID,
		})
	})
	if err != nil {
		h.templates.serverError(h.logger, w, r, "save the notification settings", err)
		return
	}
	h.wake.call()
	// With htmx the response is the page under the top bar with the Saved
	// line on it. A plain post redirects to the page with Saved in the query
	// string.
	if isHTMX(r) {
		h.renderNotifications(w, r, principal, hour, notificationsState{saved: true, announce: savedAnnouncement})
		return
	}
	http.Redirect(w, r, savedURL(notificationsPath), http.StatusSeeOther)
}

// removeBrowser deletes one push subscription. Nothing is sent to that
// browser afterwards.
func (h *more) removeBrowser(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	subscriptionID, err := uuid.Parse(r.PathValue("browser"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	removed, err := h.queries.DeletePushSubscription(r.Context(), principal.User.ID, subscriptionID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "remove the browser", err)
		return
	}
	// Another account's subscription and one already removed are both 404,
	// since neither was a button this page offered.
	if removed == 0 {
		h.templates.notFound(w, r)
		return
	}
	if isHTMX(r) {
		h.renderNotifications(w, r, principal, principal.Membership.DigestHour, notificationsState{announce: "Device removed."})
		return
	}
	http.Redirect(w, r, notificationsPath, http.StatusSeeOther)
}

func newNotificationsPage(zone string, digest, activity bool, hour int16, subscriptions []store.PushSubscription, now time.Time) notificationsPage {
	page := notificationsPage{
		Bar:       moreBar("Notifications"),
		Action:    notificationsPath,
		Subscribe: subscribePath,
		Install:   installPath,
		Digest:    digest,
		Activity:  activity,
		Zone:      zoneLabel(zone),
		Account:   accountPath,
		Test:      sendTestPath,
	}
	if digest {
		for _, choice := range hours() {
			page.Hours = append(page.Hours, option{Value: strconv.Itoa(int(choice)), Label: hourLabel(choice), On: choice == hour})
		}
	}
	for _, subscription := range subscriptions {
		page.Browsers = append(page.Browsers, browserRow{
			Name:     browserName(subscription.UserAgent),
			Used:     usedNote(subscription.LastSentAt, now),
			Endpoint: subscription.Endpoint,
			Remove:   removeBrowserPath(subscription.ID),
		})
	}
	return page
}

// hours is every hour of the day, in the order the digest select offers them.
func hours() []int16 {
	out := make([]int16, 0, 24)
	for hour := int16(0); hour < 24; hour++ {
		out = append(out, hour)
	}
	return out
}

// offeredHour reads the hour the digest select posted. It returns false for
// anything the select does not hold, so a hand-written post cannot write an
// hour the digest job would never reach.
func offeredHour(posted string) (int16, bool) {
	for _, hour := range hours() {
		if posted == strconv.Itoa(int(hour)) {
			return hour, true
		}
	}
	return 0, false
}

func notificationOn(preferences []store.NotificationPreference, kind string) bool {
	for _, preference := range preferences {
		if preference.Kind == kind {
			return preference.Enabled
		}
	}
	return false
}

// anyNotificationOn reports whether either type is on. More's row says "Off"
// when neither is.
func anyNotificationOn(preferences []store.NotificationPreference) bool {
	return notificationOn(preferences, digestKind) || notificationOn(preferences, activityKind)
}

// deviceNames and browserNames are the User-Agent substrings the row's label is
// built from. Android comes before Linux and Edge before Chrome, because a
// browser writes both tokens and the first match is the one that names it.
var (
	deviceNames = []struct{ token, name string }{
		{"iPhone", "iPhone"},
		{"iPad", "iPad"},
		{"Android", "Android"},
		{"Macintosh", "Mac"},
		{"Windows", "Windows"},
		{"Linux", "Linux"},
	}
	browserNames = []struct{ token, name string }{
		{"Edg/", "Edge"},
		{"Firefox", "Firefox"},
		{"Chrome", "Chrome"},
		{"Safari", "Safari"},
	}
)

// browserName reads a device and a browser out of a User-Agent, as
// "Mac · Chrome". It names the rows on the Notifications page and a passkey on
// the Passkeys page. A User-Agent names no model, so a MacBook Air is a Mac. A
// string that names neither device nor browser is "Unknown device", and its
// date still tells the row from the others.
func browserName(userAgent *string) string {
	if userAgent == nil {
		return "Unknown device"
	}
	parts := make([]string, 0, 2)
	for _, device := range deviceNames {
		if strings.Contains(*userAgent, device.token) {
			parts = append(parts, device.name)
			break
		}
	}
	for _, browser := range browserNames {
		if strings.Contains(*userAgent, browser.token) {
			parts = append(parts, browser.name)
			break
		}
	}
	if len(parts) == 0 {
		return "Unknown device"
	}
	return strings.Join(parts, " · ")
}
