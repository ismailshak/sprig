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

func (f *todayFixture) sheetRequest(t *testing.T, path string) *http.Request {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	req.SetPathValue("plant", strings.Split(strings.TrimPrefix(path, "/plants/"), "/")[0])
	return req
}

func (f *todayFixture) sheet(t *testing.T, path string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	req := f.sheetRequest(t, path)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	f.handler.sheet(rec, req)
	return rec
}

func (f *todayFixture) sheetTargeting(t *testing.T, path, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := f.sheetRequest(t, path)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	rec := httptest.NewRecorder()
	f.handler.sheet(rec, req)
	return rec
}

func (f *todayFixture) post(t *testing.T, plantID string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/plants/"+plantID+"/log", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("plant", plantID)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	f.handler.log(rec, req)
	return rec
}

func (f *todayFixture) undo(t *testing.T, plantID, eventID uuid.UUID, slug string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodDelete, undoPath(plantID, eventID, slug), nil)
	req.SetPathValue("plant", plantID.String())
	req.SetPathValue("event", eventID.String())
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	f.handler.undo(rec, req)
	return rec
}

// settled is Today asked for by a row whose undo window has closed.
func (f *todayFixture) settled(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, todayPath, nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	rec := httptest.NewRecorder()
	f.handler.show(rec, req)
	return rec
}

