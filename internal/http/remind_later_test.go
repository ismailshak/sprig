package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// The reader's membership of a second garden, for the check that a reminder
// set on one garden is set on that garden alone.
var otherMembershipID = uuid.MustParse("00000000-0000-7000-8000-000000000107")

// offerRemindLater sets up what Today needs to offer Remind me later: push on,
// a subscribed browser, and the daily digest on with today's sent. It is 09:00
// in London.
func (f *todayFixture) offerRemindLater(t *testing.T) {
	t.Helper()
	f.handler.pushKey = testPushKey
	f.subscribe(t)
	f.dailyDigest(t, true)
	f.digestSent(t, "2026-09-03")
}

func (f *todayFixture) subscribe(t *testing.T) {
	t.Helper()
	f.exec(t, "INSERT INTO push_subscription (user_id, endpoint, p256dh_key, auth_key, user_agent) VALUES ($1, 'https://push.example.com/reader', 'p256dh', 'auth', 'Mozilla/5.0 (iPhone) Safari')", readerID)
}

func (f *todayFixture) dailyDigest(t *testing.T, on bool) {
	t.Helper()
	f.exec(t, "INSERT INTO notification_preference (membership_id, kind, enabled) VALUES ($1, 'digest', $2)", rosewoodMembershipID, on)
}

// digestSent writes the ledger row the digest job writes once it has sent the
// reader's digest for date.
func (f *todayFixture) digestSent(t *testing.T, date string) {
	t.Helper()
	f.exec(t, "INSERT INTO notification_send (membership_id, kind, send_key) VALUES ($1, 'digest', $2)", rosewoodMembershipID, date)
}

// remindAt sets the reader's reminder in the database and on the principal
// the next request is made with.
func (f *todayFixture) remindAt(t *testing.T, at time.Time) {
	t.Helper()
	f.exec(t, "UPDATE membership SET remind_again_at = $1 WHERE id = $2", at, rosewoodMembershipID)
	f.principal.Membership.RemindAgainAt = &at
}

// storedReminder returns the reminder set on a membership, or the zero time
// when none is.
func (f *todayFixture) storedReminder(t *testing.T, membershipID uuid.UUID) time.Time {
	t.Helper()
	var at *time.Time
	if err := f.tx.QueryRow(t.Context(), "SELECT remind_again_at FROM membership WHERE id = $1", membershipID).Scan(&at); err != nil {
		t.Fatalf("reading the reminder: %v", err)
	}
	if at == nil {
		return time.Time{}
	}
	return *at
}

// remindLaterIn returns the Remind me later link in a body, or nil.
func remindLaterIn(body string) *element {
	return readHTML(body).byID(remindLaterID)
}

// remindLaterDialog returns the Remind me later sheet in a body, or nil.
func remindLaterDialog(body string) *element {
	return readHTML(body).first(isTag("dialog"), attrIs("aria-labelledby", "remind-later-title"))
}

// openRemindLater requests GET /remind-later, with the headers htmx sends
// when htmx is true.
func (f *todayFixture) openRemindLater(t *testing.T, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, remindLaterPath, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", sheetID)
	}
	rec := httptest.NewRecorder()
	f.handler.showRemindLater(rec, req)
	return rec
}

// postReminder posts form to remindLaterPath or removeReminderPath.
func (f *todayFixture) postReminder(t *testing.T, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	if path == removeReminderPath {
		f.handler.removeReminder(rec, req)
	} else {
		f.handler.setReminder(rec, req)
	}
	return rec
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, want, rec.Body.String())
	}
}

func TestToday_RemindMeLaterIsOfferedOnceTodaysDigestIsSentOrWithTheDigestOff(t *testing.T) {
	cases := []struct {
		name    string
		pushKey string
		browser bool
		digest  bool
		sent    string
		shown   bool
	}{
		{"digest on and today's sent", testPushKey, true, true, "2026-09-03", true},
		{"digest on and only yesterday's sent", testPushKey, true, true, "2026-09-02", false},
		{"digest off", testPushKey, true, false, "", true},
		{"no subscribed browser", testPushKey, false, false, "", false},
		{"push off", "", true, false, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.handler.pushKey = c.pushKey
			if c.browser {
				f.subscribe(t)
			}
			f.dailyDigest(t, c.digest)
			if c.sent != "" {
				f.digestSent(t, c.sent)
			}

			page := f.show(t)

			if link := remindLaterIn(page); (link != nil) != c.shown {
				t.Errorf("the link is shown = %v, want %v", link != nil, c.shown)
			}
		})
	}
}

