package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// The reader's membership of a second garden, for the check that a time set
// on one garden is set on that garden alone.
var otherMembershipID = uuid.MustParse("00000000-0000-7000-8000-000000000107")

// remindAgainIn returns the Remind me again banner in a response body, or
// nil when the body has none.
func remindAgainIn(body string) *element {
	return readHTML(body).byID("remind-again")
}

// startsAt returns the time the banner's input starts at, as "15:04".
func startsAt(t *testing.T, banner *element) string {
	t.Helper()
	input := banner.byID("remind-again-at")
	if !input.has("value") {
		t.Fatalf("the time input has no value:\n%s", banner)
	}
	return input.attr("value")
}

// showFrom requests Today at path. path may include a query string.
func (f *todayFixture) showFrom(t *testing.T, path string) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	f.handler.show(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *todayFixture) remindAgain(t *testing.T, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, RemindAgainPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	f.handler.remindAgain(rec, req)
	return rec
}

// storedRemindAgain returns the time set on a membership, or the zero time
// when none is.
func (f *todayFixture) storedRemindAgain(t *testing.T, membershipID uuid.UUID) time.Time {
	t.Helper()
	var at *time.Time
	if err := f.tx.QueryRow(t.Context(), "SELECT remind_again_at FROM membership WHERE id = $1", membershipID).Scan(&at); err != nil {
		t.Fatalf("reading the time: %v", err)
	}
	if at == nil {
		return time.Time{}
	}
	return *at
}

// dailyDigest switches the reader's daily digest on or off. Today offers the
// Remind me again banner only while it is on.
func (f *todayFixture) dailyDigest(t *testing.T, on bool) {
	t.Helper()
	f.exec(t, "INSERT INTO notification_preference (membership_id, kind, enabled) VALUES ($1, 'digest', $2)", rosewoodMembershipID, on)
}

func TestToday_TheRemindMeAgainBannerIsShownOnlyWhenOpenedFromTheNotificationWithPushOn(t *testing.T) {
	cases := []struct {
		name    string
		pushKey string
		path    string
		shown   bool
	}{
		{"opened from the notification", testPushKey, DigestPath, true},
		{"opened any other way", testPushKey, todayPath, false},
		{"opened from the notification with push off", "", DigestPath, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.handler.pushKey = c.pushKey
			f.dailyDigest(t, true)

			page := f.showFrom(t, c.path)

			banner := remindAgainIn(page)
			if (banner != nil) != c.shown {
				t.Fatalf("the banner is shown = %v, want %v:\n%s", banner != nil, c.shown, page)
			}
			if !c.shown {
				return
			}
			words := banner.text()
			for _, want := range []string{"In 1 hour", "In 2 hours", "Remind me"} {
				if !strings.Contains(words, want) {
					t.Errorf("the banner does not offer %q:\n%s", want, words)
				}
			}
		})
	}
}

func TestToday_TheTimeSetIsShownFromTheNotificationOnlyWhileAResendIsWaiting(t *testing.T) {
	cases := []struct {
		name string
		path string
		at   time.Time
		want string
	}{
		{"from the notification while waiting", DigestPath, thursday.Add(2 * time.Hour), "You’ll get this notification again at 11:00am. Dismiss"},
		{"a plain visit while waiting", todayPath, thursday.Add(2 * time.Hour), ""},
		{"from the notification once the time has passed", DigestPath, thursday.Add(-time.Hour), "Get this notification again later. In 1 hour In 2 hours Dismiss Time Remind me"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.handler.pushKey = testPushKey
			f.dailyDigest(t, true)
			at := c.at
			f.principal.Membership.RemindAgainAt = &at

			page := f.showFrom(t, c.path)

			if got := remindAgainIn(page).text(); got != c.want {
				t.Errorf("the banner says %q, want %q", got, c.want)
			}
		})
	}
}

