package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/ismailshak/sprig/internal/store"
)

const (
	// remindLaterPath is the URL of the Remind me later sheet. Its form posts
	// to the same URL.
	remindLaterPath    = "/remind-later"
	removeReminderPath = remindLaterPath + "/remove"

	// remindLaterID is the HTML id of the Remind me later link. The sheet's
	// form replaces the element with this id.
	remindLaterID = "remind-later"

	// remindLead is how far ahead of now the sheet's time input starts.
	remindLead = 5 * time.Minute
)

// The names and values the sheet's form posts. rescheduleField is a hidden
// input the form has while a reminder is waiting. The template and the
// stylesheet write these as literals, so a change here is made there too.
const (
	whenField       = "when"
	whenTime        = "time"
	timeField       = "time"
	rescheduleField = "reschedule"
)

// remindDelay is one of the When chips that sets a reminder a fixed time from
// now.
type remindDelay struct {
	value    string
	label    string
	duration time.Duration
}

var remindDelays = []remindDelay{
	{value: "1h", label: "In 1 hour", duration: time.Hour},
	{value: "2h", label: "In 2 hours", duration: 2 * time.Hour},
}

// remindRefused is an error whose text the sheet shows under When.
type remindRefused string

func (r remindRefused) Error() string { return string(r) }

const (
	reminderSent    remindRefused = "That reminder has already been sent."
	reminderWaiting remindRefused = "You already have a reminder. Reschedule it or remove it."
	timeMissing     remindRefused = "Enter a time."
	timePassed      remindRefused = "That time has already passed."
	timeTomorrow    remindRefused = "Choose a time before midnight."
)

// remindLaterWords is the name of the template for the sheet's line while a
// reminder is waiting. The announcement after a reminder is set uses it too.
const remindLaterWords = "remind-later-words"

// remindLaterFacts holds what Today checks before offering Remind me later,
// other than the reminder itself. It is read only with push on.
type remindLaterFacts struct {
	hasBrowser bool
	digestOn   bool
	// digestSent is true when notification_send has today's digest for the
	// reader.
	digestSent bool
}

// remindLater holds the values the Remind me later link and sheet are built
// from.
type remindLater struct {
	// now is in the reader's timezone.
	now time.Time
	// waiting is the time of the reminder not yet sent, or nil.
	waiting *time.Time
}

// remindLaterState returns the state the Remind me later link and sheet are
// built from, with at as the reader's reminder. It returns nil when Today does
// not offer Remind me later.
//
// It is offered while something is overdue or due today and the reader has a
// subscribed browser. With a reminder waiting, nothing else is checked.
// Otherwise, with the daily digest on, it is offered only once today's digest
// is sent, because before then the member has not had today's list. It stops
// being offered when the time input would start tomorrow, because a reminder
// is for today.
func (g gardenDay) remindLaterState(at *time.Time) *remindLater {
	facts := g.remindLater
	if facts == nil || !facts.hasBrowser || len(g.day.Overdue)+len(g.day.DueToday) == 0 {
		return nil
	}
	state := &remindLater{now: g.now}
	if at != nil && at.After(g.now) {
		state.waiting = at
		return state
	}
	if facts.digestOn && !facts.digestSent {
		return nil
	}
	if !sameDate(g.now.Add(remindLead), g.now) {
		return nil
	}
	return state
}