func (f *todayFixture) events(t *testing.T, plantID uuid.UUID) []store.CareEvent {
	t.Helper()

	rows, err := f.tx.Query(t.Context(), "SELECT id, garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, note, override_interval_days FROM care_event WHERE plant_id = $1 ORDER BY recorded_at", plantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []store.CareEvent
	for rows.Next() {
		var e store.CareEvent
		if err := rows.Scan(&e.ID, &e.GardenID, &e.PlantID, &e.CareTypeID, &e.PerformedBy, &e.PerformedAt, &e.RecordedAt, &e.Done, &e.Note, &e.OverrideIntervalDays); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func (f *todayFixture) latest(t *testing.T, plantID uuid.UUID) store.CareEvent {
	t.Helper()
	events := f.events(t, plantID)
	if len(events) != 2 {
		t.Fatalf("the plant has %d events, want the seeded one and the posted one", len(events))
	}
	return events[1]
}

var (
	dialogElement   = regexp.MustCompile(`(?s)<dialog[^>]*>.*?</dialog>`)
	fieldsetElement = regexp.MustCompile(`(?s)<fieldset[^>]*>\s*<legend[^>]*>([^<]+)</legend>.*?</fieldset>`)
	chipElement     = regexp.MustCompile(`(?s)<(?:label|button)[^>]*class="chip"[^>]*>(.*?)</(?:label|button)>`)
	checkedInput    = regexp.MustCompile(`<input[^>]*\bchecked\b[^>]*>`)
	pressedButton   = regexp.MustCompile(`aria-pressed="true"[^>]*>([^<]+)<`)
)

func fields(markup string) map[string]string {
	byLegend := map[string]string{}
	for _, m := range fieldsetElement.FindAllStringSubmatch(markup, -1) {
		byLegend[strings.TrimSpace(m[1])] = m[0]
	}
	return byLegend
}

func chips(fieldset string) []string {
	var out []string
	for _, m := range chipElement.FindAllStringSubmatch(fieldset, -1) {
		out = append(out, text(m[1]))
	}
	return out
}

func checked(fieldset string) string {
	for _, m := range chipElement.FindAllStringSubmatch(fieldset, -1) {
		if checkedInput.MatchString(m[0]) {
			return text(m[1])
		}
	}
	return ""
}

func TestSheet_OpensFilledInForTheRowItWasOpenedFrom(t *testing.T) {
	f := rosewood(t)
	rec := f.sheet(t, sheetPath(nigelID, "water"), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	dialog := dialogElement.FindString(page)
	if dialog == "" {
		t.Fatalf("the page holds no dialog:\n%s", page)
	}

	if !strings.Contains(dialog, `<dialog open class="sheet" id="sheet" aria-label="Log care for Nigel">`) {
		t.Errorf("the dialog is not open under the sheet's id and named for Nigel:\n%s", dialog)
	}
	if !regexp.MustCompile(`<h1[^>]*>Rosewood</h1>`).MatchString(page) {
		t.Error("the sheet is not drawn over Today")
	}
	heading := regexp.MustCompile(`(?s)<a class="sheet__plant" href="/plants/` + nigelID.String() + `">.*?</a>`).FindString(dialog)
	if got := text(heading); got != "Nigel Boston fern · Bathroom" {
		t.Errorf("the heading says %q, want the name, the species and the room, linked to the plant", got)
	}

	by := fields(dialog)
	if got := chips(by["What"]); strings.Join(got, "|") != "Water|Feed" {
		t.Errorf("What offers %v, want the two cares Nigel is scheduled for", got)
	}
	if m := pressedButton.FindStringSubmatch(by["What"]); m == nil || m[1] != "Water" {
		t.Errorf("What does not have Water pressed:\n%s", by["What"])
	}
	if got := checked(by["Outcome"]); got != "Done" {
		t.Errorf("Outcome starts on %q, want Done", got)
	}
	if got := chips(by["When it happened"]); strings.Join(got, "|") != "Just now|Earlier today|Yesterday|Another day" {
		t.Errorf("When offers %v", got)
	}
	if got := checked(by["When it happened"]); got != "Just now" {
		t.Errorf("When starts on %q, want Just now", got)
	}
	if got := chips(by["Ask again in"]); strings.Join(got, "|") != "1 day|2 days|3 days|The usual 4 days" {
		t.Errorf("Ask again in offers %v, want three short re-checks and Nigel's own four days", got)
	}
	if got := checked(by["Ask again in"]); got != "2 days" {
		t.Errorf("Ask again in starts on %q, want 2 days", got)
	}
	if !strings.Contains(dialog, `<button class="sheet__go sheet__done" name="care" value="water">Log watering</button>`) {
		t.Error("the primary button is not Log watering carrying the care")
	}
	if !strings.Contains(dialog, `hx-target="#`+rowID(nigelID)+`"`) {
		t.Error("the post does not aim at the row the sheet was opened from")
	}
	// 08:00 UTC on the fixture's Thursday is 09:00 in London.
	if !strings.Contains(dialog, `name="time" value="09:00"`) || !strings.Contains(dialog, `name="at" value="2026-09-03T09:00"`) {
		t.Errorf("the pickers do not start at now in London:\n%s", by["When it happened"])
	}
}

func TestSheet_APlantWithOneCareIsNotAskedWhat(t *testing.T) {
	f := rosewood(t)
	dialog := dialogElement.FindString(f.sheet(t, sheetPath(dorisID, "water"), false).Body.String())
	if _, ok := fields(dialog)["What"]; ok {
		t.Error("Doris is only watered, and the sheet asks What")
	}
	if !strings.Contains(dialog, ">Log watering</button>") {
		t.Error("the primary button is not Log watering")
	}
}

func TestSheet_SwitchingWhatKeepsTheDraftAndNamesThatCaresUsual(t *testing.T) {
	f := rosewood(t)
	query := url.Values{
		"row": {"water"}, "care": {"feed"}, "outcome": {"skipped"}, "again": {"3"}, "note": {"Soil still damp"},
	}
	dialog := dialogElement.FindString(f.sheet(t, logPath(nigelID)+"?"+query.Encode(), false).Body.String())
	by := fields(dialog)

	if m := pressedButton.FindStringSubmatch(by["What"]); m == nil || m[1] != "Feed" {
		t.Errorf("What does not have Feed pressed:\n%s", by["What"])
	}
	if got := checked(by["Outcome"]); got != "Skipped" {
		t.Errorf("Outcome is %q after the switch, want Skipped kept", got)
	}
	if got := chips(by["Ask again in"]); strings.Join(got, "|") != "1 day|2 days|3 days|The usual 21 days" {
		t.Errorf("Ask again in offers %v, want the feeding's three weeks as the usual", got)
	}
	if got := checked(by["Ask again in"]); got != "3 days" {
		t.Errorf("Ask again in is %q after the switch, want 3 days kept", got)
	}
	if !strings.Contains(dialog, `value="Soil still damp"`) {
		t.Error("the note was lost in the switch")
	}
	if !strings.Contains(dialog, `<button class="sheet__go sheet__skipped" name="care" value="feed">Record a skip</button>`) {
		t.Error("the primary button is not Record a skip carrying the feed")
	}
	if !strings.Contains(dialog, `<input type="hidden" name="row" value="water">`) || !strings.Contains(dialog, `hx-target="#`+rowID(nigelID)+`"`) {
		t.Error("the sheet forgot which row it was opened from")
	}
}

func TestSheet_ACareWithNoUsualOffersAWeek(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "UPDATE care_schedule SET interval_count = 1, interval_unit = 'month' WHERE plant_id = $1", spikeID)
	dialog := dialogElement.FindString(f.sheet(t, sheetPath(spikeID, "water"), false).Body.String())
	if got := chips(fields(dialog)["Ask again in"]); strings.Join(got, "|") != "1 day|2 days|3 days|7 days" {
		t.Errorf("Ask again in offers %v, want a plain week where a month is not a number of days", got)
	}

	f.exec(t, "UPDATE care_schedule SET interval_count = 2, interval_unit = 'day' WHERE plant_id = $1", spikeID)
	dialog = dialogElement.FindString(f.sheet(t, sheetPath(spikeID, "water"), false).Body.String())
	if got := chips(fields(dialog)["Ask again in"]); strings.Join(got, "|") != "1 day|The usual 2 days|3 days" {
		t.Errorf("Ask again in offers %v, want the usual to take the short chip's place", got)
	}
}

func TestSheet_ASwapGetsTheSheetAloneAndANavigationThePage(t *testing.T) {
	f := rosewood(t)

	swap := f.sheet(t, sheetPath(dorisID, "water"), true).Body.String()
	if !strings.HasPrefix(swap, "<dialog") || strings.Contains(swap, "<html") {
		t.Errorf("a swap did not get the dialog alone:\n%.200s", swap)
	}
	page := f.sheet(t, sheetPath(dorisID, "water"), false).Body.String()
	if !strings.HasPrefix(page, "<!doctype html>") || !strings.Contains(page, "<dialog") {
		t.Errorf("a navigation did not get the page with the dialog in it:\n%.200s", page)
	}
}

func TestSheet_ASwapThatNamesTheFormGetsTheFormAlone(t *testing.T) {
	f := rosewood(t)

	swap := f.sheetTargeting(t, sheetPath(nigelID, "water"), sheetFormID).Body.String()
	if !strings.HasPrefix(swap, `<form class="sheet__form" id="`+sheetFormID+`"`) {
		t.Errorf("a swap naming the form did not start with it:\n%.200s", swap)
	}
	if strings.Contains(swap, "<dialog") {
		t.Errorf("a swap naming the form carried the dialog, which would replay its animation:\n%.200s", swap)
	}
}

func TestSheet_RefusesWhatItCouldNotHaveSent(t *testing.T) {
	f := rosewood(t)
	cases := []struct {
		name string
		path string
		want int
	}{
		{"a plant the garden does not have", sheetPath(uuid.MustParse("00000000-0000-7000-8000-000000000999"), "water"), http.StatusNotFound},
		{"a path that is not an id", "/plants/nigel/log?row=water", http.StatusNotFound},
		{"a care the plant is not scheduled for", sheetPath(dorisID, "feed"), http.StatusBadRequest},
		{"an outcome that is not one of the two", logPath(dorisID) + "?row=water&outcome=maybe", http.StatusBadRequest},
		{"a day that is not one of the four", logPath(dorisID) + "?row=water&when=later", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := f.sheet(t, c.path, false); rec.Code != c.want {
				t.Errorf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}

	t.Run("an archived plant is no plant", func(t *testing.T) {
		f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", dorisID)
		if rec := f.sheet(t, sheetPath(dorisID, "water"), false); rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestLog_JustNowRecordsBothInstantsAsNow(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}, "when": {"now"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	e := f.latest(t, dorisID)
	if !e.PerformedAt.Equal(thursday) || !e.RecordedAt.Equal(thursday) {
		t.Errorf("performed %v and recorded %v, want both at %v", e.PerformedAt, e.RecordedAt, thursday)
	}
	if !e.Done || e.Note != nil || e.OverrideIntervalDays != nil || e.CareTypeID != waterID || e.PerformedBy != readerID {
		t.Errorf("the event is %+v, want a watering by the reader with no note and no override", e)
	}

	body := rec.Body.String()
	row := rowElement.FindString(body)
	if !strings.Contains(row, `id="`+rowID(dorisID)+`"`) || !strings.Contains(row, "row--done") {
		t.Errorf("the swap is not Doris's row in its logged state:\n%s", body)
	}
	if got := text(row); got != "Doris Watered just now Undo" {
		t.Errorf("the row says %q, want what was recorded and the button that reverses it", got)
	}
	if strings.Contains(row, "<a ") || strings.Contains(row, "<form") {
		t.Error("a logged row still offers the sheet or its care button")
	}
	if !strings.Contains(body, `<div id="sheet" hx-swap-oob="true"></div>`) {
		t.Error("the swap does not close the sheet")
	}
}

// The stored instant never equals a clock with nanoseconds on it because
// Postgres keeps microseconds.
func TestLog_JustNowIsSaidWhateverTheClocksPrecision(t *testing.T) {
	f := rosewood(t)
	f.handler.now = func() time.Time { return thursday.Add(123456789 * time.Nanosecond) }
	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}, "when": {"now"}}, true)
	if got := text(rowElement.FindString(rec.Body.String())); got != "Doris Watered just now Undo" {
		t.Errorf("the row says %q", got)
	}
}

func TestLog_AFormPostIsSentBackToToday(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, dorisID.String(), url.Values{"care": {"water"}}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("got %d to %q, want %d to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	f.latest(t, dorisID)
}

func TestLog_ABackdatedTimeIsReadInTheReadersDay(t *testing.T) {
	cases := []struct {
		name     string
		timezone string
		form     url.Values
		want     time.Time
		said     string
	}{
		{"earlier today is today's date with the clock given", "Europe/London",
			url.Values{"when": {"today"}, "time": {"07:30"}},
			time.Date(2026, time.September, 3, 7, 30, 0, 0, london()), "Watered Thu 3 Sep, 7:30am"},
		{"yesterday is the day before with the clock given", "Europe/London",
			url.Values{"when": {"yesterday"}, "time": {"18:00"}},
			time.Date(2026, time.September, 2, 18, 0, 0, 0, london()), "Watered Wed 2 Sep, 6:00pm"},
		{"another day is the day and clock given", "Europe/London",
			url.Values{"when": {"other"}, "at": {"2026-08-30T09:15"}},
			time.Date(2026, time.August, 30, 9, 15, 0, 0, london()), "Watered Sun 30 Aug, 9:15am"},
		// 08:00 UTC on Thursday is 20:00 in Auckland. The reader's yesterday
		// is Wednesday evening.
		{"yesterday is the reader's yesterday", "Pacific/Auckland",
			url.Values{"when": {"yesterday"}, "time": {"18:00"}},
			time.Date(2026, time.September, 2, 6, 0, 0, 0, time.UTC), "Watered Wed 2 Sep, 6:00pm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.principal.User.Timezone = c.timezone
			form := url.Values{"row": {"water"}, "care": {"water"}}
			for k, v := range c.form {
				form[k] = v
			}
			rec := f.post(t, dorisID.String(), form, true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
			}
			e := f.latest(t, dorisID)
			if !e.PerformedAt.Equal(c.want) {
				t.Errorf("performed at %v, want %v", e.PerformedAt.In(time.UTC), c.want.In(time.UTC))
			}
			if !e.RecordedAt.Equal(thursday) {
				t.Errorf("recorded at %v, want now", e.RecordedAt)
			}
			if got := text(rowElement.FindString(rec.Body.String())); got != "Doris "+c.said+" Undo" {
				t.Errorf("the row says %q, want %q", got, "Doris "+c.said+" Undo")
			}
		})
	}
}

func TestLog_ATimeThatHasNotHappenedIsRefused(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"a clock later than now", url.Values{"when": {"today"}, "time": {"23:00"}}, "That is later than now."},
		{"a day later than now", url.Values{"when": {"other"}, "at": {"2026-09-04T09:00"}}, "That is later than now."},
		{"no clock", url.Values{"when": {"yesterday"}, "time": {""}}, "Give a time."},
		{"no day", url.Values{"when": {"other"}, "at": {"soon"}}, "Give a day and a time."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			form := url.Values{"row": {"water"}, "care": {"water"}}
			for k, v := range c.form {
				form[k] = v
			}
			rec := f.post(t, dorisID.String(), form, true)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
			}
			if rec.Header().Get("HX-Retarget") != "#sheet" || rec.Header().Get("HX-Reswap") != "outerHTML" {
				t.Error("the refusal is not aimed back at the sheet")
			}
			dialog := dialogElement.FindString(rec.Body.String())
			if !strings.Contains(fields(dialog)["When it happened"], `<p class="field__error">`+c.want+`</p>`) {
				t.Errorf("the sheet does not say %q under When:\n%s", c.want, text(dialog))
			}
			if checked(fields(dialog)["When it happened"]) != map[string]string{"today": "Earlier today", "yesterday": "Yesterday", "other": "Another day"}[c.form.Get("when")] {
				t.Error("the sheet came back on a different day")
			}
			if n := len(f.events(t, dorisID)); n != 1 {
				t.Errorf("the plant has %d events, want the refused post to have written none", n)
			}
		})
	}

	t.Run("a form post gets the page back", func(t *testing.T) {
		f := rosewood(t)
		rec := f.post(t, dorisID.String(), url.Values{"care": {"water"}, "when": {"today"}, "time": {"23:00"}}, false)
		if rec.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(rec.Body.String(), "<!doctype html>") {
			t.Errorf("got %d and %.40q, want the page under the sheet", rec.Code, rec.Body.String())
		}
	})
}

func TestLog_ASkipStoresTheDaysToAskAgainIn(t *testing.T) {
	t.Run("a short re-check", func(t *testing.T) {
		f := rosewood(t)
		rec := f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"water"}, "outcome": {"skipped"}, "again": {"2"}, "note": {"  Soil still damp "}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
		}
		e := f.latest(t, nigelID)
		if e.Done || e.OverrideIntervalDays == nil || *e.OverrideIntervalDays != 2 {
			t.Errorf("the event is %+v, want a skip asking again in 2 days", e)
		}
		if e.Note == nil || *e.Note != "Soil still damp" {
			t.Errorf("the note is %v, want it trimmed", e.Note)
		}
		if got := text(rowElement.FindString(rec.Body.String())); got != "Nigel Skipped · asking again in 2 days Undo" {
			t.Errorf("the row says %q", got)
		}
	})

	t.Run("the usual interval", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, nigelID.String(), url.Values{"care": {"water"}, "outcome": {"skipped"}, "again": {"4"}}, true)
		if e := f.latest(t, nigelID); *e.OverrideIntervalDays != 4 {
			t.Errorf("the override is %d, want Nigel's own 4", *e.OverrideIntervalDays)
		}
	})

	t.Run("the usual of the care selected, not the row's", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}, "outcome": {"skipped"}, "again": {"21"}}, true)
		if e := f.latest(t, nigelID); *e.OverrideIntervalDays != 21 || e.CareTypeID != feedID {
			t.Errorf("the event is %+v, want a feed skipped for its own three weeks", e)
		}
	})

	// One fixture per test because a second transaction inserting the same
	// garden waits on the first to finish.
	for _, again := range []string{"0", "-1", "5", "4.5", "usual"} {
		t.Run("a duration the chips did not offer, "+again, func(t *testing.T) {
			f := rosewood(t)
			rec := f.post(t, nigelID.String(), url.Values{"care": {"water"}, "outcome": {"skipped"}, "again": {again}}, true)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("again=%s got %d, want %d", again, rec.Code, http.StatusBadRequest)
			}
			if n := len(f.events(t, nigelID)); n != 1 {
				t.Errorf("again=%s wrote an event", again)
			}
		})
	}

	t.Run("a done care carries no override however the chips were left", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, nigelID.String(), url.Values{"care": {"water"}, "outcome": {"done"}, "again": {"3"}}, true)
		if e := f.latest(t, nigelID); !e.Done || e.OverrideIntervalDays != nil {
			t.Errorf("the event is %+v", e)
		}
	})
}

