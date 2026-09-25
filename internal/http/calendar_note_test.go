package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

var (
	calendarAwayNoteID  = uuid.MustParse("00000000-0000-7000-8000-000000000601")
	calendarPartyNoteID = uuid.MustParse("00000000-0000-7000-8000-000000000602")
)

// awayAndParty gives the garden two notes in September: Ellie away from the
// 5th to the 9th, and a party on the 19th.
func (f *calendarFixture) awayAndParty(t *testing.T) {
	t.Helper()
	f.exec(t, "INSERT INTO calendar_note (id, garden_id, starts_on, ends_on, text, created_by) VALUES ($1, $2, '2026-09-05', '2026-09-09', 'Ellie away', $3)", calendarAwayNoteID, rosewoodID, readerID)
	f.exec(t, "INSERT INTO calendar_note (id, garden_id, starts_on, ends_on, text, created_by) VALUES ($1, $2, '2026-09-19', '2026-09-19', 'Party', $3)", calendarPartyNoteID, rosewoodID, readerID)
}

// mayManageNotes gives the reader the capability that adds, edits and deletes
// notes.
func (f *calendarFixture) mayManageNotes() {
	f.principal.Capabilities[auth.CalendarNoteManage] = true
}

// note sends a request to one of the note handlers. form is posted as the
// body when it is not nil. noteID is put in the path when it is not the zero
// id.
func (f *calendarFixture) note(t *testing.T, handler http.HandlerFunc, method, target string, noteID uuid.UUID, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if noteID != (uuid.UUID{}) {
		req.SetPathValue("note", noteID.String())
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// noteLink is a row under Notes in a day's sheet.
type noteLink struct {
	// href is the URL the row links to. It is empty for a row that is not a
	// link.
	href string
	text string
}

// sheetNotes returns the rows under Notes in the open day's sheet.
func sheetNotes(t *testing.T, page string) []noteLink {
	t.Helper()

	sheet := readHTML(page).byID(sheetID)
	if sheet == nil || sheet.tag != "dialog" {
		t.Fatalf("the page has no open sheet:\n%s", text(page))
	}
	list := sheet.first(isTag("ul"), attrIs("aria-labelledby", "day-sheet-notes"))
	if list == nil {
		return nil
	}
	var rows []noteLink
	for _, li := range list.all(isTag("li")) {
		row := noteLink{text: li.text()}
		if a := li.first(isTag("a")); a != nil {
			row.href = a.attr("href")
		}
		rows = append(rows, row)
	}
	return rows
}

// sheetNoteTexts returns the text of each row under Notes in the open day's
// sheet.
func sheetNoteTexts(t *testing.T, page string) []string {
	t.Helper()
	var texts []string
	for _, row := range sheetNotes(t, page) {
		texts = append(texts, row.text)
	}
	return texts
}

// noteRow reads the row with text in a day's sheet's Notes group, or fails.
func noteRow(t *testing.T, page, text string) noteLink {
	t.Helper()
	for _, row := range sheetNotes(t, page) {
		if strings.HasPrefix(row.text, text) {
			return row
		}
	}
	t.Fatalf("the sheet has no note row starting %q, only %q", text, sheetNotes(t, page))
	return noteLink{}
}

// noteSheetOf returns the open note sheet in a response, or fails.
func noteSheetOf(t *testing.T, body string) *element {
	t.Helper()
	sheet := readHTML(body).byID(sheetID)
	if sheet == nil || sheet.tag != "dialog" || sheet.first(isTag("input"), attrIs("name", "text")) == nil {
		t.Fatalf("the response has no open note sheet:\n%s", text(body))
	}
	return sheet
}

// noteFields reads the values of the note sheet's three fields.
func noteFields(sheet *element) (text, first, last string) {
	value := func(name string) string { return sheet.first(isTag("input"), attrIs("name", name)).attr("value") }
	return value("text"), value("first"), value("last")
}

// noteError is the line under the fields, "" when there is none. It is the
// only paragraph in the note sheet.
func noteError(sheet *element) string {
	if p := sheet.first(isTag("p")); p != nil {
		return p.text()
	}
	return ""
}

func (f *calendarFixture) storedNote(t *testing.T, id uuid.UUID) (text string, first, last time.Time) {
	t.Helper()
	if err := f.tx.QueryRow(t.Context(), "SELECT text, starts_on, ends_on FROM calendar_note WHERE garden_id = $1 AND id = $2", rosewoodID, id).Scan(&text, &first, &last); err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	return text, first, last
}

func (f *calendarFixture) noteCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM calendar_note WHERE garden_id = $1", rosewoodID).Scan(&n); err != nil {
		t.Fatalf("counting the notes: %v", err)
	}
	return n
}

