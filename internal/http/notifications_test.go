package http

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The two User-Agent strings the seeded subscriptions carry. A real one names
// no model, which is why the Mac's row cannot say MacBook Air.
const (
	safariOniPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"
	chromeOnMac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

var (
	checkedBox    = regexp.MustCompile(`<input type="checkbox" id="([^"]+)"[^>]*?( checked)?>`)
	selectedHour  = regexp.MustCompile(`<option value="(\d+)" selected>`)
	hourOption    = regexp.MustCompile(`<option value="(\d+)"[^>]*>([^<]+)</option>`)
	hourSelect    = regexp.MustCompile(`(?s)<select class="input input--narrow" id="hour".*?</select>`)
	stackRow      = regexp.MustCompile(`(?s)<li class="row row--setting row--stack">(.*?)</li>`)
	stackRowName  = regexp.MustCompile(`(?s)<span class="row__name">(.*?)</span>`)
	stackRowMeta  = regexp.MustCompile(`(?s)<span class="row__part">(.*?)</span>`)
	stackRowDrops = regexp.MustCompile(`<form method="post" action="([^"]+)"><button class="row__drop"`)
)

type stackedRow struct {
	name string
	meta string
	// drop is the URL the row's Remove button posts to, empty on a row with no
	// Remove.
	drop string
}

// stackedRowsOf reads the rows on Passkeys and under "Where they arrive",
// which are the same row.
func stackedRowsOf(page string) []stackedRow {
	var out []stackedRow
	for _, m := range stackRow.FindAllStringSubmatch(page, -1) {
		row := stackedRow{}
		if name := stackRowName.FindStringSubmatch(m[1]); name != nil {
			row.name = text(name[1])
		}
		if meta := stackRowMeta.FindStringSubmatch(m[1]); meta != nil {
			row.meta = text(meta[1])
		}
		if drop := stackRowDrops.FindStringSubmatch(m[1]); drop != nil {
			row.drop = drop[1]
		}
		out = append(out, row)
	}
	return out
}

// checkedOn reports whether the checkbox with this id is checked.
func checkedOn(t *testing.T, page, id string) bool {
	t.Helper()

	for _, m := range checkedBox.FindAllStringSubmatch(page, -1) {
		if m[1] == id {
			return m[2] != ""
		}
	}
	t.Fatalf("the page has no %s checkbox:\n%s", id, page)
	return false
}

func TestNotifications_TheTwoTypesAreCheckedAsTheyAreStored(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.notifications, notificationsPath)

	if !checkedOn(t, page, "digest") {
		t.Error("the digest is stored on and its box is not checked")
	}
	if checkedOn(t, page, "activity") {
		t.Error("the activity messages are stored off and their box is checked")
	}
}

func TestNotifications_TheHourIsOnThePageOnlyWhileTheDigestIsOn(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.notifications, notificationsPath)
	if !hourSelect.MatchString(page) {
		t.Error("the digest is on and the page offers no hour")
	}
	if got := selectedHour.FindStringSubmatch(hourSelect.FindString(page)); got == nil || got[1] != "8" {
		t.Errorf("the hour selected is %v, want 8", got)
	}

	f.exec(t, "UPDATE notification_preference SET enabled = false WHERE membership_id = $1 AND kind = 'digest'", moreMembershipID)
	if hourSelect.MatchString(f.page(t, f.handler.notifications, notificationsPath)) {
		t.Error("the digest is off and the page still offers an hour")
	}
}

// hourLabelOf is the text of the digest select's option for an hour.
func hourLabelOf(t *testing.T, page, value string) string {
	t.Helper()

	for _, m := range hourOption.FindAllStringSubmatch(hourSelect.FindString(page), -1) {
		if m[1] == value {
			return text(m[2])
		}
	}
	t.Fatalf("the digest select has no option for %q:\n%s", value, page)
	return ""
}

func TestNotifications_TheDigestSelectReadsMidnightAndNoonAsTwelve(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.notifications, notificationsPath)

	if got := hourLabelOf(t, page, "0"); got != "12:00am" {
		t.Errorf("midnight reads %q, want %q", got, "12:00am")
	}
	if got := hourLabelOf(t, page, "12"); got != "12:00pm" {
		t.Errorf("noon reads %q, want %q", got, "12:00pm")
	}
}

func TestNotifications_TheHoursNoteNamesTheAccountsTimezone(t *testing.T) {
	f := moreGarden(t)
	f.principal.User.Timezone = "America/New_York"

	page := text(f.page(t, f.handler.notifications, notificationsPath))

	if !strings.Contains(page, "In America/New York") {
		t.Errorf("the note does not name the account's timezone:\n%s", page)
	}
}

func TestNotifications_EachSubscribedBrowserIsARowWithWhenItLastReceivedOne(t *testing.T) {
	f := moreGarden(t)

	rows := stackedRowsOf(f.page(t, f.handler.notifications, notificationsPath))

	want := []stackedRow{
		{name: "iPhone · Safari", meta: "Last used today", drop: removeBrowserPath(phonePushID)},
		{name: "Mac · Chrome", meta: "Never used", drop: removeBrowserPath(laptopPushID)},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("the browsers are %v, want %v", rows, want)
	}
}

func TestNotifications_AnAccountWithNoSubscriptionSaysNothingIsBeingDelivered(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "DELETE FROM push_subscription WHERE user_id = $1", moreUserID)

	page := text(f.page(t, f.handler.notifications, notificationsPath))

	if !strings.Contains(page, "No browser is subscribed yet") {
		t.Errorf("the page does not say nothing is being delivered:\n%s", page)
	}
}

func TestNotifications_SavingWritesBothTypesAndTheHour(t *testing.T) {
	f := moreGarden(t)

	rec := f.do(t, f.handler.saveNotifications, notificationsPath, url.Values{
		"activity": {"on"},
		"hour":     {"19"},
	})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != notificationsPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, notificationsPath)
	}
	if got, want := settingsOf(t, f, moreMembershipID), (notificationSettings{digest: false, activity: true, hour: 19}); got != want {
		t.Errorf("the membership holds %+v, want %+v", got, want)
	}
}

func TestNotifications_SavingWithTheDigestOffKeepsTheHourItArrivedAt(t *testing.T) {
	f := moreGarden(t)

	// The select is not on the page while the digest is off, so the form that
	// turns it back on carries no hour.
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
	f := moreGarden(t)

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

func TestNotifications_RemovingABrowserDeletesTheSubscription(t *testing.T) {
	f := moreGarden(t)

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
	f := moreGarden(t)

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
		{"something else entirely", "Unknown browser"},
	}
	for _, c := range cases {
		if got := browserName(&c.userAgent); got != c.want {
			t.Errorf("%s is %q, want %q", c.userAgent, got, c.want)
		}
	}
	if got := browserName(nil); got != "Unknown browser" {
		t.Errorf("a subscription with no user agent is %q, want %q", got, "Unknown browser")
	}
}
