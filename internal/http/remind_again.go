package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/store"
)

const (
	// RemindAgainPath is the URL the Remind me again banner on Today and the
	// digest notification's In 1 hour and In 2 hours buttons post to.
	RemindAgainPath = "/remind-again"

	// digestParam is the query parameter the digest notification's URL adds
	// to Today. Today shows the Remind me again banner while it is present.
	digestParam = "from"
	digestValue = "digest"

	// DigestPath is the URL a digest notification opens: Today with the
	// Remind me again banner.
	DigestPath = todayPath + "?" + digestParam + "=" + digestValue

	// atField is the name of the banner's time input.
	atField = "at"
)

// remindAgainBanner is the data the Remind me again banner on Today renders
// from. It shows the delay buttons and the time input while Set is empty,
// and the time alone once Set is filled.
type remindAgainBanner struct {
	// ID is the HTML id on the banner's outer div. htmx swaps the banner
	// under it.
	ID string
	// Param is the query parameter the page's script removes from the URL.
	Param string
	// Action is the URL the form posts to. Field and At are the names of the
	// delay buttons and the time input, and Delays holds the buttons' values
	// and labels.
	Action string
	Field  string
	Delays []push.Delay
	At     string
	// Now is the value the time input starts at, five minutes from now in
	// the reader's timezone as "15:04".
	Now string
	// Dismiss is the URL the Dismiss link points at: Today, without the
	// query string that shows the banner. With JavaScript the link hides the
	// banner instead.
	Dismiss string
	// Error is the message under the time input after a refused time. It is
	// empty otherwise.
	Error string
	// Set is the time the notification will be sent again, in the reader's
	// timezone as "2:30pm". It is empty when no resend is waiting.
	Set string
}

const remindAgainID = "remind-again"

// remindAgainWords is the template for the banner's line once a time is set.
// The banner renders it and so does the announcement sent with the swap, so
// the two say the same thing.
const remindAgainWords = "remind-again-words"

// newRemindAgainBanner returns the banner with the delay buttons and a time
// input starting at now.
func newRemindAgainBanner(now time.Time) *remindAgainBanner {
	return &remindAgainBanner{
		ID:      remindAgainID,
		Param:   digestParam,
		Action:  RemindAgainPath,
		Field:   push.DelayField,
		Delays:  push.Delays,
		At:      atField,
		Now:     now.Add(5 * time.Minute).Format(clockLayout),
		Dismiss: todayPath,
	}
}

// remindAgainFor returns the banner Today shows, or nil for no banner. The
// banner is shown only on the URL the digest notification opens. It is not
// shown with the daily digest switched off, because the job sends to nobody
// who has it off and a time set would never be used.
func (h *today) remindAgainFor(r *http.Request, g gardenDay) *remindAgainBanner {
	if !g.digestOn || r.URL.Query().Get(digestParam) != digestValue {
		return nil
	}
	now := g.now
	banner := newRemindAgainBanner(now)
	if set := PrincipalFrom(r).Membership.RemindAgainAt; set != nil && set.After(now) {
		banner.Set = clockWord(*set, now)
	}
	return banner
}

// remindRefused is an error whose text is shown under the time input when
// the time cannot be set.
type remindRefused string

func (r remindRefused) Error() string { return string(r) }

// errDelayNotOffered is returned when a post names a delay that is not one of
// the banner's buttons.
var errDelayNotOffered = errors.New("the banner did not offer that delay")

// remindAgainAt returns the instant a post asks for: now plus the delay, or
// the time read on the clock on now's day in now's timezone. A time before
// the minute now falls in returns a remindRefused. The comparison is by the
// minute so that the time the input starts at is accepted.
func remindAgainAt(delay, at string, now time.Time) (time.Time, error) {
	if delay != "" {
		d, ok := push.DelayFor(delay)
		if !ok {
			return time.Time{}, errDelayNotOffered
		}
		return now.Add(d.Duration), nil
	}
	clock, err := time.Parse(clockLayout, at)
	if err != nil {
		return time.Time{}, remindRefused("Enter a time.")
	}
	chosen := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if chosen.Before(now.Truncate(time.Minute)) {
		return time.Time{}, remindRefused("That time has already passed.")
	}
	return chosen, nil
}

// remindAgain sets the time today's digest is sent again and wakes the job.
// With htmx it renders the banner showing that time. A plain post redirects
// to Today at the notification's URL. A refused time renders the banner with
// the message under the input, or the whole page without htmx.
//
// The route is 404 with push off or the daily digest switched off, because
// Today offers no banner in either case.
func (h *today) remindAgain(w http.ResponseWriter, r *http.Request) {
	if h.pushKey == "" {
		h.templates.notFound(w, r)
		return
	}
	principal := PrincipalFrom(r)
	preferences, err := h.queries.ListNotificationPreferences(r.Context(), principal.Membership.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "read the notification preferences", err)
		return
	}
	if !notificationOn(preferences, digestKind) {
		h.templates.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	now := h.now().In(locationFor(principal.User))
	at, err := remindAgainAt(r.PostForm.Get(push.DelayField), r.PostForm.Get(atField), now)
	var refused remindRefused
	switch {
	case errors.As(err, &refused):
		banner := newRemindAgainBanner(now)
		banner.Error = string(refused)
		// The input keeps the time that was typed rather than starting at
		// now again.
		if typed := r.PostForm.Get(atField); typed != "" {
			banner.Now = typed
		}
		if isHTMX(r) {
			h.templates.render(w, r, view{page: "today", fragment: "remind-again", status: http.StatusUnprocessableEntity, announce: banner.Error}, banner)
			return
		}
		g, err := h.load(r.Context(), principal)
		if err != nil {
			h.templates.serverError(h.logger, w, r, "load the day", err)
			return
		}
		page := newTodayPage(principal, g)
		page.RemindAgain = banner
		h.templates.render(w, r, view{page: "today", status: http.StatusUnprocessableEntity}, page)
		return
	case err != nil:
		h.templates.badRequest(w, r)
		return
	}

	err = h.queries.SetRemindAgain(r.Context(), store.SetRemindAgainParams{
		RemindAgainAt: &at,
		GardenID:      principal.Garden.ID,
		UserID:        principal.User.ID,
	})
	if err != nil {
		h.templates.serverError(h.logger, w, r, "set the reminder", err)
		return
	}
	h.wake.call()
	if isHTMX(r) {
		banner := newRemindAgainBanner(now)
		banner.Set = clockWord(at, now)
		announce := h.templates.sentence("today", remindAgainWords, banner.Set)
		h.templates.render(w, r, view{page: "today", fragment: "remind-again", announce: announce}, banner)
		return
	}
	http.Redirect(w, r, DigestPath, http.StatusSeeOther)
}