func TestCalendarNote_ADayLinksNameHasTheTextOfEachNoteCoveringTheDay(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)

	page := f.get(t, calendarPath, nil)

	for d, want := range map[int]string{
		// The note starts on the 5th and ends on the 9th, both included.
		4:  "Friday 4 September, 1 due",
		5:  "Saturday 5 September, Ellie away",
		7:  "Monday 7 September, Ellie away, 2 due",
		9:  "Wednesday 9 September, Ellie away",
		10: "Thursday 10 September, 1 due",
		19: "Saturday 19 September, Party, 2 due",
	} {
		if got := dayLinkName(page, at(time.September, d, 0, 0)); got != want {
			t.Errorf("the link on %d September is named %q, want %q", d, got, want)
		}
	}
}

func TestCalendarNote_ADayLinksNameHasTheNoteBeforeTheSitters(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.samAndJo(t)
	f.principal.Capabilities[auth.SittingView] = true

	page := f.get(t, calendarPath, nil)

	if got, want := dayLinkName(page, at(time.September, 7, 0, 0)), "Monday 7 September, Ellie away, 2 due, Sam and Jo sitting"; got != want {
		t.Errorf("the link on 7 September is named %q, want %q", got, want)
	}
}

func TestCalendarNote_ADaysSheetListsEachNoteWithItsDays(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-07", nil)

	if got, want := sheetNotes(t, page), []noteLink{{text: "Ellie away 5–9 Sep"}}; !slices.Equal(got, want) {
		t.Errorf("the sheet's Notes rows are %+v, want %+v", got, want)
	}
	if readHTML(page).byID(sheetID).first(isTag("a"), textIs("Add note")) != nil {
		t.Errorf("a reader who may not add a note is offered Add note:\n%s", text(page))
	}

	page = f.get(t, calendarPath+"?month=2026-09&day=2026-09-19", nil)

	// A note on one day has no second line.
	if got, want := sheetNotes(t, page), []noteLink{{text: "Party"}}; !slices.Equal(got, want) {
		t.Errorf("the sheet's Notes rows are %+v, want %+v", got, want)
	}
}

func TestCalendarNote_AReaderWhoMayManageNotesGetsLinksToAddAndEditThem(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.mayManageNotes()

	page := f.get(t, calendarPath+"?month=2026-09&day=2026-09-07", nil)

	// 2 September has nothing logged or due and no note.
	if got, want := dayLinkName(page, at(time.September, 2, 0, 0)), "Wednesday 2 September"; got != want {
		t.Errorf("the link on 2 September is named %q, want %q", got, want)
	}
	add := readHTML(page).byID(sheetID).first(isTag("a"), textIs("Add note"))
	if add == nil {
		t.Fatalf("the sheet has no Add note link:\n%s", text(page))
	}
	if got, want := add.attr("href"), "/activity/calendar/notes/new?day=2026-09-07&month=2026-09"; got != want {
		t.Errorf("Add note links to %q, want %q", got, want)
	}
	if got, want := noteRow(t, page, "Ellie away").href, "/activity/calendar/notes/"+calendarAwayNoteID.String()+"?month=2026-09"; got != want {
		t.Errorf("the note's row links to %q, want %q", got, want)
	}
}