func TestLog_ASheetOpenedOverOneRowCanLogAnotherCare(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if e := f.latest(t, nigelID); e.CareTypeID != feedID {
		t.Error("the event is not a feed")
	}
	row := rowElement.FindString(rec.Body.String())
	if !strings.Contains(row, `id="`+rowID(nigelID)+`"`) {
		t.Errorf("the swap is not the watering row the sheet was over:\n%s", row)
	}
	if got := text(row); got != "Nigel Fed just now Undo" {
		t.Errorf("the row says %q", got)
	}
}

func TestLog_RefusesWhatTheSheetCouldNotHaveSent(t *testing.T) {
	cases := []struct {
		name  string
		plant string
		form  url.Values
		want  int
	}{
		{"no care", dorisID.String(), url.Values{"when": {"now"}}, http.StatusBadRequest},
		{"a care the plant is not scheduled for", dorisID.String(), url.Values{"care": {"feed"}}, http.StatusBadRequest},
		{"a care the garden does not have", dorisID.String(), url.Values{"care": {"prune"}}, http.StatusBadRequest},
		{"an outcome that is not one of the two", dorisID.String(), url.Values{"care": {"water"}, "outcome": {"maybe"}}, http.StatusBadRequest},
		{"a day that is not one of the four", dorisID.String(), url.Values{"care": {"water"}, "when": {"later"}}, http.StatusBadRequest},
		{"a plant the garden does not have", "00000000-0000-7000-8000-000000000999", url.Values{"care": {"water"}}, http.StatusNotFound},
		{"a path that is not an id", "doris", url.Values{"care": {"water"}}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			if rec := f.post(t, c.plant, c.form, true); rec.Code != c.want {
				t.Errorf("status = %d, want %d:\n%s", rec.Code, c.want, rec.Body.String())
			}
			if n := len(f.events(t, dorisID)); n != 1 {
				t.Error("the refused post wrote an event")
			}
		})
	}
}

