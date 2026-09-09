package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	turnOnReminders = regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>Turn on notifications</a>`)
	notNow          = regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>Not now</a>`)
)

// reminders opens the Reminders page as an owner whose digest is sent at
// hour, with push on or off.
func (f *setupFixture) reminders(t *testing.T, pushKey string, hour int16) *httptest.ResponseRecorder {
	t.Helper()

	f.handler.pushKey = pushKey
	principal := auth.Principal{
		User:       store.AppUser{DisplayName: "Robin", Handle: "robin", Timezone: "Europe/London"},
		Garden:     store.Garden{Name: "Greenhouse"},
		Membership: store.Membership{Role: "owner", DigestHour: hour},
	}
	ctx := context.WithValue(t.Context(), principalKey, principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, remindersPath, nil)
	rec := httptest.NewRecorder()
	f.handler.reminders(rec, req)
	return rec
}

func TestReminders_TurnOnLinksToTheNotificationsPageWithoutJavaScript(t *testing.T) {
	f := setupOn(t, false)

	rec := f.reminders(t, testPushKey, 19)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	if !strings.Contains(text(page), "One notification a day at 7:00pm") {
		t.Errorf("the page does not say when the digest arrives:\n%s", text(page))
	}
	if m := turnOnReminders.FindStringSubmatch(page); m == nil || m[1] != notificationsPath {
		t.Errorf("Turn on notifications links to %v, want the Notifications page for a browser with no JavaScript:\n%s", m, page)
	}
	if m := notNow.FindStringSubmatch(page); m == nil || m[1] != todayPath {
		t.Errorf("Not now links to %v, want Today:\n%s", m, page)
	}
	if !strings.Contains(text(page), "Add to Home Screen") {
		t.Errorf("the iPhone's install steps are not on the page for the script to show:\n%s", text(page))
	}
	if strings.Contains(page, "nav__item") {
		t.Error("the page renders the tab bar, and the person has not seen Today yet")
	}
}

func TestReminders_WithPushOffTheBrowserIsSentOnToToday(t *testing.T) {
	f := setupOn(t, false)

	rec := f.reminders(t, "", 8)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath)
	}
}
