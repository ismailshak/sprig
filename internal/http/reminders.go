package http

import (
	"fmt"
	"net/http"
)

// remindersPath is the URL of the Reminders page, the last step of setting up
// a garden and of joining one through an invite link. Both redirect here after
// signing the new account in. The page offers to turn on the daily digest in
// this browser. Not now goes to Today.
const remindersPath = setupPath + "/reminders"

// remindersPage is the data for /setup/reminders. The page has two blocks and
// the script shows one of them: the offer, or the iPhone's install steps in a
// browser with no push API.
type remindersPage struct {
	// Key is the VAPID public key the browser subscribes with. Subscribe is
	// the URL the script posts the subscription to.
	Key       string
	Subscribe string
	// Digest is the sentence saying what the daily digest is and the hour it
	// arrives.
	Digest string
	// Notifications is the URL the Turn on notifications link points at. With
	// JavaScript the press subscribes this browser and opens Today. Without
	// it the link opens the Notifications page under More.
	Notifications string
	// Today is the URL the Not now and Continue links point at.
	Today string
	// Steps are the iPhone's install steps, shown in place of the offer when
	// the browser has no push API.
	Steps []string
}

// remindersOffer is the data for the banner on Today. It is rendered hidden.
// The script shows it when the app is opened installed, this browser has no
// push subscription, permission has not been refused and the banner has not
// been dismissed.
type remindersOffer struct {
	Key       string
	Subscribe string
}

// digestSentence says what the digest is and when it arrives, for the
// Reminders page.
func digestSentence(hour int16) string {
	return fmt.Sprintf("One notification a day at %s, listing what’s due. Skipped on days with nothing due.", hourLabel(hour))
}

func newRemindersPage(key string, hour int16) remindersPage {
	return remindersPage{
		Key:           key,
		Subscribe:     subscribePath,
		Digest:        digestSentence(hour),
		Notifications: notificationsPath,
		Today:         todayPath,
		Steps:         iPhonePlatform.steps,
	}
}

// reminders handles GET /setup/reminders. With push off there is nothing to
// turn on, so the browser is sent on to Today. The pages that redirect here do
// not check that themselves, so the step is skipped in one place.
func (h *setup) reminders(w http.ResponseWriter, r *http.Request) {
	if h.pushKey == "" {
		http.Redirect(w, r, todayPath, http.StatusSeeOther)
		return
	}
	h.templates.render(w, r, view{page: "reminders"}, newRemindersPage(h.pushKey, PrincipalFrom(r).Membership.DigestHour))
}
