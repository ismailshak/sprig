package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

// rosewoodCorrections is the log read by somebody who may change and delete
// anybody's care, which is what the owner role holds. The fixture's own
// principal holds only the capabilities Today asks for.
func rosewoodCorrections(t *testing.T) *logFixture {
	t.Helper()
	return grant(rosewoodLog(t), auth.CareEditOwn, auth.CareEditAny, auth.CareDeleteOwn, auth.CareDeleteAny)
}

// rosewoodSitter is the same log read by somebody who may change and delete
// only care they gave themselves.
func rosewoodSitter(t *testing.T) *logFixture {
	t.Helper()
	return grant(rosewoodLog(t), auth.CareEditOwn, auth.CareDeleteOwn)
}

func grant(f *logFixture, capabilities ...auth.Capability) *logFixture {
	for _, c := range capabilities {
		f.principal.Capabilities[c] = true
	}
	return f
}

// eventID reads the id of the one event this plant has, so a test can open the
// sheet over a row without naming an id the fixture never wrote.
func (f *logFixture) eventID(t *testing.T, plantID uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT id FROM care_event WHERE plant_id = $1", plantID).Scan(&id); err != nil {
		t.Fatalf("reading the event of %s: %v", plantID, err)
	}
	return id
}

func (f *logFixture) eventRequest(t *testing.T, method, target string, plantID, eventID uuid.UUID, form url.Values, htmx bool) *http.Request {
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
	req.SetPathValue("plant", plantID.String())
	req.SetPathValue("event", eventID.String())
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	return req
}

// openSheet requests the correcting sheet. Any extra query values are the ones
// a chip under Care resubmits the form with.
func (f *logFixture) openSheet(t *testing.T, plantID, eventID uuid.UUID, q logQuery, extra url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	target := eventPath(plantID, eventID, "", q)
	if len(extra) > 0 {
		if strings.Contains(target, "?") {
			target += "&"
		} else {
			target += "?"
		}
		target += extra.Encode()
	}
	rec := httptest.NewRecorder()
	f.handler.correct(rec, f.eventRequest(t, http.MethodGet, target, plantID, eventID, nil, htmx))
	return rec
}

func (f *logFixture) save(t *testing.T, plantID, eventID uuid.UUID, q logQuery, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := f.eventRequest(t, http.MethodPost, eventPath(plantID, eventID, "", q), plantID, eventID, form, htmx)
	f.handler.save(rec, req)
	return rec
}

func (f *logFixture) remove(t *testing.T, plantID, eventID uuid.UUID, q logQuery, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := f.eventRequest(t, http.MethodPost, eventPath(plantID, eventID, "/delete", q), plantID, eventID, url.Values{}, htmx)
	f.handler.remove(rec, req)
	return rec
}

func (f *logFixture) restore(t *testing.T, plantID, eventID uuid.UUID, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	req := f.eventRequest(t, http.MethodPost, eventPath(plantID, eventID, "/restore", logQuery{}), plantID, eventID, form, htmx)
	f.handler.restore(rec, req)
	return rec
}

var (
	hiddenInput = regexp.MustCompile(`<input[^>]*\stype="hidden"[^>]*>`)
	attribute   = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
	loggedLead  = regexp.MustCompile(`<p class="sheet__logged">([^<]*)</p>`)
)

// hiddenFields reads the hidden inputs of a form, which is what the browser
// sends back when its button is pressed.
func hiddenFields(markup string) url.Values {
	values := url.Values{}
	for _, input := range hiddenInput.FindAllString(markup, -1) {
		attrs := map[string]string{}
		for _, m := range attribute.FindAllStringSubmatch(input, -1) {
			attrs[m[1]] = m[2]
		}
		if name := attrs["name"]; name != "" {
			values.Set(name, attrs["value"])
		}
	}
	return values
}