func TestToday_TheTimeInputStartsFiveMinutesFromNowInTheReadersTimezone(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	page := f.showFrom(t, DigestPath)

	// 08:00 UTC is 09:00 in London.
	if got := startsAt(t, remindAgainIn(page)); got != "09:05" {
		t.Errorf("the time input starts at %q, want %q", got, "09:05")
	}
}

func TestToday_DismissOnTheBannerLinksToTodayWithoutTheQueryString(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	page := f.showFrom(t, DigestPath)

	banner := remindAgainIn(page)
	link := banner.first(isTag("a"), textIs("Dismiss"))
	if link == nil {
		t.Fatalf("the banner has no Dismiss link:\n%s", banner)
	}
	if got := link.attr("href"); got != todayPath {
		t.Errorf("Dismiss points at %q, want %q", got, todayPath)
	}
}

func TestRemindAgain_ADelaySetsTheTimeThatFarFromNowAndWakesTheJob(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)
	calls := 0
	f.handler.wake = countingWake(&calls)

	rec := f.remindAgain(t, url.Values{"delay": {"2h"}}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := f.storedRemindAgain(t, rosewoodMembershipID), thursday.Add(2*time.Hour); !got.Equal(want) {
		t.Errorf("the time set is %s, want %s", got.UTC(), want)
	}
	if calls != 1 {
		t.Errorf("the job was woken %d times, want once", calls)
	}
	// 08:00 UTC is 09:00 in London, so two hours on is 11:00am.
	if got, want := remindAgainIn(rec.Body.String()).text(), "You’ll get this notification again at 11:00am. Dismiss"; got != want {
		t.Errorf("the swapped banner says %q, want %q", got, want)
	}
	if got, want := announcement(rec.Body.String()), "You’ll get this notification again at 11:00am."; got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestRemindAgain_ATimeIsReadOnTheClockInTheReadersTimezone(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	rec := f.remindAgain(t, url.Values{"at": {"14:30"}}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	// 14:30 in London on 3 September is 13:30 UTC.
	if got, want := f.storedRemindAgain(t, rosewoodMembershipID), thursday.Add(5*time.Hour+30*time.Minute); !got.Equal(want) {
		t.Errorf("the time set is %s, want %s", got.UTC(), want)
	}
	if got, want := remindAgainIn(rec.Body.String()).text(), "You’ll get this notification again at 2:30pm. Dismiss"; got != want {
		t.Errorf("the swapped banner says %q, want %q", got, want)
	}
}

func TestRemindAgain_ATimeAlreadyPassedOrMissingIsRefusedAndNothingIsSet(t *testing.T) {
	cases := []struct {
		name string
		at   string
		want string
	}{
		{"a time earlier today", "08:59", "That time has already passed."},
		{"no time", "", "Enter a time."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.handler.pushKey = testPushKey
			f.dailyDigest(t, true)
			calls := 0
			f.handler.wake = countingWake(&calls)

			rec := f.remindAgain(t, url.Values{"at": {c.at}}, true)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
			}
			banner := remindAgainIn(rec.Body.String()).text()
			if !strings.Contains(banner, c.want) {
				t.Errorf("the banner says %q, want %q in it", banner, c.want)
			}
			if !strings.Contains(banner, "In 1 hour") {
				t.Errorf("the banner no longer offers the delays:\n%s", banner)
			}
			if got := f.storedRemindAgain(t, rosewoodMembershipID); !got.IsZero() {
				t.Errorf("a time was set, %s, want none", got.UTC())
			}
			if calls != 0 {
				t.Errorf("the job was woken %d times, want not at all", calls)
			}
		})
	}
}

func TestRemindAgain_TheTimeTheInputStartsAtIsAccepted(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)
	page := f.showFrom(t, DigestPath)

	rec := f.remindAgain(t, url.Values{"at": {startsAt(t, remindAgainIn(page))}}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := f.storedRemindAgain(t, rosewoodMembershipID), thursday.Add(5*time.Minute); !got.Equal(want) {
		t.Errorf("the time set is %s, want %s", got.UTC(), want)
	}
}