func TestCalendarNote_AnotherGardensNoteIsNotShown(t *testing.T) {
	f := rosewoodCalendar(t)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Robin', 'robin', 'Europe/London')", otherUserID)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", otherGardenID)
	f.exec(t, "INSERT INTO calendar_note (garden_id, starts_on, ends_on, text, created_by) VALUES ($1, '2026-09-01', '2026-09-30', 'Robin away', $2)", otherGardenID, otherUserID)

	page := f.get(t, calendarPath, nil)

	if strings.Contains(text(page), "Robin away") {
		t.Errorf("the calendar shows another garden's note:\n%s", text(page))
	}
}

func TestCalendarNote_TheNewNoteSheetStartsOnTheDayItWasOpenedFrom(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.newNote, http.MethodGet, "/activity/calendar/notes/new?month=2026-09&day=2026-09-07", uuid.UUID{}, nil, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	sheet := noteSheetOf(t, rec.Body.String())
	if text, first, last := noteFields(sheet); text != "" || first != "2026-09-07" || last != "" {
		t.Errorf("the fields hold %q, %q, %q, want an empty note on 2026-09-07 with no last day", text, first, last)
	}
	form := sheet.first(isTag("form"), hasAttr("action"))
	if got, want := form.attr("action"), "/activity/calendar/notes?month=2026-09"; got != want {
		t.Errorf("the form posts to %q, want %q", got, want)
	}
	if got, want := form.attr("hx-target"), "#"+calendarGridID; got != want {
		t.Errorf("the form swaps %q, want %q", got, want)
	}
	// The calendar goes to September 2027, twelve months after the current one.
	for _, name := range []string{"first", "last"} {
		if got, want := sheet.first(isTag("input"), attrIs("name", name)).attr("max"), "2027-09-30"; got != want {
			t.Errorf("the %s day accepts days up to %q, want %q", name, got, want)
		}
	}
	if sheet.first(isTag("button"), textIs("Delete")) != nil {
		t.Errorf("a new note's sheet has Delete")
	}
}

func TestCalendarNote_AnAddedNoteIsMarkedOnTheSwappedGrid(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.createNote, http.MethodPost, "/activity/calendar/notes?month=2026-09&day=2026-09-07", uuid.UUID{}, url.Values{
		"text": {"  Heatwave "}, "first": {"2026-09-07"}, "last": {"2026-09-12"},
	}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if n := f.noteCount(t); n != 1 {
		t.Fatalf("the garden has %d notes, want 1", n)
	}
	body := readHTML(rec.Body.String())
	if body.byID(calendarGridID) == nil {
		t.Errorf("the response does not swap #%s:\n%s", calendarGridID, rec.Body.String())
	}
	if sheet := body.byID(sheetID); sheet == nil || sheet.tag == "dialog" || sheet.attr("hx-swap-oob") != "true" {
		t.Errorf("the response does not close the sheet out of band:\n%s", rec.Body.String())
	}
	if got, want := dayLinkName(rec.Body.String(), at(time.September, 12, 0, 0)), "Saturday 12 September, Heatwave"; got != want {
		t.Errorf("the link on 12 September is named %q, want %q", got, want)
	}
	if got, want := dayLinkName(rec.Body.String(), at(time.September, 13, 0, 0)), "Sunday 13 September, 2 due"; got != want {
		t.Errorf("the link on 13 September is named %q, want %q", got, want)
	}
}

func TestCalendarNote_APostedNoteWithNoLastDayIsOnItsFirstDayAlone(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.createNote, http.MethodPost, "/activity/calendar/notes?month=2026-09&day=2026-09-07", uuid.UUID{}, url.Values{
		"text": {"Party"}, "first": {"2026-09-07"}, "last": {""},
	}, false)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/activity/calendar?month=2026-09" {
		t.Fatalf("status = %d to %q, want %d to the month:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, rec.Body.String())
	}
	page := f.get(t, calendarPath, nil)
	if got, want := dayLinkName(page, at(time.September, 7, 0, 0)), "Monday 7 September, Party, 2 due"; got != want {
		t.Errorf("the link on 7 September is named %q, want %q", got, want)
	}
	if got, want := dayLinkName(page, at(time.September, 8, 0, 0)), "Tuesday 8 September, 1 due"; got != want {
		t.Errorf("the link on 8 September is named %q, want %q", got, want)
	}
}

