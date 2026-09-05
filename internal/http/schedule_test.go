package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

func (f *plantFixture) editorRequest(t *testing.T, method, path string, plantID uuid.UUID, slug string, body url.Values, htmx bool) *http.Request {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequestWithContext(ctx, method, path, nil)
	}
	req.SetPathValue("plant", plantID.String())
	req.SetPathValue("care", slug)
	if htmx {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", scheduleRowPrefix+slug)
	}
	return req
}

// open GETs the editor, with query holding whatever the row's controls sent.
func (f *plantFixture) open(t *testing.T, plantID uuid.UUID, slug string, query url.Values) *httptest.ResponseRecorder {
	t.Helper()

	path := schedulePath(plantID, slug)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	rec := httptest.NewRecorder()
	f.handler.editSchedule(rec, f.editorRequest(t, http.MethodGet, path, plantID, slug, nil, false))
	return rec
}

func (f *plantFixture) ask(t *testing.T, plantID uuid.UUID, slug string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	f.handler.confirmRemoveSchedule(rec, f.editorRequest(t, http.MethodGet, removeSchedulePath(plantID, slug), plantID, slug, nil, false))
	return rec
}

func (f *plantFixture) save(t *testing.T, plantID uuid.UUID, slug string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	f.handler.saveSchedule(rec, f.editorRequest(t, http.MethodPost, schedulePath(plantID, slug), plantID, slug, form, htmx))
	return rec
}

func (f *plantFixture) remove(t *testing.T, plantID uuid.UUID, slug string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	f.handler.removeSchedule(rec, f.editorRequest(t, http.MethodPost, removeSchedulePath(plantID, slug), plantID, slug, nil, htmx))
	return rec
}

// cadence is the form a browser posts from a row set to Repeats, which has the
// interval and season fields and not the date fields.
func cadence(slug, every, unit string) url.Values {
	return url.Values{
		slug + "-shape": {shapeCadence},
		slug + "-every": {every},
		slug + "-unit":  {unit},
	}
}

var (
	editingRow = regexp.MustCompile(`(?s)<li class="sched sched--editing" id="([^"]*)">(.*?)</li>`)
	schedHint  = regexp.MustCompile(`<p class="sched__hint">(.*?)</p>`)
	schedError = regexp.MustCompile(`<p class="field__error">(.*?)</p>`)
	schedAsk   = regexp.MustCompile(`<span class="sched__ask">(.*?)</span>`)
	pickedOpt  = regexp.MustCompile(`<option value="([^"]*)" selected>`)
)

// testEditor is the editor's state read back from its control values rather
// than the surrounding markup.
type testEditor struct {
	id string
	// picked is each select's value, keyed by the part of the control's name
	// after the care type.
	picked map[string]string
	// every is the number field's value as a string, since a rejected value is
	// shown back as typed.
	every   string
	hint    string
	message string
	ask     string
	remove  bool
	cancel  bool
}

func editorOf(t *testing.T, page, slug string) testEditor {
	t.Helper()

	m := editingRow.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no row is open as the editor:\n%s", page)
	}
	e := testEditor{id: m[1], picked: map[string]string{}}
	for _, part := range []string{"shape", "unit", "day", "month", "year", "from", "to"} {
		control := regexp.MustCompile(`(?s)<select [^>]*id="` + slug + `-` + part + `"[^>]*>(.*?)</select>`).FindStringSubmatch(m[2])
		if control == nil {
			continue
		}
		if picked := pickedOpt.FindStringSubmatch(control[1]); picked != nil {
			e.picked[part] = picked[1]
		}
	}
	if number := regexp.MustCompile(`<input [^>]*id="` + slug + `-every"[^>]*value="([^"]*)"`).FindStringSubmatch(m[2]); number != nil {
		e.every = number[1]
	}
	if hint := schedHint.FindStringSubmatch(m[2]); hint != nil {
		e.hint = text(hint[1])
	}
	if message := schedError.FindStringSubmatch(m[2]); message != nil {
		e.message = text(message[1])
	}
	if ask := schedAsk.FindStringSubmatch(m[2]); ask != nil {
		e.ask = text(ask[1])
	}
	e.remove = strings.Contains(m[2], ">Remove<")
	e.cancel = strings.Contains(m[2], ">Cancel<")
	return e
}

