package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

// The two rows notification_kind holds.
const (
	digestKind   = "digest"
	activityKind = "activity"
)

func removeBrowserPath(subscriptionID uuid.UUID) string {
	return notificationsPath + "/browsers/" + subscriptionID.String() + "/remove"
}

type notificationsPage struct {
	Action string
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
}

// browserRow is one push subscription. A subscription belongs to a browser
// rather than to a person, and a dead one is invisible everywhere else in the
// app: the symptom is notifications that stop on one device with nothing on
// any screen to say so.
type browserRow struct {
	Name string
	Used string
	// Remove is the URL the row's Remove button posts to.
	Remove string
}

func (h *more) notifications(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	preferences, err := h.queries.ListNotificationPreferences(r.Context(), principal.Membership.ID)
	if err != nil {
		serverError(h.logger, w, r, "read the notification preferences", err)
		return
	}
	subscriptions, err := h.queries.ListPushSubscriptions(r.Context(), principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the push subscriptions", err)
		return
	}

	now := h.now().In(locationFor(principal.User))
	page := newNotificationsPage(principal.User.Timezone, notificationOn(preferences, digestKind),
		notificationOn(preferences, activityKind), principal.Membership.DigestHour, subscriptions, now)
	h.templates.render(w, r, view{page: "notifications"}, page)
}

// saveNotifications writes both types and the digest's hour. The hour is
// absent from the form when the digest is off, and the stored hour is kept.
func (h *more) saveNotifications(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	hour := principal.Membership.DigestHour
	if r.PostForm.Has("hour") {
		posted, ok := offeredHour(r.PostForm.Get("hour"))
		if !ok {
			http.Error(w, "the form did not offer that", http.StatusBadRequest)
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
		serverError(h.logger, w, r, "save the notification settings", err)
		return
	}
	http.Redirect(w, r, notificationsPath, http.StatusSeeOther)
}

// removeBrowser deletes one push subscription. Nothing is sent to that
// browser afterwards.
func (h *more) removeBrowser(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	subscriptionID, err := uuid.Parse(r.PathValue("browser"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	removed, err := h.queries.DeletePushSubscription(r.Context(), principal.User.ID, subscriptionID)
	if err != nil {
		serverError(h.logger, w, r, "remove the browser", err)
		return
	}
	// Another account's subscription and one already removed are both 404,
	// since neither was a button this page offered.
	if removed == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, notificationsPath, http.StatusSeeOther)
}

func newNotificationsPage(zone string, digest, activity bool, hour int16, subscriptions []store.PushSubscription, now time.Time) notificationsPage {
	page := notificationsPage{
		Action:   notificationsPath,
		Digest:   digest,
		Activity: activity,
		Zone:     zoneLabel(zone),
		Account:  accountPath,
	}
	if digest {
		for _, choice := range hours() {
			page.Hours = append(page.Hours, option{Value: strconv.Itoa(int(choice)), Label: hourLabel(choice), On: choice == hour})
		}
	}
	for _, subscription := range subscriptions {
		page.Browsers = append(page.Browsers, browserRow{
			Name:   browserName(subscription.UserAgent),
			Used:   usedNote(subscription.LastSentAt, now),
			Remove: removeBrowserPath(subscription.ID),
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

// browserName labels a subscription's row, as "Mac · Chrome". The User-Agent
// is everything a subscription carries and it names no model, so a MacBook Air
// is a Mac. A string that names neither device nor browser is "Unknown
// browser", and its date still tells the row from the others.
func browserName(userAgent *string) string {
	if userAgent == nil {
		return "Unknown browser"
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
		return "Unknown browser"
	}
	return strings.Join(parts, " · ")
}