func TestCalendarNote_ARefusedNoteIsShownAgainWithTheReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{"an empty note", url.Values{"text": {"  "}, "first": {"2026-09-07"}}, noteMissing},
		{"no first day", url.Values{"text": {"Party"}, "first": {""}}, firstDayUnusable},
		{"a last day that is not a date", url.Values{"text": {"Party"}, "first": {"2026-09-07"}, "last": {"soon"}}, lastDayUnusable},
		{"a last day before the first", url.Values{"text": {"Party"}, "first": {"2026-09-07"}, "last": {"2026-09-06"}}, lastDayTooEarly},
		// The calendar goes to September 2027, twelve months after the current
		// one.
		{"a first day after the last day the calendar shows", url.Values{"text": {"Party"}, "first": {"2027-10-01"}}, "Choose a first day on or before 30 September 2027."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := rosewoodCalendar(t)
			f.mayManageNotes()

			rec := f.note(t, f.handler.createNote, http.MethodPost, "/activity/calendar/notes?month=2026-09&day=2026-09-07", uuid.UUID{}, tc.form, true)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
			}
			if got := rec.Header().Get("HX-Retarget"); got != "#"+sheetID {
				t.Errorf("HX-Retarget is %q, want the sheet", got)
			}
			sheet := noteSheetOf(t, rec.Body.String())
			if got := noteError(sheet); got != tc.want {
				t.Errorf("the sheet says %q, want %q", got, tc.want)
			}
			if text, first, last := noteFields(sheet); text != strings.TrimSpace(tc.form.Get("text")) || first != tc.form.Get("first") || last != tc.form.Get("last") {
				t.Errorf("the fields hold %q, %q, %q, want what was posted", text, first, last)
			}
			if n := f.noteCount(t); n != 0 {
				t.Errorf("the garden has %d notes, want none", n)
			}
		})
	}
}

func TestCalendarNote_TheEditSheetIsFilledInFromTheNote(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.editNote, http.MethodGet, notePath(calendarAwayNoteID, startOfMonth(thursday.In(london()))), calendarAwayNoteID, nil, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	sheet := noteSheetOf(t, rec.Body.String())
	if text, first, last := noteFields(sheet); text != "Ellie away" || first != "2026-09-05" || last != "2026-09-09" {
		t.Errorf("the fields hold %q, %q, %q, want the note's", text, first, last)
	}
	del := sheet.first(isTag("button"), textIs("Delete"))
	if del == nil {
		t.Fatalf("the sheet has no Delete button:\n%s", text(rec.Body.String()))
	}
	if got, want := del.attr("formaction"), "/activity/calendar/notes/"+calendarAwayNoteID.String()+"/delete?month=2026-09"; got != want {
		t.Errorf("Delete posts to %q, want %q", got, want)
	}

	// A note on one day leaves the last day empty, as it was when it was added.
	rec = f.note(t, f.handler.editNote, http.MethodGet, notePath(calendarPartyNoteID, startOfMonth(thursday.In(london()))), calendarPartyNoteID, nil, true)
	if _, _, last := noteFields(noteSheetOf(t, rec.Body.String())); last != "" {
		t.Errorf("the last day holds %q, want nothing", last)
	}
}

func TestCalendarNote_ASavedChangeIsWrittenToTheNote(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.saveNote, http.MethodPost, notePath(calendarAwayNoteID, startOfMonth(thursday.In(london()))), calendarAwayNoteID, url.Values{
		"text": {"Ellie in Lisbon"}, "first": {"2026-09-06"}, "last": {"2026-09-08"},
	}, false)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	text, first, last := f.storedNote(t, calendarAwayNoteID)
	if text != "Ellie in Lisbon" || first.Format(time.DateOnly) != "2026-09-06" || last.Format(time.DateOnly) != "2026-09-08" {
		t.Errorf("the row holds %q from %s to %s, want Ellie in Lisbon from 2026-09-06 to 2026-09-08", text, first.Format(time.DateOnly), last.Format(time.DateOnly))
	}
	page := f.get(t, calendarPath, nil)
	if got, want := dayLinkName(page, at(time.September, 5, 0, 0)), "Saturday 5 September"; got != want {
		t.Errorf("the link on 5 September is named %q, want %q", got, want)
	}
}