func TestToday_ARowOffersItsTwoTargets(t *testing.T) {
	page := rosewood(t).show(t)
	byID, _ := sections(page)
	row := rowElement.FindString(byID["due-today"][strings.Index(byID["due-today"], `id="`+rowID(dorisID)+`"`)-20:])

	if !strings.Contains(row, `<a class="row__open" href="`+sheetPath(dorisID, "water")+`" hx-get="`+sheetPath(dorisID, "water")+`" hx-target="#sheet" hx-swap="outerHTML">`) {
		t.Errorf("the row does not open the sheet for its care:\n%s", row)
	}
	if !strings.Contains(row, `<form method="post" action="`+logPath(dorisID)+`" hx-post="`+logPath(dorisID)+`" hx-target="#`+rowID(dorisID)+`" hx-swap="outerHTML settle:0ms">`) {
		t.Errorf("the care button does not post to the plant's log and swap the row:\n%s", row)
	}
	for _, want := range []string{`name="when" value="now"`, `name="row" value="water"`, `name="care" value="water"`} {
		if !strings.Contains(row, want) {
			t.Errorf("the care button's form lacks %s", want)
		}
	}
	if !strings.Contains(page, `<div id="sheet"></div>`) {
		t.Error("the page has no slot for the sheet to land in")
	}
}