func TestToday_RemindMeLaterIsBesideTheTitleOfTheFirstSectionThatIsDue(t *testing.T) {
	cases := []struct {
		name    string
		watered []uuid.UUID
		want    string
	}{
		{"with something overdue", nil, "overdue"},
		{"with nothing overdue", []uuid.UUID{bigFellaID}, "due-today"},
		{"with nothing overdue or due today", []uuid.UUID{bigFellaID, dorisID, nigelID}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.offerRemindLater(t)
			for _, plant := range c.watered {
				f.water(t, plant)
			}
			// The watering is at the fixture's now, so it is outside the grace
			// window a minute later.
			f.handler.now = func() time.Time { return thursday.Add(time.Minute) }

			sections, order := sectionsOf(f.show(t))

			var in []string
			for _, id := range order {
				if sections[id].byID(remindLaterID) != nil {
					in = append(in, id)
				}
			}
			switch {
			case c.want == "" && len(in) > 0:
				t.Errorf("the link is in %q, want no link with nothing due", in)
			case c.want != "" && (len(in) != 1 || in[0] != c.want):
				t.Errorf("the link is in %q, want it in %q alone", in, c.want)
			}
		})
	}
}

func TestToday_RemindMeLaterReadsTheTimeOfAWaitingReminder(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"a reminder later today", thursday.Add(2 * time.Hour), "Reminder at 11:00am"},
		{"a reminder already sent", thursday.Add(-time.Hour), "Remind me later"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.offerRemindLater(t)
			f.remindAt(t, c.at)

			link := remindLaterIn(f.show(t))

			if got := link.text(); got != c.want {
				t.Errorf("the link reads %q, want %q", got, c.want)
			}
			if got := link.attr("href"); got != remindLaterPath {
				t.Errorf("the link points at %q, want %q", got, remindLaterPath)
			}
		})
	}
}

func TestToday_RemindMeLaterIsNotOfferedInTheLastFiveMinutesOfTheDayUnlessAReminderIsWaiting(t *testing.T) {
	// 22:54 UTC is 23:54 in London.
	beforeCutoff := time.Date(2026, time.September, 3, 22, 54, 0, 0, time.UTC)
	late := time.Date(2026, time.September, 3, 22, 56, 0, 0, time.UTC)
	cases := []struct {
		name    string
		now     time.Time
		waiting bool
		shown   bool
	}{
		{"at 23:54 with no reminder", beforeCutoff, false, true},
		{"at 23:56 with no reminder", late, false, false},
		{"at 23:56 with a reminder at 23:58", late, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.offerRemindLater(t)
			f.handler.now = func() time.Time { return c.now }
			if c.waiting {
				f.remindAt(t, c.now.Add(2*time.Minute))
			}

			if link := remindLaterIn(f.show(t)); (link != nil) != c.shown {
				t.Errorf("the link is shown = %v, want %v", link != nil, c.shown)
			}
		})
	}
}

func TestToday_AWaitingReminderIsShownBeforeTodaysDigestIsSent(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.subscribe(t)
	// The reader set the reminder with the digest off, then switched the
	// digest on before its hour.
	f.dailyDigest(t, true)
	f.remindAt(t, thursday.Add(2*time.Hour))

	link := remindLaterIn(f.show(t))

	if link == nil {
		t.Fatal("the link is not shown")
	}
	if got, want := link.text(), "Reminder at 11:00am"; got != want {
		t.Errorf("the link reads %q, want %q", got, want)
	}
}