func TestCalendarNote_ARefusedChangeKeepsTheNoteAsItWas(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.saveNote, http.MethodPost, notePath(calendarAwayNoteID, startOfMonth(thursday.In(london()))), calendarAwayNoteID, url.Values{
		"text": {""}, "first": {"2026-09-06"}, "last": {"2026-09-08"},
	}, true)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if got := noteError(noteSheetOf(t, rec.Body.String())); got != noteMissing {
		t.Errorf("the sheet says %q, want %q", got, noteMissing)
	}
	if text, first, _ := f.storedNote(t, calendarAwayNoteID); text != "Ellie away" || first.Format(time.DateOnly) != "2026-09-05" {
		t.Errorf("the row holds %q from %s, want Ellie away from 2026-09-05", text, first.Format(time.DateOnly))
	}
}

func TestCalendarNote_ADeletedNoteIsGoneFromTheGrid(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.deleteNote, http.MethodPost, "/activity/calendar/notes/"+calendarAwayNoteID.String()+"/delete?month=2026-09", calendarAwayNoteID, url.Values{}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if n := f.noteCount(t); n != 1 {
		t.Errorf("the garden has %d notes, want the party alone", n)
	}
	if got, want := dayLinkName(rec.Body.String(), at(time.September, 7, 0, 0)), "Monday 7 September, 2 due"; got != want {
		t.Errorf("the link on 7 September is named %q, want %q", got, want)
	}
}

func TestCalendarNote_AnotherGardensNoteCannotBeOpenedSavedOrDeleted(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Robin', 'robin', 'Europe/London')", otherUserID)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", otherGardenID)
	f.exec(t, "INSERT INTO calendar_note (id, garden_id, starts_on, ends_on, text, created_by) VALUES ($1, $2, '2026-09-05', '2026-09-09', 'Robin away', $3)", calendarAwayNoteID, otherGardenID, otherUserID)
	month := startOfMonth(thursday.In(london()))
	form := url.Values{"text": {"Changed"}, "first": {"2026-09-05"}}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"the edit sheet": f.note(t, f.handler.editNote, http.MethodGet, notePath(calendarAwayNoteID, month), calendarAwayNoteID, nil, true),
		"a save":         f.note(t, f.handler.saveNote, http.MethodPost, notePath(calendarAwayNoteID, month), calendarAwayNoteID, form, true),
		"a delete":       f.note(t, f.handler.deleteNote, http.MethodPost, "/activity/calendar/notes/"+calendarAwayNoteID.String()+"/delete?month=2026-09", calendarAwayNoteID, url.Values{}, true),
	} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s of another garden's note got %d, want %d", name, rec.Code, http.StatusNotFound)
		}
	}
	var text string
	if err := f.tx.QueryRow(t.Context(), "SELECT text FROM calendar_note WHERE id = $1", calendarAwayNoteID).Scan(&text); err != nil || text != "Robin away" {
		t.Errorf("the other garden's note reads %q (%v), want Robin away", text, err)
	}
}

func TestCalendarNote_ANoteOnTheLastDayTheCalendarShowsIsAdded(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()

	rec := f.note(t, f.handler.createNote, http.MethodPost, "/activity/calendar/notes?month=2027-09", uuid.UUID{}, url.Values{
		"text": {"Party"}, "first": {"2027-09-30"},
	}, false)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	page := f.get(t, calendarPath+"?month=2027-09&day=2027-09-30", nil)
	if got, want := sheetNoteTexts(t, page), []string{"Party"}; !slices.Equal(got, want) {
		t.Errorf("the sheet on 30 September 2027 lists %q, want %q", got, want)
	}
}