func TestLog_TheLoggedRowCarriesItsUndoWindow(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	row := rowElement.FindString(rec.Body.String())

	for _, want := range []string{
		`style="--grace:4000ms"`,
		`hx-get="/"`,
		`hx-trigger="load delay:4000ms"`,
		`hx-swap="delete swap:320ms settle:0ms"`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the row lacks %s, so its window has no %s:\n%s", want, map[bool]string{true: "bar", false: "end"}[strings.Contains(want, "grace")], row)
		}
	}
	if want := `hx-delete="` + undoPath(dorisID, f.latest(t, dorisID).ID, "water") + `"`; !strings.Contains(row, want) {
		t.Errorf("Undo does not delete the event that was just written:\n%s", row)
	}
	if !strings.Contains(row, `hx-target="#`+rowID(dorisID)+`" hx-swap="outerHTML settle:0ms"`) {
		t.Errorf("Undo does not swap the row it sits in:\n%s", row)
	}
}

func TestUndo_TakesTheEventBackOutAndTheRowWithIt(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)

	rec := f.undo(t, dorisID, event.ID, "water", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if n := len(f.events(t, dorisID)); n != 1 {
		t.Errorf("the plant has %d events, want the seeded one alone", n)
	}

	row := rowElement.FindString(rec.Body.String())
	if got := text(row); got != "Doris Bedroom Water" {
		t.Errorf("the row says %q, want the row that was there before the event", got)
	}
	if strings.Contains(row, "row--done") || strings.Contains(row, "--grace") {
		t.Errorf("the row came back still logged:\n%s", row)
	}
}