func TestToday_RemindMeLaterIsNotOfferedForADigestSentOnlyInTheReadersOtherGarden(t *testing.T) {
	f := rosewood(t)
	f.handler.pushKey = testPushKey
	f.subscribe(t)
	f.dailyDigest(t, true)
	f.fairview(t)
	f.exec(t, "INSERT INTO notification_send (membership_id, kind, send_key) VALUES ($1, 'digest', '2026-09-03')", otherMembershipID)

	if link := remindLaterIn(f.show(t)); link != nil {
		t.Errorf("the link is shown on Rosewood for Fairview's digest:\n%s", link)
	}
}

func TestRemindLater_TheSheetOffersTheDelaysAndATimeFiveMinutesFromNow(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)

	rec := f.openRemindLater(t, true)

	wantStatus(t, rec, http.StatusOK)
	sheet := remindLaterDialog(rec.Body.String())
	if sheet == nil {
		t.Fatalf("the response has no Remind me later sheet:\n%s", rec.Body.String())
	}
	when := sheet.first(isTag("fieldset"))
	if got, want := chipLabels(when), []string{"In 1 hour", "In 2 hours", "At a time"}; !slices.Equal(got, want) {
		t.Errorf("the When chips are %q, want %q", got, want)
	}
	if got := checkedValue(when); got != "1h" {
		t.Errorf("the chosen chip is %q, want %q", got, "1h")
	}
	// 08:00 UTC is 09:00 in London.
	if got := sheet.first(attrIs("name", timeField)).attr("value"); got != "09:05" {
		t.Errorf("the time input starts at %q, want %q", got, "09:05")
	}
	if sheet.first(attrIs("name", rescheduleField)) != nil {
		t.Error("the sheet reschedules with no reminder waiting")
	}
	if sheet.first(isTag("button"), textIs("Remove reminder")) != nil {
		t.Error("the sheet offers Remove reminder with no reminder waiting")
	}
	if sheet.first(isTag("button"), textIs("Remind me")) == nil {
		t.Errorf("the sheet has no Remind me button:\n%s", sheet)
	}
}

func TestRemindLater_TheSheetWithAReminderWaitingOffersRescheduleAndRemove(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	f.remindAt(t, thursday.Add(5*time.Hour+30*time.Minute))

	sheet := remindLaterDialog(f.openRemindLater(t, true).Body.String())

	if got, want := sheet.first(isTag("p")).text(), "You’ll get a reminder at 2:30pm."; got != want {
		t.Errorf("the sheet says %q, want %q", got, want)
	}
	if got := checkedValue(sheet.first(isTag("fieldset"))); got != whenTime {
		t.Errorf("the chosen chip is %q, want At a time", got)
	}
	if got := sheet.first(attrIs("name", timeField)).attr("value"); got != "14:30" {
		t.Errorf("the time input holds %q, want the reminder's %q", got, "14:30")
	}
	if sheet.first(attrIs("name", rescheduleField)) == nil {
		t.Error("the form has no reschedule field")
	}
	if sheet.first(isTag("button"), textIs("Reschedule")) == nil {
		t.Errorf("the sheet has no Reschedule button:\n%s", sheet)
	}
	if remove := sheet.first(isTag("button"), textIs("Remove reminder")); remove == nil || remove.attr("formaction") != removeReminderPath {
		t.Errorf("Remove reminder does not post to %q:\n%s", removeReminderPath, sheet)
	}
}

func TestRemindLater_ADelayEndingAfterMidnightIsNotOffered(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	// 21:30 UTC is 22:30 in London, so two hours on is tomorrow.
	f.handler.now = func() time.Time { return time.Date(2026, time.September, 3, 21, 30, 0, 0, time.UTC) }

	sheet := remindLaterDialog(f.openRemindLater(t, true).Body.String())

	if got, want := chipLabels(sheet.first(isTag("fieldset"))), []string{"In 1 hour", "At a time"}; !slices.Equal(got, want) {
		t.Errorf("the When chips are %q, want %q", got, want)
	}
}

func TestRemindLater_WithoutJavaScriptTheSheetOpensOverToday(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)

	rec := f.openRemindLater(t, false)

	wantStatus(t, rec, http.StatusOK)
	page := rec.Body.String()
	if remindLaterDialog(page) == nil {
		t.Errorf("the page has no Remind me later sheet:\n%s", page)
	}
	if _, order := sectionsOf(page); len(order) == 0 {
		t.Errorf("the day's sections are not on the page:\n%s", page)
	}
}