func TestCalendarNote_ANoteCrossingAMonthsEdgeIsOnItsDaysInBothMonths(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()
	f.exec(t, "INSERT INTO calendar_note (garden_id, starts_on, ends_on, text, created_by) VALUES ($1, '2026-08-28', '2026-09-03', 'Ellie away', $2), ($1, '2026-09-29', '2026-10-02', 'Heatwave', $2)", rosewoodID, readerID)

	for _, tc := range []struct {
		day  string
		want []string
	}{
		{"2026-09-01", []string{"Ellie away 28 Aug–3 Sep"}},
		{"2026-09-03", []string{"Ellie away 28 Aug–3 Sep"}},
		{"2026-09-04", nil},
		{"2026-09-30", []string{"Heatwave 29 Sep–2 Oct"}},
		{"2026-10-02", []string{"Heatwave 29 Sep–2 Oct"}},
		{"2026-10-03", nil},
	} {
		page := f.get(t, calendarPath+"?month="+tc.day[:7]+"&day="+tc.day, nil)
		if got := sheetNoteTexts(t, page); !slices.Equal(got, tc.want) {
			t.Errorf("the sheet on %s lists %q, want %q", tc.day, got, tc.want)
		}
	}
}

func TestCalendarNote_ADaysSheetGivesANotesYearWhenItIsNotTheCurrentYear(t *testing.T) {
	f := rosewoodCalendar(t)
	f.exec(t, `INSERT INTO calendar_note (garden_id, starts_on, ends_on, text, created_by) VALUES
		($1, '2025-12-30', '2026-01-02', 'Last winter', $2),
		($1, '2026-12-30', '2027-01-02', 'New year', $2),
		($1, '2027-01-04', '2027-01-08', 'Ski trip', $2)`, rosewoodID, readerID)

	for day, want := range map[string]string{
		"2026-01-01": "Last winter 30 Dec 2025–2 Jan",
		"2026-12-31": "New year 30 Dec–2 Jan 2027",
		"2027-01-05": "Ski trip 4–8 Jan 2027",
	} {
		page := f.get(t, calendarPath+"?month="+day[:7]+"&day="+day, nil)
		if got := sheetNoteTexts(t, page); !slices.Equal(got, []string{want}) {
			t.Errorf("the sheet on %s lists %q, want %q", day, got, want)
		}
	}
}

func TestCalendarNote_AReaderWestOfUTCSeesTheNoteOnItsOwnDates(t *testing.T) {
	f := rosewoodCalendar(t)
	f.awayAndParty(t)
	f.mayManageNotes()
	f.principal.User.Timezone = "America/New_York"

	for day, want := range map[string][]string{
		"2026-09-04": nil,
		"2026-09-05": {"Ellie away 5–9 Sep"},
		"2026-09-09": {"Ellie away 5–9 Sep"},
		"2026-09-10": nil,
	} {
		page := f.get(t, calendarPath+"?month=2026-09&day="+day, nil)
		if got := sheetNoteTexts(t, page); !slices.Equal(got, want) {
			t.Errorf("the sheet on %s lists %q, want %q", day, got, want)
		}
	}
}

func TestCalendarNote_ANewNoteStartsOnTodayInTheReadersTimezone(t *testing.T) {
	f := rosewoodCalendar(t)
	f.mayManageNotes()
	f.principal.User.Timezone = "America/New_York"
	// 02:00 UTC on 3 September is 22:00 on 2 September in New York.
	f.handler.now = func() time.Time { return time.Date(2026, time.September, 3, 2, 0, 0, 0, time.UTC) }

	rec := f.note(t, f.handler.newNote, http.MethodGet, "/activity/calendar/notes/new", uuid.UUID{}, nil, true)

	if _, first, _ := noteFields(noteSheetOf(t, rec.Body.String())); first != "2026-09-02" {
		t.Errorf("the first day holds %q, want 2026-09-02", first)
	}
}
