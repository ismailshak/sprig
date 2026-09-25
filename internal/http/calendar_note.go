package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// notesPath is the URL a new note is posted to. Every other note route is
// under it.
const notesPath = calendarPath + "/notes"

// noteSheetTemplate is the template that renders the Add note and Edit note
// sheet.
const noteSheetTemplate = "note-sheet"

// noteChangedTemplate is the template for the response to an added, saved or
// deleted note: the month's grid, and an empty #sheet swapped out of band to
// close the sheet.
const noteChangedTemplate = "note-changed"

const (
	noteMissing      = "Enter a note."
	firstDayUnusable = "Enter a first day."
	lastDayUnusable  = "Enter a last day or leave it empty."
	lastDayTooEarly  = "Choose a last day on or after the first."
)

// firstDayTooLate is the error message for a first day after last, the last
// day the calendar shows. A note starting later would be on no page of the
// calendar and could never be edited or deleted.
func firstDayTooLate(last time.Time) string {
	return "Choose a first day on or before " + last.Format("2 January 2006") + "."
}

// lastNoteDay is the last day of the last month the calendar shows. It is the
// latest first day a note may have.
func lastNoteDay(now time.Time) time.Time {
	return lastCalendarMonth(now).AddDate(0, 1, -1)
}

// newNotePath is the URL of the Add note sheet with day as the first day.
func newNotePath(month, day time.Time) string {
	return notesPath + "/new?" + noteQuery(month, &day).Encode()
}

// notePath is the URL of the note's Edit note sheet. The sheet's form posts
// to the same URL.
func notePath(id uuid.UUID, month time.Time) string {
	return notesPath + "/" + id.String() + "?" + noteQuery(month, nil).Encode()
}

// noteQuery is the query string on every note URL. month is the month the
// calendar shows behind the sheet and after a save. day, when not nil, is the
// day the Add note sheet was opened from.
func noteQuery(month time.Time, day *time.Time) url.Values {
	values := url.Values{monthParam: {month.Format(monthFormat)}}
	if day != nil {
		values.Set(dayParam, day.Format(time.DateOnly))
	}
	return values
}

// noteSheet is the Add note or Edit note sheet.
type noteSheet struct {
	// Title is "Add note" or "Edit note".
	Title string
	// Path is the URL the form posts to.
	Path string
	// Text, First and Last are the fields' values. First and Last are dates
	// as a date input holds them, "2026-10-03".
	Text  string
	First string
	Last  string
	// Max is the latest day either date input accepts, "2027-09-30".
	Max string
	// Error is the line under the fields. It is empty when nothing was
	// refused.
	Error string
	// Button is the primary button's text, "Add note" or "Save changes".
	Button string
	// Delete is the URL the Delete button posts to. It is empty on a new note.
	Delete string
}

// noteDraft is the note form's fields as posted, with the spaces either side
// trimmed.
type noteDraft struct {
	Text  string
	First string
	Last  string
}

func readNoteDraft(values url.Values) noteDraft {
	return noteDraft{
		Text:  strings.TrimSpace(values.Get("text")),
		First: strings.TrimSpace(values.Get("first")),
		Last:  strings.TrimSpace(values.Get("last")),
	}
}

// days returns the first and last day as midnight in now's location. refusal
// is the error message to show under the fields, or empty. An empty last day
// is the first day.
func (d noteDraft) days(now time.Time) (first, last time.Time, refusal string) {
	if d.Text == "" {
		return first, last, noteMissing
	}
	loc := now.Location()
	first, err := time.ParseInLocation(time.DateOnly, d.First, loc)
	if err != nil {
		return first, last, firstDayUnusable
	}
	if latest := lastNoteDay(now); first.After(latest) {
		return first, last, firstDayTooLate(latest)
	}
	if d.Last == "" {
		return first, first, ""
	}
	last, err = time.ParseInLocation(time.DateOnly, d.Last, loc)
	if err != nil {
		return first, last, lastDayUnusable
	}
	if last.Before(first) {
		return first, last, lastDayTooEarly
	}
	return first, last, ""
}

