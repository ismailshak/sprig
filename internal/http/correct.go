package http

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/schedule"
	"github.com/ismailshak/sprig/internal/store"
)

// eventRowPrefix starts the HTML id of every row on the activity log. A delete
// swaps the row it was made from, so the row needs an id the server assigns.
const eventRowPrefix = "event-"

func eventRowID(eventID uuid.UUID) string {
	return eventRowPrefix + eventID.String()
}

// eventPath is the URL of one event already recorded. With an empty suffix it
// is the correcting sheet on GET and the save on POST; the two other posts are
// "/delete" and "/restore". Every one of them carries the log's own query
// string, so the response can render the page the sheet was opened over,
// filter and paging cursor included.
func eventPath(plantID, eventID uuid.UUID, suffix string, q logQuery) string {
	path := logPath(plantID) + "/" + eventID.String() + suffix
	if values := q.values(); len(values) > 0 {
		path += "?" + values.Encode()
	}
	return path
}

// correctHref is the URL a row on the log points at, which is the sheet that
// corrects the event. It is empty for a reader who may not correct that event,
// and the row is then not pressable.
func correctHref(principal auth.Principal, q logQuery, e store.CareEvent) string {
	if !mayCorrect(principal, e) {
		return ""
	}
	return eventPath(e.PlantID, e.ID, "", q)
}

// mayCorrect reports whether the reader may change an event. It repeats
// UpdateCareEvent's own test, so a row that is not pressable and a save that
// returns 404 agree on who may correct what.
func mayCorrect(principal auth.Principal, e store.CareEvent) bool {
	return principal.Can(auth.CareEditAny) ||
		(e.PerformedBy == principal.User.ID && principal.Can(auth.CareEditOwn))
}

// mayDelete reports whether the sheet shows Delete. It repeats
// DeleteCareEvent's own test, so the button and the 404 agree. Unlike the Undo
// on Today's feed it has no time limit, because this sheet is where care
// recorded days ago is removed.
func mayDelete(principal auth.Principal, e store.CareEvent) bool {
	return principal.Can(auth.CareDeleteAny) ||
		(e.PerformedBy == principal.User.ID && principal.Can(auth.CareDeleteOwn))
}

// event is one recorded event and the page it was opened from. Every route
// under /plants/{plant}/log/{event} starts by reading one.
type event struct {
	row store.GetCareEventRow
	q   logQuery
	// now is in the reader's timezone.
	now time.Time
}

func (e event) care() store.CareEvent { return e.row.CareEvent }

// readEventPath reads the plant and the event out of the path, and the log
// query out of the query string. It returns false when any of them names
// nothing: an id that does not parse, or a filter naming a plant other than the
// one in the path, which is a URL no row on the log produces.
func readEventPath(r *http.Request) (plantID, eventID uuid.UUID, q logQuery, ok bool) {
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		return plantID, eventID, q, false
	}
	eventID, err = uuid.Parse(r.PathValue("event"))
	if err != nil {
		return plantID, eventID, q, false
	}
	q, ok = parseLogQuery(r)
	if !ok || (q.plant != nil && *q.plant != plantID) {
		return plantID, eventID, q, false
	}
	return plantID, eventID, q, true
}