func TestRemindLater_EveryRouteIsNotFoundWhenTodayHasNoRemindMeLaterLink(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, f *todayFixture)
	}{
		{"push off", func(*testing.T, *todayFixture) {}},
		{"today's digest not sent yet", func(t *testing.T, f *todayFixture) {
			f.handler.pushKey = testPushKey
			f.subscribe(t)
			f.dailyDigest(t, true)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			c.setup(t, f)

			if rec := f.openRemindLater(t, true); rec.Code != http.StatusNotFound {
				t.Errorf("GET status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {"1h"}}, true); rec.Code != http.StatusNotFound {
				t.Errorf("POST status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if rec := f.postReminder(t, removeReminderPath, nil, true); rec.Code != http.StatusNotFound {
				t.Errorf("Remove status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if got := f.storedReminder(t, rosewoodMembershipID); !got.IsZero() {
				t.Errorf("a reminder was set, %s, want none", got.UTC())
			}
		})
	}
}

func TestRemindLater_ADelaySetsTheReminderThatFarFromNowAndWakesTheJob(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	calls := 0
	f.handler.wake = countingWake(&calls)

	rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {"2h"}}, true)

	wantStatus(t, rec, http.StatusOK)
	if got, want := f.storedReminder(t, rosewoodMembershipID), thursday.Add(2*time.Hour); !got.Equal(want) {
		t.Errorf("the reminder is at %s, want %s", got.UTC(), want)
	}
	if calls != 1 {
		t.Errorf("the job was woken %d times, want once", calls)
	}
	body := rec.Body.String()
	// 08:00 UTC is 09:00 in London, so two hours on is 11:00am.
	if got, want := remindLaterIn(body).text(), "Reminder at 11:00am"; got != want {
		t.Errorf("the swapped link reads %q, want %q", got, want)
	}
	if closed := readHTML(body).byID(sheetID); closed == nil || closed.attr("hx-swap-oob") != "true" || closed.tag != "div" {
		t.Errorf("the response does not close the sheet:\n%s", body)
	}
	if got, want := announcement(body), "You’ll get a reminder at 11:00am."; got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestRemindLater_ATimeIsInTheReadersTimezone(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)

	rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {whenTime}, timeField: {"14:30"}}, true)

	wantStatus(t, rec, http.StatusOK)
	// 14:30 in London on 3 September is 13:30 UTC.
	if got, want := f.storedReminder(t, rosewoodMembershipID), thursday.Add(5*time.Hour+30*time.Minute); !got.Equal(want) {
		t.Errorf("the reminder is at %s, want %s", got.UTC(), want)
	}
}

func TestRemindLater_TheTimeTheInputStartsAtIsAccepted(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	sheet := remindLaterDialog(f.openRemindLater(t, true).Body.String())
	start := sheet.first(attrIs("name", timeField)).attr("value")

	rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {whenTime}, timeField: {start}}, true)

	wantStatus(t, rec, http.StatusOK)
	if got, want := f.storedReminder(t, rosewoodMembershipID), thursday.Add(remindLead); !got.Equal(want) {
		t.Errorf("the reminder is at %s, want %s", got.UTC(), want)
	}
}

func TestRemindLater_ARefusedTimeSetsNothingAndKeepsWhatWasTyped(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		form url.Values
		want remindRefused
	}{
		{"a time earlier today", thursday, url.Values{whenField: {whenTime}, timeField: {"08:30"}}, timePassed},
		{"no time", thursday, url.Values{whenField: {whenTime}, timeField: {""}}, timeMissing},
		// 21:30 UTC is 22:30 in London, so two hours on is tomorrow.
		{"a delay ending after midnight", time.Date(2026, time.September, 3, 21, 30, 0, 0, time.UTC), url.Values{whenField: {"2h"}, timeField: {"22:35"}}, timeTomorrow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.offerRemindLater(t)
			f.handler.now = func() time.Time { return c.now }
			calls := 0
			f.handler.wake = countingWake(&calls)

			rec := f.postReminder(t, remindLaterPath, c.form, true)

			wantStatus(t, rec, http.StatusUnprocessableEntity)
			if got := rec.Header().Get("HX-Retarget"); got != "#"+sheetID {
				t.Errorf("HX-Retarget = %q, want %q", got, "#"+sheetID)
			}
			sheet := remindLaterDialog(rec.Body.String())
			if !strings.Contains(sheet.text(), string(c.want)) {
				t.Errorf("the sheet does not say %q:\n%s", c.want, sheet.text())
			}
			if got := sheet.first(attrIs("name", timeField)).attr("value"); got != c.form.Get(timeField) {
				t.Errorf("the time input reads %q, want the %q that was posted", got, c.form.Get(timeField))
			}
			if got := f.storedReminder(t, rosewoodMembershipID); !got.IsZero() {
				t.Errorf("a reminder was set, %s, want none", got.UTC())
			}
			if calls != 0 {
				t.Errorf("the job was woken %d times, want not at all", calls)
			}
		})
	}
}