// The row on the page has to come back even where the sheet logged a care it
// was not opened for.
func TestUndo_GivesBackTheRowTheSheetWasOpenedFrom(t *testing.T) {
	f := rosewood(t)
	f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}}, true)
	event := f.latest(t, nigelID)

	row := rowElement.FindString(f.undo(t, nigelID, event.ID, "water", true).Body.String())
	if !strings.Contains(row, `id="`+rowID(nigelID)+`"`) {
		t.Errorf("the swap is not the watering row:\n%s", row)
	}
	if got := text(row); got != "Nigel Bathroom Water" {
		t.Errorf("the row says %q, want Nigel's watering as it was", got)
	}
}

func TestUndo_RefusesAnEventItCannotFind(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)
	sam := uuid.MustParse("00000000-0000-7000-8000-000000000199")
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", sam)

	cases := []struct {
		name    string
		plant   uuid.UUID
		event   uuid.UUID
		arrange func()
	}{
		{name: "an event of somebody else's, from a reader who may only delete their own", plant: dorisID, event: event.ID, arrange: func() {
			f.exec(t, "UPDATE care_event SET performed_by = $1 WHERE id = $2", sam, event.ID)
		}},
		{name: "an event that is not there", plant: dorisID, event: uuid.MustParse("00000000-0000-7000-8000-000000000998")},
		{name: "an event of another plant's", plant: nigelID, event: event.ID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.arrange != nil {
				c.arrange()
			}
			if rec := f.undo(t, c.plant, c.event, "water", true); rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if n := len(f.events(t, dorisID)); n != 2 {
				t.Errorf("the plant has %d events, want the refused undo to have deleted none", n)
			}
		})
	}
}