// storedSchedule returns the care_schedule row for a plant and care type, or
// false if there is none.
func (f *plantFixture) storedSchedule(t *testing.T, plantID, careTypeID uuid.UUID) (store.CareSchedule, bool) {
	t.Helper()

	rows, err := store.New(f.tx).ListCareSchedules(t.Context(), rosewoodID)
	if err != nil {
		t.Fatalf("listing the schedules: %v", err)
	}
	for _, row := range rows {
		if row.CareSchedule.PlantID == plantID && row.CareSchedule.CareTypeID == careTypeID {
			return row.CareSchedule, true
		}
	}
	return store.CareSchedule{}, false
}

// Big Fella is watered every ten days and was last watered on 22 August, so
// the row is two days late on the fixture's Thursday.
func TestScheduleEditor_TheEditorOpensOnTheCurrentSchedule(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.open(t, bigFellaID, "water", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	editor := editorOf(t, rec.Body.String(), "water")
	// The row's id is the same open or closed, since that is what a swap
	// targets.
	if editor.id != scheduleRowPrefix+"water" {
		t.Errorf("the editor carries the id %q, want the row's own", editor.id)
	}
	if editor.picked["shape"] != shapeCadence || editor.every != "10" || editor.picked["unit"] != "day" {
		t.Errorf("the editor opened on %v every %q, want a cadence of 10 days", editor.picked, editor.every)
	}
	if editor.hint != "Counted from the last time it was done" {
		t.Errorf("the sentence under the select reads %q", editor.hint)
	}
	if !editor.remove || !editor.cancel {
		t.Errorf("the foot offers Remove = %v and Cancel = %v, want both", editor.remove, editor.cancel)
	}
}

// Big Fella has no feeding schedule, so its feeding row reads Not scheduled.
func TestScheduleEditor_AnUnscheduledCareOpensOnWeeklyWithNoRemoveLink(t *testing.T) {
	f := rosewoodPlant(t)

	editor := editorOf(t, f.open(t, bigFellaID, "feed", nil).Body.String(), "feed")

	if editor.picked["shape"] != shapeCadence || editor.every != "1" || editor.picked["unit"] != "week" {
		t.Errorf("the editor opened on %v every %q, want a cadence of one week", editor.picked, editor.every)
	}
	if editor.remove {
		t.Error("the foot offered Remove for a care type the plant has no schedule for")
	}
}

func TestScheduleEditor_ChoosingAShapeReRendersTheRowWithThatShapesFields(t *testing.T) {
	f := rosewoodPlant(t)

	query := cadence("water", "10", "day")
	query.Set("water-shape", shapeOnce)
	editor := editorOf(t, f.open(t, bigFellaID, "water", query).Body.String(), "water")

	if editor.every != "" {
		t.Errorf("a one-off kept the interval field holding %q", editor.every)
	}
	if editor.picked["month"] == "" || editor.picked["year"] == "" {
		t.Errorf("a one-off drew no date, and picked %v", editor.picked)
	}
	if editor.hint != "One date, and then nothing" {
		t.Errorf("the sentence under the select reads %q", editor.hint)
	}
}

func TestScheduleEditor_SavingAnIntervalUpdatesTheDueDateOnTheReturnedRow(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.save(t, bigFellaID, "water", cadence("water", "20", "day"), true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	// The last watering is unchanged, so changing ten days to twenty pushes
	// the next one ten days out. It is not recomputed from today.
	row := rowFor(t, rec.Body.String(), "Water")
	if row.rule != "Every 20 days" || row.when != "Due in 8 days" {
		t.Errorf("the watering row reads %+v, want every 20 days and due in 8 days", row)
	}
}

func TestScheduleEditor_SavingAnUnscheduledCareMakesItScheduled(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.save(t, bigFellaID, "feed", cadence("feed", "3", "week"), true)

	row := rowFor(t, rec.Body.String(), "Feed")
	if row.rule != "Every 3 weeks" {
		t.Errorf("the feeding row reads %+v, want every 3 weeks", row)
	}
	// A schedule with no events counts from when it was set, so the first
	// feeding is due one interval ahead rather than overdue immediately. The
	// exact count is not asserted because set_at is the database's clock and
	// the row is read against the fixture's.
	if row.late || !strings.HasPrefix(row.when, "Due in") {
		t.Errorf("the feeding row is due %q and late = %v, want a day still to come", row.when, row.late)
	}
}

func TestScheduleEditor_ASeasonIsSavedOnlyWhenTheBoxIsTicked(t *testing.T) {
	f := rosewoodPlant(t)

	form := cadence("feed", "3", "week")
	form.Set("feed-seasonal", "on")
	form.Set("feed-from", "3")
	form.Set("feed-to", "9")
	rec := f.save(t, bigFellaID, "feed", form, true)

	if row := rowFor(t, rec.Body.String(), "Feed"); row.rule != "Every 3 weeks · Mar–Sep" {
		t.Errorf("the feeding row reads %+v, want the window beside the interval", row)
	}
	stored, ok := f.storedSchedule(t, bigFellaID, feedID)
	if !ok || stored.SeasonStartMonth == nil || *stored.SeasonStartMonth != 3 || *stored.SeasonEndMonth != 9 {
		t.Errorf("the stored season is %v to %v, want March to September", stored.SeasonStartMonth, stored.SeasonEndMonth)
	}
}

// The day select's first option is Any day. The schema cannot store a month
// without a day, so the anchor is stored on the 1st.
func TestScheduleEditor_AnAnchorWithNoDayIsStoredOnTheFirstOfItsMonth(t *testing.T) {
	f := rosewoodPlant(t)

	form := url.Values{
		"feed-shape": {shapeOnce},
		"feed-day":   {"0"},
		"feed-month": {"3"},
		"feed-year":  {"2028"},
	}
	rec := f.save(t, bigFellaID, "feed", form, true)

	if row := rowFor(t, rec.Body.String(), "Feed"); row.rule != "Just once" || row.when != "Due in March 2028" {
		t.Errorf("the feeding row reads %+v, want a one-off named by its month", row)
	}
	stored, ok := f.storedSchedule(t, bigFellaID, feedID)
	if !ok {
		t.Fatal("the save wrote no schedule")
	}
	if stored.AnchorDate.Format(time.DateOnly) != "2028-03-01" || *stored.AnchorPrecision != "month" {
		t.Errorf("the anchor is %v at %q precision, want 2028-03-01 to the month", stored.AnchorDate, *stored.AnchorPrecision)
	}
	if stored.IntervalCount != nil {
		t.Errorf("a one-off was stored with an interval of %d", *stored.IntervalCount)
	}
}

// The schema's interval_count > 0 check is a backstop. The editor refuses a
// zero itself and shows it back in the field.
func TestScheduleEditor_AnIntervalOfZeroIsRefusedAndShownBackInTheField(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.save(t, bigFellaID, "water", cadence("water", "0", "day"), true)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	editor := editorOf(t, rec.Body.String(), "water")
	if editor.message != "Give a number between 1 and 999." {
		t.Errorf("the row says %q, want a sentence naming the range", editor.message)
	}
	if editor.every != "0" {
		t.Errorf("the field holds %q, want the 0 that was typed", editor.every)
	}
	stored, _ := f.storedSchedule(t, bigFellaID, waterID)
	if *stored.IntervalCount != 10 {
		t.Errorf("the stored interval is %d, want the 10 the refused post left alone", *stored.IntervalCount)
	}
}

func TestScheduleEditor_AShapeWithNoDateIsRefusedWithTheDateFieldsShown(t *testing.T) {
	f := rosewoodPlant(t)

	// A browser posts only the controls it rendered, so a shape picked without
	// JavaScript arrives without the fields that shape needs.
	rec := f.save(t, bigFellaID, "water", url.Values{"water-shape": {shapeOnce}}, true)

	editor := editorOf(t, rec.Body.String(), "water")
	if editor.message != "Give it a date." {
		t.Errorf("the row says %q, want a sentence asking for the date", editor.message)
	}
	if editor.picked["month"] == "" || editor.picked["year"] == "" {
		t.Errorf("the refused row drew no date to answer with, and picked %v", editor.picked)
	}
}

func TestScheduleEditor_TheRemoveConfirmationRendersInTheRowWithTheScheduleStillShown(t *testing.T) {
	f := rosewoodPlant(t)

	editor := editorOf(t, f.ask(t, bigFellaID, "water").Body.String(), "water")

	if editor.ask != "Remove this schedule?" {
		t.Errorf("the foot asks %q", editor.ask)
	}
	if editor.cancel {
		t.Error("the question was asked beside the buttons it replaces")
	}
	if _, ok := f.storedSchedule(t, bigFellaID, waterID); !ok {
		t.Error("asking the question removed the schedule")
	}
}

func TestScheduleEditor_RemovingAScheduleMovesTheCareTypeToNotScheduled(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.remove(t, bigFellaID, "water", true)

	if row := rowFor(t, rec.Body.String(), "Water"); row.rule != "" || row.when != "Not scheduled" {
		t.Errorf("the watering row reads %+v, want a care the plant is not scheduled for", row)
	}
	if _, ok := f.storedSchedule(t, bigFellaID, waterID); ok {
		t.Error("the schedule is still stored")
	}
	// The events the schedule produced are kept.
	if recent := recentOf(f.page(t, bigFellaID)); len(recent) == 0 {
		t.Error("removing the schedule took the plant's history with it")
	}
}

func TestScheduleEditor_RemovingAScheduleThatDoesNotExistIs404(t *testing.T) {
	f := rosewoodPlant(t)

	if rec := f.remove(t, bigFellaID, "feed", true); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestScheduleEditor_AnHTMXRequestGetsTheRowAlone(t *testing.T) {
	f := rosewoodPlant(t)

	body := f.save(t, bigFellaID, "water", cadence("water", "20", "day"), true).Body.String()

	if strings.Contains(body, "<h1") || strings.Contains(body, "Reference") {
		t.Errorf("the swap carried the page around the row:\n%s", body)
	}
	if rows := scheduleOf(t, body); len(rows) != 1 {
		t.Errorf("the swap carried %d rows, want the one it replaces", len(rows))
	}
}

func TestScheduleEditor_ASaveWithoutJavaScriptRedirectsToThePlant(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.save(t, bigFellaID, "water", cadence("water", "20", "day"), false)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != plantPath(bigFellaID) {
		t.Errorf("the save answered %d to %q, want %d to the plant", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
}

func TestScheduleEditor_ARemovalWithoutJavaScriptRedirectsToThePlant(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.remove(t, bigFellaID, "water", false)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != plantPath(bigFellaID) {
		t.Errorf("the removal answered %d to %q, want %d to the plant", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
}

// The year select starts at this year, so a schedule anchored earlier would
// otherwise re-render under a year nobody chose.
func TestScheduleEditor_AnAnchorSetBeforeThisYearKeepsItsYear(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, anchor_date, anchor_precision) VALUES ($1, $2, $3, '2024-05-01', 'month')",
		rosewoodID, bigFellaID, feedID)

	editor := editorOf(t, f.open(t, bigFellaID, "feed", nil).Body.String(), "feed")

	if editor.picked["year"] != "2024" || editor.picked["month"] != "5" || editor.picked["day"] != "0" {
		t.Errorf("the editor opened on %v, want May 2024 with no day", editor.picked)
	}
}

func TestScheduleEditor_AFixedDateScheduleOpensWithItsIntervalAndDate(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, anchor_date, anchor_precision) VALUES ($1, $2, $3, 1, 'year', '2027-05-01', 'day')",
		rosewoodID, bigFellaID, feedID)

	editor := editorOf(t, f.open(t, bigFellaID, "feed", nil).Body.String(), "feed")

	if editor.picked["shape"] != shapeDate || editor.every != "1" || editor.picked["unit"] != "year" {
		t.Errorf("the editor opened on %v every %q, want a fixed date every year", editor.picked, editor.every)
	}
	if editor.picked["day"] != "1" || editor.picked["month"] != "5" || editor.picked["year"] != "2027" {
		t.Errorf("the editor opened on %v, want 1 May 2027", editor.picked)
	}
}

func TestScheduleEditor_ASeasonalScheduleOpensWithItsMonths(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, season_start_month, season_end_month) VALUES ($1, $2, $3, 3, 'week', 3, 9)",
		rosewoodID, bigFellaID, feedID)

	editor := editorOf(t, f.open(t, bigFellaID, "feed", nil).Body.String(), "feed")

	if editor.picked["shape"] != shapeCadence || editor.every != "3" || editor.picked["unit"] != "week" {
		t.Errorf("the editor opened on %v every %q, want a cadence of 3 weeks", editor.picked, editor.every)
	}
	// The month selects are rendered only when the box is ticked, so months in
	// the post imply the box is ticked too.
	if editor.picked["from"] != "3" || editor.picked["to"] != "9" {
		t.Errorf("the editor opened on %v, want March to September", editor.picked)
	}
}

// A one-off that has been done reads "Not scheduled", and the editor opens as
// for an unscheduled care rather than on the completed date.
func TestScheduleEditor_ACompletedOneOffOpensAsUnscheduled(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, anchor_date, anchor_precision, set_at) VALUES ($1, $2, $3, '2026-08-20', 'day', $4)",
		rosewoodID, bigFellaID, feedID, day(time.August, 1))
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, bigFellaID, feedID, readerID, day(time.August, 21))

	editor := editorOf(t, f.open(t, bigFellaID, "feed", nil).Body.String(), "feed")

	if editor.picked["shape"] != shapeCadence || editor.every != "1" || editor.picked["unit"] != "week" {
		t.Errorf("the editor opened on %v every %q, want a cadence of one week", editor.picked, editor.every)
	}
	if editor.remove {
		t.Error("the foot offered Remove for a schedule the row says is not there")
	}
}