func TestRemindAgain_ARefusedTimeKeepsTheTimeThatWasTyped(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	rec := f.remindAgain(t, url.Values{"at": {"08:30"}}, true)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if got := startsAt(t, remindAgainIn(rec.Body.String())); got != "08:30" {
		t.Errorf("the time input reads %q, want the %q that was typed", got, "08:30")
	}
}

func TestRemindAgain_ARefusedTimeWithoutJavaScriptRendersTodayWithTheMessage(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	rec := f.remindAgain(t, url.Values{"at": {"08:00"}}, false)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	page := rec.Body.String()
	if !strings.Contains(remindAgainIn(page).text(), "That time has already passed.") {
		t.Errorf("the banner does not say the time has passed:\n%s", page)
	}
	if _, order := sectionsOf(page); len(order) == 0 {
		t.Errorf("the day's sections are not on the page:\n%s", page)
	}
}

func TestRemindAgain_ADelayTheBannerDoesNotOfferIsRefused(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	rec := f.remindAgain(t, url.Values{"delay": {"3h"}}, true)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := f.storedRemindAgain(t, rosewoodMembershipID); !got.IsZero() {
		t.Errorf("a time was set, %s, want none", got.UTC())
	}
}

func TestRemindAgain_APlainPostRedirectsToTodayAtTheNotificationsURL(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)

	rec := f.remindAgain(t, url.Values{"delay": {"1h"}}, false)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != DigestPath {
		t.Errorf("status = %d, Location = %q, want %d to %q", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, DigestPath)
	}
	if got, want := f.storedRemindAgain(t, rosewoodMembershipID), thursday.Add(time.Hour); !got.Equal(want) {
		t.Errorf("the time set is %s, want %s", got.UTC(), want)
	}
}

func TestToday_TheRemindMeAgainBannerIsNotShownWithTheDailyDigestSwitchedOff(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, false)

	page := f.showFrom(t, DigestPath)

	if banner := remindAgainIn(page); banner != nil {
		t.Errorf("the banner is shown with no digest to send again:\n%s", banner)
	}
}

func TestRemindAgain_WithTheDailyDigestSwitchedOffTheRouteIsNotFound(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, false)

	rec := f.remindAgain(t, url.Values{"delay": {"1h"}}, true)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := f.storedRemindAgain(t, rosewoodMembershipID); !got.IsZero() {
		t.Errorf("a time was set, %s, want none", got.UTC())
	}
}

func TestRemindAgain_TheTimeIsSetOnlyOnTheGardenTheSessionIsOn(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.dailyDigest(t, true)
	// The reader is in a second garden. The session is on Rosewood.
	ownedGarden{
		id:           otherGardenID,
		name:         "Fairview",
		owner:        store.AppUser{ID: readerID},
		ownerExists:  true,
		membershipID: otherMembershipID,
	}.insert(t, f.tx)

	rec := f.remindAgain(t, url.Values{"delay": {"1h"}}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := f.storedRemindAgain(t, rosewoodMembershipID), thursday.Add(time.Hour); !got.Equal(want) {
		t.Errorf("Rosewood's time is %s, want %s", got.UTC(), want)
	}
	if got := f.storedRemindAgain(t, otherMembershipID); !got.IsZero() {
		t.Errorf("Fairview's time is %s, want none", got.UTC())
	}
}

func TestRemindAgain_WithPushOffTheRouteIsNotFound(t *testing.T) {
	f := rosewood(t)

	rec := f.remindAgain(t, url.Values{"delay": {"1h"}}, true)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := f.storedRemindAgain(t, rosewoodMembershipID); !got.IsZero() {
		t.Errorf("a time was set, %s, want none", got.UTC())
	}
}