func TestUndo_AnOwnerMayTakeBackWhatAnybodyLogged(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)
	sam := uuid.MustParse("00000000-0000-7000-8000-000000000199")
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", sam)
	f.exec(t, "UPDATE care_event SET performed_by = $1 WHERE id = $2", sam, event.ID)
	f.principal.Capabilities[auth.CareDeleteAny] = true

	if rec := f.undo(t, dorisID, event.ID, "water", true); rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if n := len(f.events(t, dorisID)); n != 1 {
		t.Errorf("the plant has %d events, want Sam's deleted", n)
	}
}

func TestUndo_AFormPostIsSentBackToToday(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)

	rec := f.undo(t, dorisID, event.ID, "water", false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Errorf("got %d to %q, want %d to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	if n := len(f.events(t, dorisID)); n != 1 {
		t.Errorf("the plant has %d events, want the event deleted", n)
	}
}

func TestWindow_TheHeadComesBackWithTheRowItLeavesAbove(t *testing.T) {
	t.Run("a day with work left counts what is left", func(t *testing.T) {
		f := rosewood(t)
		head := headElement.FindString(f.post(t, dorisID.String(), url.Values{"care": {"water"}}, true).Body.String())
		if got := text(head); got != "2 plants need you today, 1 of them overdue." {
			t.Errorf("the head says %q, want the count without Doris", got)
		}
		if !strings.Contains(head, `hx-swap-oob="true"`) {
			t.Errorf("the head is not swapped out of band, so nothing but the row would move:\n%s", head)
		}
	})

	t.Run("a day whose rows are all logged says so rather than emptying", func(t *testing.T) {
		f := rosewood(t)
		f.water(t, bigFellaID)
		f.water(t, nigelID)
		head := headElement.FindString(f.post(t, dorisID.String(), url.Values{"care": {"water"}}, true).Body.String())
		if got := text(head); got != "All done for today." {
			t.Errorf("the head says %q, want the tick that waits for the windows to close", got)
		}
		if strings.Contains(head, "Nothing else is due.") {
			t.Error("the empty screen was drawn over rows that are still on the page")
		}
	})

	t.Run("an undo puts back what it took away", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
		event := f.latest(t, dorisID)
		head := headElement.FindString(f.undo(t, dorisID, event.ID, "water", true).Body.String())
		if got := text(head); got != "3 plants need you today, 1 of them overdue." {
			t.Errorf("the head says %q, want Doris counted again", got)
		}
	})
}