func TestRemindLater_AWhenTheSheetDoesNotOfferIsABadRequest(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)

	rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {"3h"}}, true)

	wantStatus(t, rec, http.StatusBadRequest)
	if got := f.storedReminder(t, rosewoodMembershipID); !got.IsZero() {
		t.Errorf("a reminder was set, %s, want none", got.UTC())
	}
}

func TestRemindLater_ASecondReminderIsRefusedWhileOneIsWaiting(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	waiting := thursday.Add(5 * time.Hour)
	f.remindAt(t, waiting)

	rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {"1h"}}, true)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if got := f.storedReminder(t, rosewoodMembershipID); !got.Equal(waiting) {
		t.Errorf("the reminder is at %s, want it left at %s", got.UTC(), waiting)
	}
	sheet := remindLaterDialog(rec.Body.String())
	if !strings.Contains(sheet.text(), string(reminderWaiting)) {
		t.Errorf("the sheet does not say %q:\n%s", reminderWaiting, sheet.text())
	}
	if sheet.first(isTag("button"), textIs("Reschedule")) == nil {
		t.Errorf("the sheet does not offer Reschedule:\n%s", sheet)
	}
}

func TestRemindLater_RescheduleMovesTheWaitingReminder(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	f.remindAt(t, thursday.Add(5*time.Hour))

	rec := f.postReminder(t, remindLaterPath, url.Values{rescheduleField: {"1"}, whenField: {"1h"}}, true)

	wantStatus(t, rec, http.StatusOK)
	if got, want := f.storedReminder(t, rosewoodMembershipID), thursday.Add(time.Hour); !got.Equal(want) {
		t.Errorf("the reminder is at %s, want %s", got.UTC(), want)
	}
	if got, want := remindLaterIn(rec.Body.String()).text(), "Reminder at 10:00am"; got != want {
		t.Errorf("the swapped link reads %q, want %q", got, want)
	}
}

func TestRemindLater_RescheduleIsRefusedOnceTheReminderHasBeenSent(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	// The sheet was opened with a reminder waiting, and the job sent it and
	// cleared it before Reschedule was pressed.
	at := thursday.Add(5 * time.Hour)
	f.principal.Membership.RemindAgainAt = &at

	rec := f.postReminder(t, remindLaterPath, url.Values{rescheduleField: {"1"}, whenField: {"1h"}}, true)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if got := f.storedReminder(t, rosewoodMembershipID); !got.IsZero() {
		t.Errorf("a reminder was set, %s, want none", got.UTC())
	}
	sheet := remindLaterDialog(rec.Body.String())
	if !strings.Contains(sheet.text(), string(reminderSent)) {
		t.Errorf("the sheet does not say %q:\n%s", reminderSent, sheet.text())
	}
	if sheet.first(isTag("button"), textIs("Remind me")) == nil {
		t.Errorf("the sheet does not offer Remind me:\n%s", sheet)
	}
}

