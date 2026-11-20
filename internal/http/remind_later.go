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

// The names of the templates for the sentence saying when the next
// notification is sent: digestAtWords while today's digest is unsent, and
// remindLaterWords for a waiting reminder otherwise. The announcement after a
// reminder is set or removed renders them too.
const (
	remindLaterWords = "remind-later-words"
	digestAtWords    = "digest-at-words"
)

// remindLaterFacts holds what Today checks before offering Remind me later,
// other than the reminder itself. It is read only with push on.
type remindLaterFacts struct {
	hasBrowser bool
	digestOn   bool
	// digestSent is true when notification_send has today's digest for the
	// reader.
	digestSent bool
	// digestAt is the instant of the reader's digest hour today.
	digestAt time.Time
}

// remindLater holds the values the Remind me later link and sheet are built
// from.
type remindLater struct {
	// now is in the reader's timezone.
	now time.Time
	// waiting is the time of the reminder not yet sent, or nil.
	waiting *time.Time
	// digest is the instant of today's digest hour while today's digest is
	// still unsent, or nil. While it is set, a reminder moves today's digest
	// to the reminder's time.
	digest *time.Time
}

// remindLaterState returns the state the Remind me later link and sheet are
// built from, with at as the reader's reminder. It returns nil when Today does
// not offer Remind me later.
//
// It is offered while something is overdue or due today and the reader has a
// subscribed browser. With no reminder waiting, it stops being offered when
// the time input would start tomorrow, because a reminder is for today.
//
// The digest field is set while the digest is on, notification_send has no row
// for today, and either the digest hour is still to come or a reminder is
// waiting. Past the hour with no reminder waiting it is nil, because the job
// sends the digest at that hour.
func (g gardenDay) remindLaterState(at *time.Time) *remindLater {
	facts := g.remindLater
	if facts == nil || !facts.hasBrowser || len(g.day.Overdue)+len(g.day.DueToday) == 0 {
		return nil
	}
	state := &remindLater{now: g.now}
	waiting := at != nil && at.After(g.now)
	if facts.digestOn && !facts.digestSent && (waiting || g.now.Before(facts.digestAt)) {
		state.digest = &facts.digestAt
	}
	if waiting {
		state.waiting = at
		return state
	}
	if !sameDate(g.now.Add(remindLead), g.now) {
		return nil
	}
	return state
}

// digestPassed reports whether today's digest is not yet sent and its hour
// has gone by.
func (s *remindLater) digestPassed() bool {
	return s.digest != nil && !s.now.Before(*s.digest)
}

// words returns the sentence saying when the waiting reminder, or the digest it
// moved, is sent. at is the reminder's time, or the digest's own hour once a
// reminder that moved it is removed.
func (s *remindLater) words(templates *Templates, at time.Time) string {
	name := remindLaterWords
	if s.digest != nil {
		name = digestAtWords
	}
	return templates.sentence("today", name, clockWord(at, s.now))
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
	// At is the time the link shows, "2:30pm": the waiting reminder's, or the
	// digest hour when Digest is true and no reminder is waiting. The link
	// reads Remind me later when At is empty.
	At string
	// Digest is true while today's digest is still unsent. The link then
	// reads Digest at At.
	Digest bool
}

func (s *remindLater) link() *remindLaterLink {
	if s == nil {
		return nil
	}
	link := &remindLaterLink{ID: remindLaterID, Href: remindLaterPath, Digest: s.digest != nil}
	switch {
	case s.waiting != nil:
		link.At = clockWord(*s.waiting, s.now)
	case s.digest != nil:
		link.At = clockWord(*s.digest, s.now)
	}
	return link
}

type remindLaterSheet struct {
	// Path is the URL the form posts to. Target is the id of the link the
	// form's swap replaces.
	Path   string
	Target string
	// Set is the time of the waiting reminder, "2:30pm". Remove is the URL
	// the button that clears the reminder posts to. Both are empty when no
	// reminder is waiting.
	Set    string
	Remove string
	// Digest is the time of today's digest hour, "8:00am", while today's
	// digest is still unsent. It is empty otherwise. Passed is true once that
	// hour has gone by. A post to Remove then also sends the digest.
	Digest string
	Passed bool
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
	if s.digest != nil {
		sheet.Digest = clockWord(*s.digest, s.now)
		sheet.Passed = s.digestPassed()
	}
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
	h.renderRemindLater(w, r, g, state.sheet(), "remind-later-sheet", http.StatusOK)
}

// renderRemindLater renders sheet. An htmx request gets the fragment alone:
// the dialog, or the form inside it for a refusal. Any other request gets Today
// with the sheet open over it.
func (h *today) renderRemindLater(w http.ResponseWriter, r *http.Request, g gardenDay, sheet *remindLaterSheet, fragment string, status int) {
	page := todayPage{}
	if !isHTMX(r) {
		page = newTodayPage(PrincipalFrom(r), g)
	}
	page.RemindLaterSheet = sheet
	h.templates.render(w, r, view{page: "today", fragment: fragment, status: status, announce: sheet.Error}, page)
}

// refuseRemindLater renders the sheet's form again with its error and a 422.
func (h *today) refuseRemindLater(w http.ResponseWriter, r *http.Request, g gardenDay, sheet *remindLaterSheet) {
	refuseOnSheet(w)
	h.renderRemindLater(w, r, g, sheet, "remind-later-form", http.StatusUnprocessableEntity)
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
	// The state is built again with the new reminder, because past the digest
	// hour the digest field is set only while a reminder is waiting.
	set := g.remindLaterState(&at)
	h.reminderChanged(w, r, set.link(), set.words(h.templates, at))
}

// removeReminder handles POST /remind-later/remove. It clears the waiting
// reminder. Removing one the job has already sent is refused, with the sheet
// rendered again as having no reminder waiting.
//
// A reminder that moved today's digest is removed the same way. The job then
// sends the digest at its hour. Once that hour has passed, the digest is
// sent here instead, because the job skips a digest more than an hour past its
// hour.
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
	if state.digestPassed() {
		err := h.sendDigest.call(r.Context(), principal.Membership, principal.User, principal.Garden)
		h.wake.call()
		if err != nil {
			h.templates.serverError(h.logger, w, r, "send the digest", err)
			return
		}
		facts := *g.remindLater
		facts.digestSent = true
		g.remindLater = &facts
		h.reminderChanged(w, r, g.remindLaterState(nil).link(), "Digest sent.")
		return
	}
	h.wake.call()
	announce := "Reminder removed."
	if state.digest != nil {
		announce = state.words(h.templates, *state.digest)
	}
	h.reminderChanged(w, r, g.remindLaterState(nil).link(), announce)
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