func TestWindow_AClosedWindowAsksTodayForWhatTheRowsLeavingChanged(t *testing.T) {
	t.Run("the head alone while another row is still inside its window", func(t *testing.T) {
		f := rosewood(t)
		f.water(t, bigFellaID)
		f.water(t, dorisID)
		f.water(t, nigelID)

		body := f.settled(t, rowID(dorisID)).Body.String()
		if strings.Contains(body, `id="day"`) {
			t.Errorf("the feed came back under rows that are still on the page:\n%s", body)
		}
		if got := text(body); got != "All done for today." {
			t.Errorf("the answer says %q, want the tick", got)
		}
	})

	t.Run("the whole feed once no window is open", func(t *testing.T) {
		f := rosewood(t)
		f.water(t, bigFellaID)
		f.water(t, dorisID)
		f.water(t, nigelID)
		f.exec(t, "UPDATE care_event SET recorded_at = recorded_at - interval '1 minute'")

		body := f.settled(t, rowID(dorisID)).Body.String()
		if !strings.Contains(body, `<div class="app__body app__body--feed" id="day" hx-swap-oob="true">`) {
			t.Errorf("the feed did not come back as an out-of-band swap:\n%.200s", body)
		}
		if !strings.Contains(body, "All done for today") || !strings.Contains(body, "Nothing else is due.") {
			t.Errorf("the empty screen is not in the feed:\n%s", text(body))
		}
		// Watering Nigel puts his row under Coming up rather than off the feed
		// because his watering is every four days.
		for _, plant := range []uuid.UUID{bigFellaID, dorisID} {
			if strings.Contains(body, rowID(plant)) {
				t.Errorf("%s is still on the feed after its window closed", rowID(plant))
			}
		}
		if strings.Contains(body, "row--done") {
			t.Errorf("a row came back logged:\n%s", body)
		}
		if strings.HasPrefix(body, "<!doctype html>") {
			t.Error("a row that has settled was answered with the whole page")
		}
	})
}

// A reload is a fresh read because the undo window lives only in the page the
// swap left behind.
func TestWindow_ANavigationDrawsNoRowInsideAWindow(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"care": {"water"}}, true)

	page := f.show(t)
	if strings.Contains(page, rowID(dorisID)) {
		t.Error("Doris is still on the page after being watered")
	}
	for _, unwanted := range []string{"--grace", "All done for today.", "row--done"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the page carries %s, which belongs to a row that was swapped in", unwanted)
		}
	}
}