// readEvent reads the event the request names. It writes the response and
// returns false for a URL that names nothing, and for an event the garden does
// not have.
func (h *activity) readEvent(w http.ResponseWriter, r *http.Request) (event, bool) {
	principal := PrincipalFrom(r)
	plantID, eventID, q, ok := readEventPath(r)
	if !ok {
		http.NotFound(w, r)
		return event{}, false
	}

	row, err := h.queries.GetCareEvent(r.Context(), store.GetCareEventParams{
		GardenID: principal.Garden.ID,
		PlantID:  plantID,
		ID:       eventID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return event{}, false
	case err != nil:
		serverError(h.logger, w, r, "read the care", err)
		return event{}, false
	}
	return event{row: row, q: q, now: h.now().In(locationFor(principal.User))}, true
}

// correct handles GET /plants/{plant}/log/{event} and opens the sheet over the
// row, filled in from the event. A page navigation gets the whole activity page
// with the sheet on it, the swap that opens the sheet gets the dialog, and the
// swap that switches What gets the form alone.
func (h *activity) correct(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	e, ok := h.readEvent(w, r)
	if !ok {
		return
	}
	if !mayCorrect(principal, e.care()) {
		http.NotFound(w, r)
		return
	}

	// A GET with no "when" is a row opening the sheet for the first time, so
	// the fields come from the event. With one it is a What chip fetching the
	// sheet again, and the fields come from the form it submitted.
	d := draftOf(e)
	if r.URL.Query().Get("when") != "" {
		d, ok = readDraft(r.URL.Query())
		if !ok {
			http.Error(w, "the sheet did not send that", http.StatusBadRequest)
			return
		}
	}

	s, err := h.sheetOver(r, principal, e, d)
	switch {
	case errors.Is(err, errNotOffered):
		http.Error(w, "the garden has no such care", http.StatusBadRequest)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return
	}
	page, err := h.page(r.Context(), principal, e.q)
	if err != nil {
		serverError(h.logger, w, r, "load the log", err)
		return
	}
	page.Sheet = s
	h.templates.render(w, r, view{page: "activity", fragment: sheetFragment(r)}, page)
}

// save handles POST /plants/{plant}/log/{event} and writes the correction. The
// swap replaces the whole log body, because a correction that moves the time
// moves the row to the day it now belongs under and changes the count on the
// day it left. A form post gets a redirect back to the log.
func (h *activity) save(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	e, ok := h.readEvent(w, r)
	if !ok {
		return
	}
	if !mayCorrect(principal, e.care()) {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	d, ok := readDraft(r.PostForm)
	if !ok {
		http.Error(w, "the sheet did not send that", http.StatusBadRequest)
		return
	}

	s, err := h.sheetOver(r, principal, e, d)
	switch {
	case errors.Is(err, errNotOffered):
		http.Error(w, "the garden has no such care", http.StatusBadRequest)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the plant", err)
		return
	}

	performedAt, err := s.accept(d, e.now)
	var refused whenRefused
	switch {
	case errors.As(err, &refused):
		page, err := h.page(r.Context(), principal, e.q)
		if err != nil {
			serverError(h.logger, w, r, "load the log", err)
			return
		}
		s.WhenError = string(refused)
		page.Sheet = s
		// The form targets the log body, so a refusal retargets the response at
		// the sheet, where the message is.
		w.Header().Set("HX-Retarget", "#sheet")
		w.Header().Set("HX-Reswap", "outerHTML")
		h.templates.render(w, r, view{page: "activity", fragment: "sheet", status: http.StatusUnprocessableEntity}, page)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params := store.UpdateCareEventParams{
		ID:          e.care().ID,
		GardenID:    principal.Garden.ID,
		PlantID:     e.care().PlantID,
		CareTypeID:  s.careTypeID,
		PerformedAt: performedAt,
		Done:        !d.Skipped,
		MayEditAny:  principal.Can(auth.CareEditAny),
		PerformedBy: principal.User.ID,
	}
	if d.Note != "" {
		params.Note = &d.Note
	}
	if d.Skipped {
		params.OverrideIntervalDays = &d.Again
	}
	switch _, err := h.queries.UpdateCareEvent(r.Context(), params); {
	case errors.Is(err, pgx.ErrNoRows):
		// The event was deleted between reading it and writing the correction.
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "save the correction", err)
		return
	}

	if !isHTMX(r) {
		http.Redirect(w, r, e.q.href(), http.StatusSeeOther)
		return
	}
	// The log is read again so the row is placed by the time it now holds.
	saved, err := h.page(r.Context(), principal, e.q)
	if err != nil {
		serverError(h.logger, w, r, "load the log", err)
		return
	}
	h.templates.render(w, r, view{page: "activity", fragment: "log-saved"}, saved)
}

// remove handles POST /plants/{plant}/log/{event}/delete. The event is deleted
// straight away and the row stays in place for the length of the undo window,
// showing "Deleted" with an Undo button. Deleting when the button is pressed
// rather than when the window closes means a reader who navigates away has
// still deleted what they asked to delete.
//
// A form post gets a redirect back to the log and no window, since the window
// is a timer the page runs.
func (h *activity) remove(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	e, ok := h.readEvent(w, r)
	if !ok {
		return
	}

	_, err := h.queries.DeleteCareEvent(r.Context(), store.DeleteCareEventParams{
		ID:           e.care().ID,
		GardenID:     principal.Garden.ID,
		PlantID:      e.care().PlantID,
		MayDeleteAny: principal.Can(auth.CareDeleteAny),
		PerformedBy:  principal.User.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Somebody else's event is a 404, the same as one that does not exist.
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "delete the care", err)
		return
	}

	if !isHTMX(r) {
		http.Redirect(w, r, e.q.href(), http.StatusSeeOther)
		return
	}
	h.templates.render(w, r, view{page: "activity", fragment: "event-deleted"}, deletedRow(principal, e))
}

// restore handles POST /plants/{plant}/log/{event}/restore, which is the Undo
// button on a deleted row. The event is gone from the table by then, so its
// values come back as hidden fields on that button's form. RestoreCareEvent
// refuses a performer other than the reader unless they may delete anyone's
// care, so nobody can put back an event they could not have deleted.
func (h *activity) restore(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, eventID, q, ok := readEventPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// The plant is read first because the restore inserts a row against it, and
	// a plant the garden does not have has to be a 404 rather than a row that
	// fails a foreign key.
	if _, err := h.queries.GetPlant(r.Context(), principal.Garden.ID, plantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(h.logger, w, r, "read the plant", err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form did not parse", http.StatusBadRequest)
		return
	}
	params, ok := restoreParams(r.PostForm, h.now())
	if !ok {
		http.Error(w, "the row did not send that", http.StatusBadRequest)
		return
	}
	params.ID = eventID
	params.GardenID = principal.Garden.ID
	params.PlantID = plantID
	params.MayDeleteAny = principal.Can(auth.CareDeleteAny)
	params.RestoredBy = principal.User.ID

	// The care type is checked here for the same reason as the plant: the
	// insert references it, and one the garden does not have is a 404 rather
	// than a row that fails a foreign key. An archived care type counts, since
	// the event being restored can be one of the last recorded under it.
	if _, err := h.queries.GetCareType(r.Context(), principal.Garden.ID, params.CareTypeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(h.logger, w, r, "read the care type", err)
		return
	}

	if _, err := h.queries.RestoreCareEvent(r.Context(), params); err != nil {
		// No row means the event is already back, or the reader may not restore
		// one performed by somebody else. Both are a 404, the same as an event
		// that does not exist.
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		serverError(h.logger, w, r, "restore the care", err)
		return
	}

	if !isHTMX(r) {
		// href builds /activity from a parsed uuid and a parsed cursor, never
		// from text the request supplied.
		http.Redirect(w, r, q.href(), http.StatusSeeOther)
		return
	}
	e, ok := h.readEvent(w, r)
	if !ok {
		return
	}
	h.templates.render(w, r, view{page: "activity", fragment: "event-row"}, eventRowOf(principal, e))
}

// restoreParams reads the deleted event out of the Undo form. It returns false
// for anything the row it came from could not have held: a field that does not
// parse, a time later than now, or a skip with no interval.
//
// The times are checked because every field here arrives with the request. The
// sheet refuses care given later than now, and without the same check a
// hand-made post to this route would be the one way to store an event dated in
// the future, which would take the plant off Today until that date.
func restoreParams(values url.Values, now time.Time) (store.RestoreCareEventParams, bool) {
	var p store.RestoreCareEventParams
	careTypeID, err := uuid.Parse(values.Get("care"))
	if err != nil {
		return p, false
	}
	performedBy, err := uuid.Parse(values.Get("by"))
	if err != nil {
		return p, false
	}
	performedAt, err := time.Parse(time.RFC3339Nano, values.Get("at"))
	if err != nil {
		return p, false
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, values.Get("recorded"))
	if err != nil {
		return p, false
	}
	done, err := strconv.ParseBool(values.Get("done"))
	if err != nil {
		return p, false
	}
	if performedAt.After(now) || recordedAt.After(now) {
		return p, false
	}
	p = store.RestoreCareEventParams{
		CareTypeID:  careTypeID,
		PerformedBy: performedBy,
		PerformedAt: performedAt,
		RecordedAt:  recordedAt,
		Done:        done,
	}
	if note := values.Get("note"); note != "" {
		p.Note = &note
	}
	if again := values.Get("again"); again != "" {
		n, err := strconv.ParseInt(again, 10, 32)
		if err != nil {
			return p, false
		}
		days := int32(n)
		p.OverrideIntervalDays = &days
	}
	// A skip is the interval it put the care off by. The log-care sheet always
	// records one, so a skip arriving without it is not a row this app wrote.
	if !p.Done && p.OverrideIntervalDays == nil {
		return p, false
	}
	return p, true
}

// sheetOver builds the sheet open over one row of the log. It returns
// errNotOffered when the draft names a care type the garden does not have.
func (h *activity) sheetOver(r *http.Request, principal auth.Principal, e event, d draft) (*sheet, error) {
	detail, err := loadPlant(r.Context(), h.queries, principal, e.care().PlantID, h.now())
	if err != nil {
		return nil, err
	}
	// The sheet offers every care type in the garden rather than the ones this
	// plant is scheduled for, because an event can be of any type and "I
	// recorded a watering and it was actually a feed" has to be a correction
	// the sheet can make.
	offers := withEventCare(detail.offers(), e.row.CareType)
	care, ok := offerFor(offers, d.Care)
	if !ok {
		return nil, errNotOffered
	}

	s := newSheet(detail.plant, offers, care, d, e.now)
	s.forEvent(principal, e, d)
	return s, nil
}

// withEventCare adds the care type an event was recorded under to the ones the
// sheet lists, when the garden's list has left it out. A care type archived
// since is no longer listed, and without this the sheet could not open on an
// event holding one, let alone save it again. The chip keeps its place in
// creation order, which is the order the rest of them are in.
func withEventCare(offers []offer, careType store.CareType) []offer {
	if _, ok := offerFor(offers, careType.Slug); ok {
		return offers
	}
	at := len(offers)
	for i, o := range offers {
		if o.CareType.CreatedAt.After(careType.CreatedAt) {
			at = i
			break
		}
	}
	return slices.Insert(offers, at, offer{CareType: careType})
}

// draftOf fills the sheet's fields from an event already recorded. The day chip
// is picked from how many days ago the care happened, because the chip owns the
// date and the picker under it edits only the time. "Just now" is not a value a
// past event produces, though it stays on offer, since "no, I did it just now"
// is a correction somebody can make.
func draftOf(e event) draft {
	at := e.care().PerformedAt.In(e.now.Location())
	d := draft{
		Care:    e.row.CareType.Slug,
		Skipped: !e.care().Done,
		Again:   2,
		Clock:   at.Format(clockLayout),
		At:      at.Format(atLayout),
		When:    whenOther,
	}
	switch days := schedule.DaysBetween(at, e.now); {
	case days <= 0:
		d.When = whenToday
	case days == 1:
		d.When = whenYesterday
	}
	if note := e.care().Note; note != nil {
		d.Note = *note
	}
	if again := e.care().OverrideIntervalDays; again != nil {
		d.Again = *again
	}
	return d
}

// recordedLine is the line above the sheet's buttons saying who wrote the event
// down and when. It is the only place the app shows both of the times an event
// holds: care given yesterday evening can have been entered this morning.
func recordedLine(principal auth.Principal, e event) string {
	who := e.row.PerformedByName
	if e.care().PerformedBy == principal.User.ID {
		who = "you"
	}
	performed := agoWord(e.care().PerformedAt, e.now) + " at " + clockWord(e.care().PerformedAt, e.now)
	if schedule.DaysBetween(e.care().RecordedAt, e.now) == schedule.DaysBetween(e.care().PerformedAt, e.now) {
		return "Recorded by " + who + ", " + performed + "."
	}
	return "Recorded by " + who + " " + agoWord(e.care().RecordedAt, e.now) + ", for " + performed + "."
}

// eventRowOf builds one event as the log renders it, so that a swap can replace
// a single row.
func eventRowOf(principal auth.Principal, e event) *eventRow {
	row := store.ListCareEventLogRow{
		CareEvent:       e.care(),
		Plant:           e.row.Plant,
		CareType:        e.row.CareType,
		PerformedByName: e.row.PerformedByName,
	}
	if e.q.plant != nil {
		return newPlantEventRow(principal, e.q, row, e.now)
	}
	return newEventRow(principal, e.q, row, e.now)
}

// deletedRow builds the row a delete leaves in place of the event. It keeps the
// lead the row already had, so nothing on the page moves, and carries the
// deleted event as hidden fields on the Undo form.
func deletedRow(principal auth.Principal, e event) *eventRow {
	row := eventRowOf(principal, e)
	row.Href = ""
	row.Deleted = true
	row.Restore = eventPath(e.care().PlantID, e.care().ID, "/restore", e.q)
	row.Settled = e.q.href()
	row.Grace = int(graceWindow.Milliseconds())
	row.Fields = &restoreFields{
		Care:        e.row.CareType.ID.String(),
		PerformedBy: e.care().PerformedBy.String(),
		PerformedAt: e.care().PerformedAt.Format(time.RFC3339Nano),
		RecordedAt:  e.care().RecordedAt.Format(time.RFC3339Nano),
		Done:        strconv.FormatBool(e.care().Done),
	}
	if note := e.care().Note; note != nil {
		row.Fields.Note = *note
	}
	if again := e.care().OverrideIntervalDays; again != nil {
		row.Fields.Again = strconv.Itoa(int(*again))
	}
	return row
}

// restoreFields is the deleted event as hidden inputs on the Undo form. The row
// is gone from the table while the window runs, so what puts it back has to
// travel with the request.
type restoreFields struct {
	// Care is the care type's id.
	Care        string
	PerformedBy string
	// PerformedAt and RecordedAt are RFC 3339 timestamps.
	PerformedAt string
	RecordedAt  string
	// Done is "true" or "false".
	Done string
	Note string
	// Again is the skip's interval in days, and empty for care that was done.
	Again string
}