// noteRequest is what every note handler reads from the request first.
type noteRequest struct {
	principal auth.Principal
	// now is the current time in the reader's timezone.
	now time.Time
	// q is the month and day in the URL.
	q calendarQuery
}

func (h *activity) noteRequest(r *http.Request) noteRequest {
	principal := PrincipalFrom(r)
	now := h.now().In(locationFor(principal.User))
	return noteRequest{principal: principal, now: now, q: parseCalendarQuery(r.URL.Query(), now)}
}

// newNote handles GET /activity/calendar/notes/new and renders the Add note
// sheet. The first day is the day in the URL, or today when there is none.
func (h *activity) newNote(w http.ResponseWriter, r *http.Request) {
	n := h.noteRequest(r)
	first := midnightOn(n.now, n.now.Location())
	if n.q.day != nil {
		first = *n.q.day
	}
	s := &noteSheet{
		Title:  "Add note",
		Path:   notesPath + "?" + noteQuery(n.q.month, nil).Encode(),
		First:  first.Format(time.DateOnly),
		Button: "Add note",
	}
	h.renderNoteSheet(w, r, n, s, 0)
}

// createNote handles POST /activity/calendar/notes and writes the note.
func (h *activity) createNote(w http.ResponseWriter, r *http.Request) {
	n := h.noteRequest(r)
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	d := readNoteDraft(r.PostForm)
	first, last, refusal := d.days(n.now)
	if refusal != "" {
		s := &noteSheet{
			Title:  "Add note",
			Path:   notesPath + "?" + noteQuery(n.q.month, nil).Encode(),
			Text:   d.Text,
			First:  d.First,
			Last:   d.Last,
			Error:  refusal,
			Button: "Add note",
		}
		h.refuseNote(w, r, n, s)
		return
	}
	if _, err := h.queries.CreateCalendarNote(r.Context(), store.CreateCalendarNoteParams{
		GardenID:  n.principal.Garden.ID,
		StartsOn:  first,
		EndsOn:    last,
		Text:      d.Text,
		CreatedBy: n.principal.User.ID,
	}); err != nil {
		h.templates.serverError(h.logger, w, r, "save the note", err)
		return
	}
	h.noteChanged(w, r, n, "Note added.")
}

// editNote handles GET /activity/calendar/notes/{note} and renders the Edit
// note sheet filled in from the note.
func (h *activity) editNote(w http.ResponseWriter, r *http.Request) {
	n := h.noteRequest(r)
	note, ok := h.readNote(w, r, n)
	if !ok {
		return
	}
	s := editNoteSheet(note, n.q.month)
	h.renderNoteSheet(w, r, n, s, 0)
}

// saveNote handles POST /activity/calendar/notes/{note} and writes the
// change.
func (h *activity) saveNote(w http.ResponseWriter, r *http.Request) {
	n := h.noteRequest(r)
	note, ok := h.readNote(w, r, n)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.templates.badRequest(w, r)
		return
	}
	d := readNoteDraft(r.PostForm)
	first, last, refusal := d.days(n.now)
	if refusal != "" {
		s := editNoteSheet(note, n.q.month)
		s.Text, s.First, s.Last, s.Error = d.Text, d.First, d.Last, refusal
		h.refuseNote(w, r, n, s)
		return
	}
	switch _, err := h.queries.UpdateCalendarNote(r.Context(), store.UpdateCalendarNoteParams{
		StartsOn: first,
		EndsOn:   last,
		Text:     d.Text,
		GardenID: n.principal.Garden.ID,
		NoteID:   note.ID,
	}); {
	case errors.Is(err, pgx.ErrNoRows):
		// The note was deleted between reading it and writing the change.
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "save the note", err)
		return
	}
	h.noteChanged(w, r, n, "Note saved.")
}