func TestCorrect_TheSheetOverARowIsFilledInFromTheEvent(t *testing.T) {
	f := rosewoodCorrections(t)
	// Nigel was fed at six yesterday evening, with a note.
	rec := f.openSheet(t, nigelID, f.eventID(t, nigelID), logQuery{}, nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	dialog := dialogElement.FindString(rec.Body.String())
	if dialog == "" {
		t.Fatalf("the page holds no dialog:\n%s", rec.Body.String())
	}

	by := fields(dialog)
	if m := pressedButton.FindStringSubmatch(by["Care"]); m == nil || m[1] != "Feed" {
		t.Errorf("Care does not have Feed pressed:\n%s", by["Care"])
	}
	if got := checked(by["Outcome"]); got != "Done" {
		t.Errorf("Outcome is %q, want Done", got)
	}
	if got := checked(by["When"]); got != "Yesterday" {
		t.Errorf("When is %q, want Yesterday", got)
	}
	if !strings.Contains(dialog, `name="time" value="18:00"`) {
		t.Errorf("the time field does not hold six in the evening:\n%s", by["When"])
	}
	if !strings.Contains(dialog, `value="New pot"`) {
		t.Errorf("the note field does not hold the note that was recorded:\n%s", dialog)
	}
	if !strings.Contains(dialog, ">Save changes</button>") {
		t.Error("the primary button does not read Save changes")
	}
	if !strings.Contains(dialog, ">Delete</button>") {
		t.Error("the sheet offers no Delete")
	}
}

func TestCorrect_TheSheetOffersEveryCareTypeInTheGarden(t *testing.T) {
	f := rosewoodCorrections(t)
	// Big Fella is scheduled for watering alone, and the garden has two care
	// types, because an event can be of any type.
	dialog := dialogElement.FindString(f.openSheet(t, bigFellaID, f.eventID(t, bigFellaID), logQuery{}, nil, false).Body.String())

	if got := chips(fields(dialog)["Care"]); strings.Join(got, "|") != "Water|Feed" {
		t.Errorf("Care offers %v, want every care type in the garden", got)
	}
}

func TestCorrect_TheSheetsFormSendsBackThePlantTheLogIsFilteredTo(t *testing.T) {
	f := rosewoodCorrections(t)
	// A chip under Care submits the form as a GET, and a GET form replaces the
	// query string of the URL it submits to, so a filter held only in the URL
	// would be dropped the moment somebody changed the care type.
	q := logQuery{plant: &nigelID}
	dialog := dialogElement.FindString(f.openSheet(t, nigelID, f.eventID(t, nigelID), q, nil, false).Body.String())

	if got := hiddenFields(dialog).Get(plantParam); got != nigelID.String() {
		t.Errorf("the form sends plant=%q, want the plant the log is filtered to", got)
	}
}

func TestCorrect_ACareChipKeepsTheFieldsAlreadyFilledIn(t *testing.T) {
	f := rosewoodCorrections(t)
	// The chip resubmits the form, so everything on it comes back in the query.
	extra := url.Values{"care": {"water"}, "outcome": {"skipped"}, "when": {"yesterday"}, "time": {"18:00"}, "again": {"3"}, "note": {"New pot"}}
	dialog := dialogElement.FindString(f.openSheet(t, nigelID, f.eventID(t, nigelID), logQuery{}, extra, false).Body.String())

	by := fields(dialog)
	if m := pressedButton.FindStringSubmatch(by["Care"]); m == nil || m[1] != "Water" {
		t.Errorf("Care does not have Water pressed:\n%s", by["Care"])
	}
	if got := checked(by["Outcome"]); got != "Skipped" {
		t.Errorf("Outcome is %q, want the Skipped the chip resubmitted", got)
	}
	if got := checked(by["Remind me in"]); got != "3 days" {
		t.Errorf("Remind me in is %q, want the three days the chip resubmitted", got)
	}
	if !strings.Contains(dialog, `value="New pot"`) {
		t.Errorf("the note the chip resubmitted is not in the sheet:\n%s", dialog)
	}
}

func TestCorrect_ASkipShowsTheIntervalItWasLoggedWith(t *testing.T) {
	f := rosewoodCorrections(t)
	// Doris was skipped for two days, and is watered every 21.
	dialog := dialogElement.FindString(f.openSheet(t, dorisID, f.eventID(t, dorisID), logQuery{}, nil, false).Body.String())

	by := fields(dialog)
	if got := checked(by["Outcome"]); got != "Skipped" {
		t.Errorf("Outcome is %q, want Skipped", got)
	}
	if got := checked(by["Remind me in"]); got != "2 days" {
		t.Errorf("Remind me in is %q, want the two days the skip was recorded with", got)
	}
}

func TestCorrect_AnIntervalTheScheduleNoLongerOffersIsStillOnTheSheet(t *testing.T) {
	f := rosewoodCorrections(t)
	// A skip logged when the schedule said five days. Nothing offers five now
	// that Doris is watered every 21.
	f.exec(t, "UPDATE care_event SET override_interval_days = 5 WHERE plant_id = $1", dorisID)
	dialog := dialogElement.FindString(f.openSheet(t, dorisID, f.eventID(t, dorisID), logQuery{}, nil, false).Body.String())

	by := fields(dialog)
	if got := chips(by["Remind me in"]); strings.Join(got, "|") != "1 day|2 days|3 days|5 days|21 days (usual)" {
		t.Errorf("Remind me in offers %v, want the five days the skip holds among the chips", got)
	}
	if got := checked(by["Remind me in"]); got != "5 days" {
		t.Errorf("Remind me in is %q, want the five days the skip holds", got)
	}
}

func TestCorrect_TheSheetSaysWhoLoggedTheCareAndWhen(t *testing.T) {
	f := rosewoodCorrections(t)
	dialog := dialogElement.FindString(f.openSheet(t, nigelID, f.eventID(t, nigelID), logQuery{}, nil, false).Body.String())

	m := loggedLead.FindStringSubmatch(dialog)
	if m == nil {
		t.Fatalf("the sheet says nothing about who recorded the care:\n%s", dialog)
	}
	if got := text(m[1]); got != "Logged by you yesterday at 6:00pm." {
		t.Errorf("the sheet says %q", got)
	}
}

func TestCorrect_CareEnteredADayAfterItWasGivenNamesBothDays(t *testing.T) {
	f := rosewoodCorrections(t)
	// Nigel was fed yesterday evening and written down this morning.
	f.exec(t, "UPDATE care_event SET recorded_at = $2 WHERE plant_id = $1", nigelID, at(time.September, 3, 8, 0))
	dialog := dialogElement.FindString(f.openSheet(t, nigelID, f.eventID(t, nigelID), logQuery{}, nil, false).Body.String())

	m := loggedLead.FindStringSubmatch(dialog)
	if m == nil {
		t.Fatalf("the sheet says nothing about who recorded the care:\n%s", dialog)
	}
	if got := text(m[1]); got != "Logged by you today for yesterday at 6:00pm." {
		t.Errorf("the sheet says %q, want the day it was entered as well as the day it happened", got)
	}
}

func TestCorrect_ACorrectionThatMovesTheTimeMovesTheRowToItsNewDay(t *testing.T) {
	f := rosewoodCorrections(t)
	// Big Fella was watered at half past seven this morning.
	if got := dayOf(f.show(t), "Big Fella"); got != "Today" {
		t.Fatalf("Big Fella's row is under %q, want Today", got)
	}

	rec := f.save(t, bigFellaID, f.eventID(t, bigFellaID), logQuery{}, url.Values{
		"care": {"water"}, "outcome": {"done"}, "when": {"yesterday"}, "time": {"20:00"},
	}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	page := f.show(t)
	if got := dayOf(page, "Big Fella"); got != "Yesterday" {
		t.Errorf("Big Fella's row is under %q, want Yesterday", got)
	}
	for _, i := range logEntries(page) {
		if strings.HasPrefix(i.text, "Today · ") && i.text != "Today · 1" {
			t.Errorf("today's marker says %q, want the one event it still holds", i.text)
		}
	}
}

func TestCorrect_ACorrectionChangesTheCareTheRowNames(t *testing.T) {
	f := rosewoodCorrections(t)
	rec := f.save(t, bigFellaID, f.eventID(t, bigFellaID), logQuery{}, url.Values{
		"care": {"feed"}, "outcome": {"done"}, "when": {"today"}, "time": {"07:30"},
	}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	page := f.show(t)
	for _, i := range logEntries(page) {
		if strings.HasPrefix(i.text, "Big Fella") && !strings.Contains(i.text, "You fed") {
			t.Errorf("Big Fella's row says %q, want the feed it was corrected to", i.text)
		}
	}
}

func TestCorrect_ASaveWithATimeLaterThanNowIsRefusedAndTheSheetSaysThatIsLaterThanNow(t *testing.T) {
	f := rosewoodCorrections(t)
	rec := f.save(t, bigFellaID, f.eventID(t, bigFellaID), logQuery{}, url.Values{
		"care": {"water"}, "outcome": {"done"}, "when": {"today"}, "time": {"23:00"},
	}, true)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if rec.Header().Get("HX-Retarget") != "#sheet" {
		t.Error("the refusal is not aimed back at the sheet")
	}
	dialog := dialogElement.FindString(rec.Body.String())
	if !strings.Contains(fields(dialog)["When"], `<p class="field__error">That time is in the future.</p>`) {
		t.Errorf("the sheet does not say the time is later than now:\n%s", text(dialog))
	}
	if got := dayOf(f.show(t), "Big Fella"); got != "Today" {
		t.Errorf("Big Fella's row moved to %q, want the refused save to have written nothing", got)
	}
}

func TestCorrect_ASavedCorrectionReturnsToTheLogItWasOpenedOver(t *testing.T) {
	f := rosewoodCorrections(t)
	q := logQuery{plant: &bigFellaID}
	rec := f.save(t, bigFellaID, f.eventID(t, bigFellaID), q, url.Values{
		"care": {"water"}, "outcome": {"done"}, "when": {"today"}, "time": {"07:30"},
	}, false)

	if got := rec.Header().Get("Location"); got != q.href() {
		t.Errorf("the save redirects to %q, want the log it was opened over", got)
	}
}

func TestCorrect_ASavedCorrectionSwapsTheLogAndClosesTheSheet(t *testing.T) {
	f := rosewoodCorrections(t)
	rec := f.save(t, bigFellaID, f.eventID(t, bigFellaID), logQuery{}, url.Values{
		"care": {"water"}, "outcome": {"done"}, "when": {"yesterday"}, "time": {"20:00"},
	}, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.HasPrefix(body, "<!doctype html>") {
		t.Errorf("the response is the whole page, want the log body alone:\n%.120s", body)
	}
	if !strings.Contains(body, `id="log-body"`) {
		t.Errorf("the response does not hold the log body:\n%.120s", body)
	}
	if !strings.Contains(body, `<div id="sheet" hx-swap-oob="true"></div>`) {
		t.Error("the response does not close the sheet")
	}
	if got := dayOf(body, "Big Fella"); got != "Yesterday" {
		t.Errorf("the swapped log has Big Fella under %q, want Yesterday", got)
	}
}

func TestCorrect_ADeletedEventIsNotOnTheLog(t *testing.T) {
	f := rosewoodCorrections(t)
	rec := f.remove(t, bigFellaID, f.eventID(t, bigFellaID), logQuery{}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	page := f.show(t)
	if got := dayOf(page, "Big Fella"); got != "" {
		t.Errorf("Big Fella's row is still on the log, under %q", got)
	}
}

func TestCorrect_TheRowADeleteLeavesSaysDeletedAndOffersUndo(t *testing.T) {
	f := rosewoodCorrections(t)
	eventID := f.eventID(t, bigFellaID)
	rec := f.remove(t, bigFellaID, eventID, logQuery{}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	rows := textOf(logEntries(body), eventKind)
	if len(rows) != 1 {
		t.Fatalf("the response holds %d rows, want the one that was deleted:\n%s", len(rows), body)
	}
	if got := rows[0]; got != "Big Fella Deleted Undo" {
		t.Errorf("the row says %q, want the plant, Deleted and an Undo button", got)
	}
	if !strings.Contains(body, `id="`+eventRowID(eventID)+`"`) {
		t.Error("the row does not keep the id the delete was aimed at")
	}
	if !strings.Contains(body, `action="`+eventPath(bigFellaID, eventID, "/restore", logQuery{})+`"`) {
		t.Error("Undo does not post to the event's restore URL")
	}
	if !strings.Contains(body, `<div id="sheet" hx-swap-oob="true"></div>`) {
		t.Error("the response does not close the sheet the Delete was pressed in")
	}
}

func TestCorrect_UndoPutsTheEventBackAsItWasLogged(t *testing.T) {
	f := rosewoodCorrections(t)
	// Doris was skipped by Sam, so the restore has to keep somebody else's name
	// on the row.
	eventID := f.eventID(t, dorisID)
	deleted := f.remove(t, dorisID, eventID, logQuery{}, true).Body.String()

	rec := f.restore(t, dorisID, eventID, hiddenFields(deleted), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	page := f.show(t)
	var row string
	for _, i := range logEntries(page) {
		if strings.HasPrefix(i.text, "Doris") {
			row = i.text
		}
	}
	if !strings.Contains(row, "Sam skipped") {
		t.Errorf("the restored row says %q, want Sam as the person who skipped the watering", row)
	}
	if !strings.Contains(row, "Reminder in 2 days") {
		t.Errorf("the restored row says %q, want the two days the skip was recorded with", row)
	}
}

func TestCorrect_ASheetOpenedUnderACareTypeTheGardenDoesNotHaveIsNotFound(t *testing.T) {
	f := rosewoodCorrections(t)
	eventID := f.eventID(t, bigFellaID)

	rec := f.openSheet(t, bigFellaID, eventID, logQuery{care: "prune"}, nil, false)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, the same as the log itself gives that URL", rec.Code, http.StatusNotFound)
	}
}

func TestCorrect_AnEventRestoredTwiceIsNotFound(t *testing.T) {
	f := rosewoodCorrections(t)
	eventID := f.eventID(t, bigFellaID)
	form := hiddenFields(f.remove(t, bigFellaID, eventID, logQuery{}, true).Body.String())

	f.restore(t, bigFellaID, eventID, form, true)
	rec := f.restore(t, bigFellaID, eventID, form, true)

	if rec.Code != http.StatusNotFound {
		t.Errorf("the second restore got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCorrect_ARestoreNamingACareTypeTheGardenDoesNotHaveIsNotFound(t *testing.T) {
	f := rosewoodCorrections(t)
	eventID := f.eventID(t, bigFellaID)
	form := hiddenFields(f.remove(t, bigFellaID, eventID, logQuery{}, true).Body.String())
	// A care type id that is not one of Rosewood's.
	form.Set("care", fairviewWaterID.String())

	rec := f.restore(t, bigFellaID, eventID, form, true)

	if rec.Code != http.StatusNotFound {
		t.Errorf("the restore got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCorrect_ASitterMayNotCorrectCareSomebodyElseGave(t *testing.T) {
	f := rosewoodSitter(t)
	// Doris was skipped by Sam, and the reader is not Sam.
	eventID := f.eventID(t, dorisID)

	if got := f.openSheet(t, dorisID, eventID, logQuery{}, nil, false).Code; got != http.StatusNotFound {
		t.Errorf("the sheet over somebody else's care got %d, want %d", got, http.StatusNotFound)
	}
	rec := f.save(t, dorisID, eventID, logQuery{}, url.Values{"care": {"water"}, "outcome": {"done"}, "when": {"today"}, "time": {"06:15"}}, false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("saving somebody else's care got %d, want %d", rec.Code, http.StatusNotFound)
	}

	if strings.Contains(f.show(t), eventPath(dorisID, eventID, "", logQuery{})) {
		t.Error("the log links to a sheet the reader may not open")
	}
}

func TestCorrect_ASitterMayNotRestoreCareSomebodyElseGave(t *testing.T) {
	f := rosewoodCorrections(t)
	// The owner deletes Sam's skip and reads the fields off the row, then a
	// sitter posts them.
	eventID := f.eventID(t, dorisID)
	form := hiddenFields(f.remove(t, dorisID, eventID, logQuery{}, true).Body.String())

	delete(f.principal.Capabilities, auth.CareDeleteAny)
	rec := f.restore(t, dorisID, eventID, form, true)

	if rec.Code != http.StatusNotFound {
		t.Errorf("restoring somebody else's care got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCorrect_ACareTypeArchivedSinceIsStillOnTheSheet(t *testing.T) {
	f := rosewoodCorrections(t)
	// Nigel was fed, and feeding has been turned off since. An archived care
	// type is left out of the garden's list, and the event still holds it.
	f.exec(t, "UPDATE care_type SET archived_at = now() WHERE garden_id = $1 AND slug = 'feed'", rosewoodID)
	rec := f.openSheet(t, nigelID, f.eventID(t, nigelID), logQuery{}, nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	by := fields(dialogElement.FindString(rec.Body.String()))
	if got := chips(by["Care"]); strings.Join(got, "|") != "Water|Feed" {
		t.Errorf("Care offers %v, want the feed the event was logged as among them", got)
	}
	if m := pressedButton.FindStringSubmatch(by["Care"]); m == nil || m[1] != "Feed" {
		t.Errorf("Care does not have Feed pressed:\n%s", by["Care"])
	}
}

func TestCorrect_ACorrectionKeepsACareTypeArchivedSince(t *testing.T) {
	f := rosewoodCorrections(t)
	f.exec(t, "UPDATE care_type SET archived_at = now() WHERE garden_id = $1 AND slug = 'feed'", rosewoodID)

	rec := f.save(t, nigelID, f.eventID(t, nigelID), logQuery{}, url.Values{
		"care": {"feed"}, "outcome": {"done"}, "when": {"today"}, "time": {"07:00"},
	}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	page := f.show(t)
	if got := dayOf(page, "Nigel"); got != "Today" {
		t.Errorf("Nigel's row is under %q, want Today", got)
	}
	for _, i := range logEntries(page) {
		if strings.HasPrefix(i.text, "Nigel") && !strings.Contains(i.text, "You fed") {
			t.Errorf("Nigel's row says %q, want the feed it still holds", i.text)
		}
	}
}

func TestCorrect_UndoPutsBackAnEventOfACareTypeArchivedSince(t *testing.T) {
	f := rosewoodCorrections(t)
	f.exec(t, "UPDATE care_type SET archived_at = now() WHERE garden_id = $1 AND slug = 'feed'", rosewoodID)
	eventID := f.eventID(t, nigelID)
	form := hiddenFields(f.remove(t, nigelID, eventID, logQuery{}, true).Body.String())

	rec := f.restore(t, nigelID, eventID, form, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	if got := dayOf(f.show(t), "Nigel"); got != "Yesterday" {
		t.Errorf("Nigel's row is under %q, want the Yesterday it was recorded under", got)
	}
}

func TestCorrect_ARestoreDatedLaterThanNowIsRefused(t *testing.T) {
	f := rosewoodCorrections(t)
	// Every field of a restore arrives with the request, and the sheet refuses
	// care given later than now, so this route has to as well.
	eventID := f.eventID(t, bigFellaID)
	form := hiddenFields(f.remove(t, bigFellaID, eventID, logQuery{}, true).Body.String())
	form.Set("at", thursday.Add(time.Hour).Format(time.RFC3339Nano))

	rec := f.restore(t, bigFellaID, eventID, form, true)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("the restore got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := dayOf(f.show(t), "Big Fella"); got != "" {
		t.Errorf("Big Fella's row is on the log under %q, want the refused restore to have written nothing", got)
	}
}

func TestCorrect_ARestoreOfASkipWithNoIntervalIsRefused(t *testing.T) {
	f := rosewoodCorrections(t)
	// A skip is the interval it put the care off by, and the sheet always
	// records one.
	eventID := f.eventID(t, dorisID)
	form := hiddenFields(f.remove(t, dorisID, eventID, logQuery{}, true).Body.String())
	form.Set("again", "")

	rec := f.restore(t, dorisID, eventID, form, true)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("the restore got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCorrect_ADeleteOfCareSomebodyElseGaveIsNotFound(t *testing.T) {
	f := rosewoodSitter(t)
	// Doris was skipped by Sam, and the reader is not Sam.
	rec := f.remove(t, dorisID, f.eventID(t, dorisID), logQuery{}, true)

	if rec.Code != http.StatusNotFound {
		t.Errorf("deleting somebody else's care got %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := dayOf(f.show(t), "Doris"); got != "Today" {
		t.Errorf("Doris's row is under %q, want the refused delete to have left it on the log", got)
	}
}

func TestCorrect_ASheetOverCareTheReaderMayNotDeleteHasNoDeleteButton(t *testing.T) {
	// This reader may correct anybody's care and delete only their own, so
	// Doris's skip, which Sam logged, opens a sheet that saves but does not
	// offer Delete.
	f := grant(rosewoodLog(t), auth.CareEditOwn, auth.CareEditAny, auth.CareDeleteOwn)
	rec := f.openSheet(t, dorisID, f.eventID(t, dorisID), logQuery{}, nil, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	dialog := dialogElement.FindString(rec.Body.String())
	if !strings.Contains(dialog, ">Save changes</button>") {
		t.Error("the sheet the reader may correct does not offer Save changes")
	}
	if strings.Contains(dialog, ">Delete</button>") {
		t.Error("the sheet offers Delete on care the reader may not delete")
	}
}

func TestCorrect_TheSheetOverAnOlderEventHoldsTheDayAndTimeItHappened(t *testing.T) {
	f := rosewoodCorrections(t)
	// Trail Mix was watered at nine in the morning, thirteen days ago. Anything
	// older than yesterday is edited on the day-and-time field rather than a
	// chip, so that field has to hold the day the care happened.
	dialog := dialogElement.FindString(f.openSheet(t, trailMixID, f.eventID(t, trailMixID), logQuery{}, nil, false).Body.String())

	if got := checked(fields(dialog)["When"]); got != "Another day" {
		t.Errorf("When is %q, want Another day", got)
	}
	if !strings.Contains(dialog, `name="at" value="2026-08-21T09:00"`) {
		t.Errorf("the day and time field does not hold the morning of 21 August:\n%s", dialog)
	}
}