// sameDate reports whether a and b fall on the same date in b's timezone.
func sameDate(a, b time.Time) bool {
	ay, am, ad := a.In(b.Location()).Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// remindLaterLink is the Remind me later link beside the title of Today's
// first section.
type remindLaterLink struct {
	ID   string
	Href string
	// At is the time of the waiting reminder, "2:30pm". The link reads
	// Remind me later when it is empty.
	At string
}

func (s *remindLater) link() *remindLaterLink {
	if s == nil {
		return nil
	}
	link := &remindLaterLink{ID: remindLaterID, Href: remindLaterPath}
	if s.waiting != nil {
		link.At = clockWord(*s.waiting, s.now)
	}
	return link
}

type remindLaterSheet struct {
	// Path is the URL the form posts to. Target is the id of the link the
	// form's swap replaces.
	Path   string
	Target string
	// Set is the time of the waiting reminder, "2:30pm". Remove is the URL
	// the Remove reminder button posts to. Both are empty when no reminder is
	// waiting.
	Set    string
	Remove string
	// Whens is the When chips: each delay that ends before midnight, then At
	// a time. Time is the time input's value, "15:04".
	Whens []chip
	Time  string
	// Error is the line under the time input. It is empty when nothing was
	// refused.
	Error string
}

// sheet returns the sheet for s. With a reminder waiting, At a time is chosen
// and the time input holds the reminder's time. Otherwise the first chip is
// chosen and the time input starts remindLead from now.
func (s *remindLater) sheet() *remindLaterSheet {
	sheet := &remindLaterSheet{
		Path:   remindLaterPath,
		Target: remindLaterID,
		Time:   s.now.Add(remindLead).Format(clockLayout),
	}
	for _, d := range remindDelays {
		if sameDate(s.now.Add(d.duration), s.now) {
			sheet.Whens = append(sheet.Whens, chip{Value: d.value, Label: d.label})
		}
	}
	sheet.Whens = append(sheet.Whens, chip{Value: whenTime, Label: "At a time"})
	chosen := 0
	if s.waiting != nil {
		sheet.Set = clockWord(*s.waiting, s.now)
		sheet.Remove = removeReminderPath
		sheet.Time = s.waiting.In(s.now.Location()).Format(clockLayout)
		chosen = len(sheet.Whens) - 1
	}
	sheet.Whens[chosen].On = true
	return sheet
}

// choose marks the chip whose value is when as chosen, and no other.
func (s *remindLaterSheet) choose(when string) {
	for i := range s.Whens {
		s.Whens[i].On = s.Whens[i].Value == when
	}
}

// errWhenNotOffered is returned when a post names a When the sheet does not
// offer.
var errWhenNotOffered = errors.New("the sheet did not offer that time")

// reminderAt returns the instant a post asks for: now plus the chosen delay,
// or the posted time on now's date in now's timezone. It returns a
// remindRefused for an empty time, a time before the current minute, and a
// delay that ends tomorrow. The time is compared by the minute so that the
// input's starting value is accepted.
func reminderAt(when, clock string, now time.Time) (time.Time, error) {
	if when == whenTime {
		c, err := time.Parse(clockLayout, clock)
		if err != nil {
			return time.Time{}, timeMissing
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), c.Hour(), c.Minute(), 0, 0, now.Location())
		if at.Before(now.Truncate(time.Minute)) {
			return time.Time{}, timePassed
		}
		return at, nil
	}
	for _, d := range remindDelays {
		if d.value != when {
			continue
		}
		at := now.Add(d.duration)
		if !sameDate(at, now) {
			return time.Time{}, timeTomorrow
		}
		return at, nil
	}
	return time.Time{}, errWhenNotOffered
}

// loadRemindLater loads the day and returns it with the reader's Remind me
// later state. It writes the error response and returns false when the load
// fails or Today does not offer Remind me later.
func (h *today) loadRemindLater(w http.ResponseWriter, r *http.Request) (gardenDay, *remindLater, bool) {
	principal := PrincipalFrom(r)
	g, err := h.load(r.Context(), principal)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the day", err)
		return g, nil, false
	}
	state := g.remindLaterState(principal.Membership.RemindAgainAt)
	if state == nil {
		h.templates.notFound(w, r)
		return g, nil, false
	}
	return g, state, true
}

// showRemindLater handles GET /remind-later.
func (h *today) showRemindLater(w http.ResponseWriter, r *http.Request) {
	g, state, ok := h.loadRemindLater(w, r)
	if !ok {
		return
	}
	h.renderRemindLater(w, r, g, state.sheet(), http.StatusOK)
}

// renderRemindLater renders sheet. An htmx request gets the dialog alone. Any
// other request gets Today with the sheet open over it.
func (h *today) renderRemindLater(w http.ResponseWriter, r *http.Request, g gardenDay, sheet *remindLaterSheet, status int) {
	page := todayPage{}
	if !isHTMX(r) {
		page = newTodayPage(PrincipalFrom(r), g)
	}
	page.RemindLaterSheet = sheet
	h.templates.render(w, r, view{page: "today", fragment: "remind-later-sheet", status: status, announce: sheet.Error}, page)
}

// refuseRemindLater renders the sheet again with its error and a 422. The form
// targets the link, so the response headers retarget it at #sheet.
func (h *today) refuseRemindLater(w http.ResponseWriter, r *http.Request, g gardenDay, sheet *remindLaterSheet) {
	w.Header().Set("HX-Retarget", "#"+sheetID)
	w.Header().Set("HX-Reswap", "outerHTML settle:0ms")
	h.renderRemindLater(w, r, g, sheet, http.StatusUnprocessableEntity)
}