// deleteNote handles POST /activity/calendar/notes/{note}/delete. A note is
// one line and its days, so it is deleted straight away with no undo.
func (h *activity) deleteNote(w http.ResponseWriter, r *http.Request) {
	n := h.noteRequest(r)
	id, ok := noteID(r)
	if !ok {
		h.templates.notFound(w, r)
		return
	}
	switch _, err := h.queries.DeleteCalendarNote(r.Context(), n.principal.Garden.ID, id); {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "delete the note", err)
		return
	}
	h.noteChanged(w, r, n, "Note deleted.")
}

// noteID is the note's id from the path. ok is false for an id that does not
// parse.
func noteID(r *http.Request) (id uuid.UUID, ok bool) {
	id, err := uuid.Parse(r.PathValue("note"))
	return id, err == nil
}

// readNote reads the note the path names. When it cannot, it writes a 404 or
// a 500 and returns false.
func (h *activity) readNote(w http.ResponseWriter, r *http.Request, n noteRequest) (store.CalendarNote, bool) {
	id, ok := noteID(r)
	if !ok {
		h.templates.notFound(w, r)
		return store.CalendarNote{}, false
	}
	note, err := h.queries.GetCalendarNote(r.Context(), n.principal.Garden.ID, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return store.CalendarNote{}, false
	case err != nil:
		h.templates.serverError(h.logger, w, r, "read the note", err)
		return store.CalendarNote{}, false
	}
	return note, true
}

// editNoteSheet returns the Edit note sheet filled in from note. Last day is
// empty for a note on one day, the same as when the note was added.
func editNoteSheet(note store.CalendarNote, month time.Time) *noteSheet {
	s := &noteSheet{
		Title:  "Edit note",
		Path:   notePath(note.ID, month),
		Text:   note.Text,
		First:  note.StartsOn.Format(time.DateOnly),
		Button: "Save changes",
		Delete: notesPath + "/" + note.ID.String() + "/delete?" + noteQuery(month, nil).Encode(),
	}
	if !note.EndsOn.Equal(note.StartsOn) {
		s.Last = note.EndsOn.Format(time.DateOnly)
	}
	return s
}

// renderNoteSheet renders s. An htmx request gets the dialog alone. Any other
// request gets the calendar page with the sheet open over it.
func (h *activity) renderNoteSheet(w http.ResponseWriter, r *http.Request, n noteRequest, s *noteSheet, status int) {
	s.Max = lastNoteDay(n.now).Format(time.DateOnly)
	page := calendarPage{}
	if !isHTMX(r) {
		var err error
		page, err = h.calendarPage(r.Context(), n.principal, n.q, n.now)
		if err != nil {
			h.templates.serverError(h.logger, w, r, "load the calendar", err)
			return
		}
	}
	page.NoteSheet = s
	h.templates.render(w, r, view{page: "calendar", fragment: noteSheetTemplate, status: status, announce: s.Error}, page)
}

// refuseNote renders the sheet again with its error message and a 422. The
// form targets #calendar-grid, so the response headers retarget it at #sheet.
func (h *activity) refuseNote(w http.ResponseWriter, r *http.Request, n noteRequest, s *noteSheet) {
	w.Header().Set("HX-Retarget", "#"+sheetID)
	w.Header().Set("HX-Reswap", "outerHTML")
	h.renderNoteSheet(w, r, n, s, http.StatusUnprocessableEntity)
}

// noteChanged responds to an added, saved or deleted note. Without htmx it
// redirects to the month. With htmx it renders the month's grid and an empty
// #sheet that closes the dialog.
func (h *activity) noteChanged(w http.ResponseWriter, r *http.Request, n noteRequest, announce string) {
	if !isHTMX(r) {
		http.Redirect(w, r, calendarHref(n.q.month, nil), http.StatusSeeOther)
		return
	}
	page, err := h.calendarPage(r.Context(), n.principal, calendarQuery{month: n.q.month}, n.now)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the calendar", err)
		return
	}
	h.templates.render(w, r, view{page: "calendar", fragment: noteChangedTemplate, announce: announce}, page)
}
