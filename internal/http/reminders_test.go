package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
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
	page := readHTML(rec.Body.String())
	if !strings.Contains(page.text(), "One notification a day at 7:00pm") {
		t.Errorf("the page does not say when the digest arrives:\n%s", page.text())
	}
	if got := page.first(isTag("a"), textIs("Turn on notifications")).attr("href"); got != notificationsPath {
		t.Errorf("Turn on notifications links to %q, want the Notifications page for a browser with no JavaScript", got)
	}
	if got := page.first(isTag("a"), textIs("Not now")).attr("href"); got != todayPath {
		t.Errorf("Not now links to %q, want Today", got)
	}
	if !strings.Contains(page.text(), "Add to Home Screen") {
		t.Errorf("the iPhone's install steps are not on the page for the script to show:\n%s", page.text())
	}
	if page.first(isTag("nav")) != nil {
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