// setReminder handles POST /remind-later. It sets a new reminder, or with the
// reschedule field moves the waiting one. A new reminder is refused while one
// is waiting, because the reader has one at a time. A reschedule is refused
// once the job has sent the reminder. A refusal renders the sheet again from
// the database, with the error.
func (h *today) setReminder(w http.ResponseWriter, r *http.Request) {
	g, state, ok := h.loadRemindLater(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	when, clock := r.PostForm.Get(whenField), r.PostForm.Get(timeField)
	at, err := reminderAt(when, clock, state.now)
	var refused remindRefused
	switch {
	case errors.As(err, &refused):
		sheet := state.sheet()
		sheet.choose(when)
		sheet.Time = clock
		sheet.Error = string(refused)
		h.refuseRemindLater(w, r, g, sheet)
		return
	case err != nil:
		h.templates.badRequest(w, r)
		return
	}

	principal := PrincipalFrom(r)
	var changed int64
	if r.PostForm.Has(rescheduleField) {
		changed, err = h.queries.RescheduleReminder(r.Context(), store.RescheduleReminderParams{
			RemindAgainAt: &at,
			GardenID:      principal.Garden.ID,
			UserID:        principal.User.ID,
			Now:           state.now,
		})
	} else {
		changed, err = h.queries.SetReminder(r.Context(), store.SetReminderParams{
			RemindAgainAt: &at,
			GardenID:      principal.Garden.ID,
			UserID:        principal.User.ID,
			Now:           state.now,
		})
	}
	if err != nil {
		h.templates.serverError(h.logger, w, r, "set the reminder", err)
		return
	}
	if changed == 0 {
		// The sheet was opened before another post or the job changed the
		// reminder, so it is rendered from what the database holds now.
		membership, err := h.queries.GetMembershipWithUserAndGarden(r.Context(), principal.Garden.ID, principal.User.ID)
		if err != nil {
			h.templates.serverError(h.logger, w, r, "read the reminder", err)
			return
		}
		current := g.remindLaterState(membership.Membership.RemindAgainAt)
		if current == nil {
			h.templates.notFound(w, r)
			return
		}
		sheet := current.sheet()
		sheet.Error = string(reminderSent)
		if current.waiting != nil {
			sheet.Error = string(reminderWaiting)
		}
		h.refuseRemindLater(w, r, g, sheet)
		return
	}
	h.wake.call()
	set := &remindLater{now: state.now, waiting: &at}
	announce := h.templates.sentence("today", remindLaterWords, clockWord(at, state.now))
	h.reminderChanged(w, r, set.link(), announce)
}

// removeReminder handles POST /remind-later/remove. It clears the waiting
// reminder. Removing one the job has already sent is refused, with the sheet
// rendered again as having no reminder waiting.
func (h *today) removeReminder(w http.ResponseWriter, r *http.Request) {
	g, state, ok := h.loadRemindLater(w, r)
	if !ok {
		return
	}
	principal := PrincipalFrom(r)
	changed, err := h.queries.RemoveReminder(r.Context(), store.RemoveReminderParams{
		GardenID: principal.Garden.ID,
		UserID:   principal.User.ID,
		Now:      state.now,
	})
	if err != nil {
		h.templates.serverError(h.logger, w, r, "remove the reminder", err)
		return
	}
	if changed == 0 {
		current := g.remindLaterState(nil)
		if current == nil {
			h.templates.notFound(w, r)
			return
		}
		sheet := current.sheet()
		sheet.Error = string(reminderSent)
		h.refuseRemindLater(w, r, g, sheet)
		return
	}
	h.wake.call()
	h.reminderChanged(w, r, g.remindLaterState(nil).link(), "Reminder removed.")
}

// reminderChanged responds to a reminder set, rescheduled or removed. Without
// htmx it redirects to Today. With htmx it renders the link and an empty
// #sheet that closes the dialog. The link is nil when Today no longer offers
// Remind me later. The swap then removes the old one.
func (h *today) reminderChanged(w http.ResponseWriter, r *http.Request, link *remindLaterLink, announce string) {
	if !isHTMX(r) {
		http.Redirect(w, r, todayPath, http.StatusSeeOther)
		return
	}
	h.templates.render(w, r, view{page: "today", fragment: "remind-later-changed", announce: announce}, link)
}