// A one-off that has been done reads "Not scheduled". Saving updates set_at,
// so the new date makes it scheduled again.
func TestScheduleEditor_SavingADateOnACompletedOneOffReschedulesIt(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, anchor_date, anchor_precision, set_at) VALUES ($1, $2, $3, '2026-08-20', 'day', $4)",
		rosewoodID, bigFellaID, feedID, day(time.August, 1))
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, bigFellaID, feedID, readerID, day(time.August, 21))

	form := url.Values{
		"feed-shape": {shapeOnce},
		"feed-day":   {"1"},
		"feed-month": {"11"},
		"feed-year":  {"2026"},
	}
	rec := f.save(t, bigFellaID, "feed", form, true)

	if row := rowFor(t, rec.Body.String(), "Feed"); row.rule != "Just once" || row.when != "Due 1 November" {
		t.Errorf("the feeding row reads %+v, want a one-off due on 1 November", row)
	}
}

func TestScheduleEditor_AnArchivedPlantsScheduleEditorIs404(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if rec := f.open(t, bigFellaID, "water", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := f.save(t, bigFellaID, "water", cadence("water", "20", "day"), true); rec.Code != http.StatusNotFound {
		t.Errorf("the save answered %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestScheduleEditor_ACareTypeTheGardenDoesNotHaveIs404(t *testing.T) {
	f := rosewoodPlant(t)

	if rec := f.open(t, bigFellaID, "prune", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// Unscheduled rows exist only to be clicked, so a reader who cannot edit does
// not see them, and the scheduled rows they do see are not links.
func TestPlant_AReaderWhoMayNotEditSchedulesSeesNoEditLinks(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	page := f.page(t, bigFellaID)

	for _, row := range scheduleOf(t, page) {
		if row.when == "Not scheduled" {
			t.Errorf("the page offered a %s row to a reader who may not open it", row.care)
		}
	}
	if strings.Contains(page, schedulePath(bigFellaID, "water")) {
		t.Error("a schedule row led to the editor for a reader who may not change one")
	}
}

// Cancel is a link to the plant's page without JavaScript and an htmx swap
// targeting the row with it. The plant route handles both.
func TestPlant_AnHTMXRequestTargetingAScheduleRowGetsThatRowAlone(t *testing.T) {
	f := rosewoodPlant(t)

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, plantPath(bigFellaID), nil)
	req.SetPathValue("plant", bigFellaID.String())
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", scheduleRowPrefix+"water")
	rec := httptest.NewRecorder()
	f.handler.plant(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<h1") {
		t.Errorf("the swap carried the page around the row:\n%s", body)
	}
	rows := scheduleOf(t, body)
	if len(rows) != 1 || rows[0].care != "Water" {
		t.Errorf("the swap carried %v, want the watering row alone", rows)
	}
}

func TestPlant_AnHTMXRequestTargetingARowThePageDoesNotHaveIs404(t *testing.T) {
	f := rosewoodPlant(t)

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, plantPath(bigFellaID), nil)
	req.SetPathValue("plant", bigFellaID.String())
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", scheduleRowPrefix+"prune")
	rec := httptest.NewRecorder()
	f.handler.plant(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