func TestRemindLater_RemoveClearsTheWaitingReminder(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	f.remindAt(t, thursday.Add(5*time.Hour))

	rec := f.postReminder(t, removeReminderPath, nil, true)

	wantStatus(t, rec, http.StatusOK)
	if got := f.storedReminder(t, rosewoodMembershipID); !got.IsZero() {
		t.Errorf("the reminder is still at %s, want none", got.UTC())
	}
	body := rec.Body.String()
	if got, want := remindLaterIn(body).text(), "Remind me later"; got != want {
		t.Errorf("the swapped link reads %q, want %q", got, want)
	}
	if got, want := announcement(body), "Reminder removed."; got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestRemindLater_RemoveIsRefusedOnceTheReminderHasBeenSent(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	calls := 0
	f.handler.wake = countingWake(&calls)
	// The sheet was opened with a reminder waiting, and the job sent it and
	// cleared it before Remove reminder was pressed.
	at := thursday.Add(5 * time.Hour)
	f.principal.Membership.RemindAgainAt = &at

	rec := f.postReminder(t, removeReminderPath, nil, true)

	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if got := rec.Header().Get("HX-Retarget"); got != "#"+sheetID {
		t.Errorf("HX-Retarget = %q, want %q", got, "#"+sheetID)
	}
	sheet := remindLaterDialog(rec.Body.String())
	if !strings.Contains(sheet.text(), string(reminderSent)) {
		t.Errorf("the sheet does not say %q:\n%s", reminderSent, sheet.text())
	}
	if sheet.first(isTag("button"), textIs("Remind me")) == nil {
		t.Errorf("the sheet does not offer Remind me:\n%s", sheet)
	}
	if calls != 0 {
		t.Errorf("the job was woken %d times, want not at all", calls)
	}
}

func TestRemindLater_APostWithoutHTMXRedirectsToToday(t *testing.T) {
	for _, path := range []string{remindLaterPath, removeReminderPath} {
		t.Run(path, func(t *testing.T) {
			f := rosewood(t)
			f.offerRemindLater(t)
			if path == removeReminderPath {
				f.remindAt(t, thursday.Add(5*time.Hour))
			}

			rec := f.postReminder(t, path, url.Values{whenField: {"1h"}}, false)

			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
				t.Errorf("status = %d, Location = %q, want %d to %q", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath)
			}
		})
	}
}

// fairview puts the reader in a second garden, Fairview. The session stays on
// Rosewood.
func (f *todayFixture) fairview(t *testing.T) {
	t.Helper()
	ownedGarden{
		id:           otherGardenID,
		name:         "Fairview",
		owner:        store.AppUser{ID: readerID},
		ownerExists:  true,
		membershipID: otherMembershipID,
	}.insert(t, f.tx)
}

func TestRemindLater_TheReminderIsSetOnlyOnTheGardenTheSessionIsOn(t *testing.T) {
	f := rosewood(t)
	f.offerRemindLater(t)
	f.fairview(t)

	rec := f.postReminder(t, remindLaterPath, url.Values{whenField: {"1h"}}, true)

	wantStatus(t, rec, http.StatusOK)
	if got, want := f.storedReminder(t, rosewoodMembershipID), thursday.Add(time.Hour); !got.Equal(want) {
		t.Errorf("Rosewood's reminder is at %s, want %s", got.UTC(), want)
	}
	if got := f.storedReminder(t, otherMembershipID); !got.IsZero() {
		t.Errorf("Fairview's reminder is at %s, want none", got.UTC())
	}
}

func TestRemindLater_RescheduleAndRemoveLeaveTheReadersOtherGardenAlone(t *testing.T) {
	cases := []struct {
		name string
		path string
		form url.Values
	}{
		{"Reschedule", remindLaterPath, url.Values{rescheduleField: {"1"}, whenField: {"1h"}}},
		{"Remove reminder", removeReminderPath, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.offerRemindLater(t)
			f.fairview(t)
			f.remindAt(t, thursday.Add(5*time.Hour))
			fairviews := thursday.Add(6 * time.Hour)
			f.exec(t, "UPDATE membership SET remind_again_at = $1 WHERE id = $2", fairviews, otherMembershipID)

			wantStatus(t, f.postReminder(t, c.path, c.form, true), http.StatusOK)

			if got := f.storedReminder(t, otherMembershipID); !got.Equal(fairviews) {
				t.Errorf("Fairview's reminder is at %s, want it left at %s", got.UTC(), fairviews)
			}
		})
	}
}
