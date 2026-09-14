package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/push"
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

// settled requests Today the way a row whose grace window has closed does:
// an htmx GET targeting that row.
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

// sheetIn returns the element with the id sheet in a response body. That is
// the open dialog, or the empty element that closes it.
func sheetIn(body string) *element {
	return readHTML(body).byID("sheet")
}

// sheetFields returns each fieldset in the sheet by the text of its legend.
func sheetFields(sheet *element) map[string]*element {
	byLegend := map[string]*element{}
	for _, fieldset := range sheet.all(isTag("fieldset")) {
		byLegend[fieldset.first(isTag("legend")).text()] = fieldset
	}
	return byLegend
}

// chipLabels returns the text of each label in a fieldset, in page order.
func chipLabels(fieldset *element) []string {
	var out []string
	for _, label := range fieldset.all(isTag("label")) {
		out = append(out, label.text())
	}
	return out
}

// checkedValue returns the value of the checked input in a fieldset, or ""
// when none is checked.
func checkedValue(fieldset *element) string {
	return fieldset.first(isTag("input"), hasAttr("checked")).attr("value")
}

// remindMeIn returns the Remind me in fieldset for one care type. The form
// holds one for every care type it offers and the stylesheet shows the chosen
// type's.
func remindMeIn(sheet *element, care string) *element {
	for _, fieldset := range sheet.all(isTag("fieldset")) {
		if fieldset.first(attrIs("name", againField(care))) != nil {
			return fieldset
		}
	}
	return nil
}

func TestSheet_OpensOnTheCareOfTheRowItWasOpenedFrom(t *testing.T) {
	f := rosewood(t)
	rec := f.sheet(t, sheetPath(nigelID, "water"), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	page := readHTML(rec.Body.String())
	sheet := page.byID("sheet")
	if sheet == nil || sheet.tag != "dialog" {
		t.Fatalf("the page holds no dialog under the sheet's id:\n%s", rec.Body.String())
	}

	if !sheet.has("open") || sheet.attr("aria-label") != "Log care for Nigel" {
		t.Errorf("the dialog is not open and named for Nigel:\n%s", sheet)
	}
	if got := page.first(isTag("h1")).text(); got != "Rosewood" {
		t.Errorf("the page's heading is %q, want the sheet over Today, headed Rosewood", got)
	}
	if got := sheet.first(isTag("a"), attrIs("href", plantPath(nigelID))).text(); got != "Nigel Boston fern · Bathroom" {
		t.Errorf("the heading says %q, want the name, the species and the room, linked to the plant", got)
	}

	by := sheetFields(sheet)
	if got := chipLabels(by["Care"]); strings.Join(got, "|") != "Water|Feed" {
		t.Errorf("Care offers %v, want the two cares Nigel is scheduled for", got)
	}
	if got := checkedValue(by["Care"]); got != "water" {
		t.Errorf("Care starts on %q, want water", got)
	}
	if got := checkedValue(by["Outcome"]); got != "done" {
		t.Errorf("Outcome starts on %q, want done", got)
	}
	if got := chipLabels(by["When"]); strings.Join(got, "|") != "Just now|Earlier today|Yesterday|Another day" {
		t.Errorf("When offers %v", got)
	}
	if got := checkedValue(by["When"]); got != "now" {
		t.Errorf("When starts on %q, want now", got)
	}
	if got := chipLabels(remindMeIn(sheet, "water")); strings.Join(got, "|") != "1 day|2 days|3 days|4 days (usual)" {
		t.Errorf("Remind me in offers %v, want three short re-checks and Nigel's own four days", got)
	}
	if got := checkedValue(remindMeIn(sheet, "water")); got != "2" {
		t.Errorf("Remind me in starts on %q days, want 2", got)
	}
	if sheet.first(isTag("button"), textIs("Log watering")) == nil {
		t.Error("the sheet has no Log watering button")
	}
	if got := sheet.first(isTag("form"), hasAttr("hx-post")).attr("hx-target"); got != "#"+rowID(nigelID) {
		t.Errorf("the post targets %q, want the row the sheet was opened from", got)
	}
	// 08:00 UTC on the fixture's Thursday is 09:00 in London.
	if clock, at := sheet.first(attrIs("name", "time")).attr("value"), sheet.first(attrIs("name", "at")).attr("value"); clock != "09:00" || at != "2026-09-03T09:00" {
		t.Errorf("the pickers start at %q and %q, want now in London", clock, at)
	}
}

func TestSheet_APlantWithOneCareHasNoCareTypeChoice(t *testing.T) {
	f := rosewood(t)
	sheet := sheetIn(f.sheet(t, sheetPath(dorisID, "water"), false).Body.String())
	if _, ok := sheetFields(sheet)["Care"]; ok {
		t.Error("Doris is only watered, and the sheet asks Care")
	}
	if sheet.first(isTag("button"), textIs("Log watering")) == nil {
		t.Error("the sheet has no Log watering button")
	}
}

func TestSheet_HoldsEveryCareTypesReminderChipsAndLogButton(t *testing.T) {
	f := rosewood(t)
	sheet := sheetIn(f.sheet(t, sheetPath(nigelID, "water"), false).Body.String())

	if got := chipLabels(remindMeIn(sheet, "feed")); strings.Join(got, "|") != "1 day|2 days|3 days|21 days (usual)" {
		t.Errorf("Remind me in for feeding offers %v, want its three weeks as the usual", got)
	}
	if got := checkedValue(remindMeIn(sheet, "feed")); got != "2" {
		t.Errorf("Remind me in for feeding starts on %q days, want 2", got)
	}
	if sheet.first(isTag("button"), textIs("Log feeding")) == nil {
		t.Error("there is no Log feeding button for the stylesheet to show")
	}
	if sheet.first(isTag("button"), textIs("Log skip")) == nil {
		t.Error("there is no Log skip button")
	}
}

func TestSheet_ACareWithNoIntervalInDaysOffersSevenDays(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "UPDATE care_schedule SET interval_count = 1, interval_unit = 'month' WHERE plant_id = $1", spikeID)
	sheet := sheetIn(f.sheet(t, sheetPath(spikeID, "water"), false).Body.String())
	if got := chipLabels(remindMeIn(sheet, "water")); strings.Join(got, "|") != "1 day|2 days|3 days|7 days" {
		t.Errorf("Remind me in offers %v, want a plain week where a month is not a number of days", got)
	}

	f.exec(t, "UPDATE care_schedule SET interval_count = 2, interval_unit = 'day' WHERE plant_id = $1", spikeID)
	sheet = sheetIn(f.sheet(t, sheetPath(spikeID, "water"), false).Body.String())
	if got := chipLabels(remindMeIn(sheet, "water")); strings.Join(got, "|") != "1 day|2 days (usual)|3 days" {
		t.Errorf("Remind me in offers %v, want the usual to take the short chip's place", got)
	}
}

func TestSheet_AnHTMXRequestGetsTheSheetAloneAndANavigationTheWholePage(t *testing.T) {
	f := rosewood(t)

	swap := f.sheet(t, sheetPath(dorisID, "water"), true).Body.String()
	if top := readHTML(swap).first(); top == nil || top.tag != "dialog" || readHTML(swap).first(isTag("html")) != nil {
		t.Errorf("a swap did not get the dialog alone:\n%.200s", swap)
	}
	page := f.sheet(t, sheetPath(dorisID, "water"), false).Body.String()
	if readHTML(page).first(isTag("html")) == nil || readHTML(page).first(isTag("dialog")) == nil {
		t.Errorf("a navigation did not get the page with the dialog in it:\n%.200s", page)
	}
}

func TestSheet_AValueTheSheetDoesNotOfferIs400(t *testing.T) {
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

	t.Run("an archived plant is 404", func(t *testing.T) {
		f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", dorisID)
		if rec := f.sheet(t, sheetPath(dorisID, "water"), false); rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

// careRowIn returns the watering row for the plant in a response body, or nil.
func careRowIn(body string, plantID uuid.UUID) *element {
	return readHTML(body).byID(rowID(plantID))
}

func TestLog_JustNowRecordsPerformedAtAndRecordedAtAsNow(t *testing.T) {
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
	row := careRowIn(body, dorisID)
	if row == nil {
		t.Fatalf("the swap has no row for Doris:\n%s", body)
	}
	if got := row.text(); got != "Doris Watered just now Undo" {
		t.Errorf("the row says %q, want what was recorded and the button that reverses it", got)
	}
	if row.first(isTag("a")) != nil || row.first(isTag("form")) != nil {
		t.Error("a logged row still offers the sheet or its care button")
	}
	if sheet := sheetIn(body); sheet == nil || sheet.attr("hx-swap-oob") != "true" || len(sheet.children) != 0 {
		t.Errorf("the swap does not close the sheet: %s", sheet)
	}
}

// The stored time never equals a Go time with nanoseconds, because Postgres
// keeps microseconds.
func TestLog_JustNowIsShownWhateverThePrecisionOfTheStoredTime(t *testing.T) {
	f := rosewood(t)
	f.handler.now = func() time.Time { return thursday.Add(123456789 * time.Nanosecond) }
	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}, "when": {"now"}}, true)
	if got := careRowIn(rec.Body.String(), dorisID).text(); got != "Doris Watered just now Undo" {
		t.Errorf("the row says %q", got)
	}
}

// The app does not deduplicate. A repeat logged by mistake is deleted from
// Activity instead.
func TestLog_TheSameCareLoggedTwiceSecondsApartIsTwoEvents(t *testing.T) {
	f := rosewood(t)
	form := url.Values{"row": {"water"}, "care": {"water"}, "when": {"now"}}

	first := f.post(t, dorisID.String(), form, true)
	f.handler.now = func() time.Time { return thursday.Add(5 * time.Second) }
	second := f.post(t, dorisID.String(), form, true)

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("the two posts got %d and %d, want %d for both:\n%s", first.Code, second.Code, http.StatusOK, second.Body.String())
	}
	events := f.events(t, dorisID)
	if len(events) != 3 {
		t.Fatalf("Doris has %d events, want the seeded one and both logs", len(events))
	}
	logged := events[1:]
	if !logged[0].PerformedAt.Equal(thursday) || !logged[1].PerformedAt.Equal(thursday.Add(5*time.Second)) {
		t.Errorf("the logs were performed at %v and %v, want %v and %v",
			logged[0].PerformedAt, logged[1].PerformedAt, thursday, thursday.Add(5*time.Second))
	}
	if logged[0].ID == logged[1].ID {
		t.Error("the two logs are one event, want two")
	}
}

func TestLog_AFormPostRedirectsToToday(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, dorisID.String(), url.Values{"care": {"water"}}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("got %d to %q, want %d to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	f.latest(t, dorisID)
}

func TestLog_ABackdatedTimeIsReadInTheReadersTimezone(t *testing.T) {
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
			if got := careRowIn(rec.Body.String(), dorisID).text(); got != "Doris "+c.said+" Undo" {
				t.Errorf("the row says %q, want %q", got, "Doris "+c.said+" Undo")
			}
		})
	}
}

func TestLog_ATimeInTheFutureIsRefused(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"a clock later than now", url.Values{"when": {"today"}, "time": {"23:00"}}, "That time is in the future."},
		{"a day later than now", url.Values{"when": {"other"}, "at": {"2026-09-04T09:00"}}, "That time is in the future."},
		{"no clock", url.Values{"when": {"yesterday"}, "time": {""}}, "Enter a time."},
		{"no day", url.Values{"when": {"other"}, "at": {"soon"}}, "Enter a day and time."},
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
			when := sheetFields(sheetIn(rec.Body.String()))["When"]
			if !strings.Contains(when.text(), c.want) {
				t.Errorf("the sheet does not say %q under When:\n%s", c.want, text(rec.Body.String()))
			}
			if got := checkedValue(when); got != c.form.Get("when") {
				t.Errorf("the sheet came back on %q, want %q", got, c.form.Get("when"))
			}
			if n := len(f.events(t, dorisID)); n != 1 {
				t.Errorf("the plant has %d events, want the refused post to have written none", n)
			}
		})
	}

	t.Run("a form post gets the whole page", func(t *testing.T) {
		f := rosewood(t)
		rec := f.post(t, dorisID.String(), url.Values{"care": {"water"}, "when": {"today"}, "time": {"23:00"}}, false)
		if rec.Code != http.StatusUnprocessableEntity || readHTML(rec.Body.String()).first(isTag("html")) == nil {
			t.Errorf("got %d and %.40q, want the page under the sheet", rec.Code, rec.Body.String())
		}
	})
}

func TestLog_ASkipStoresTheOverrideIntervalInDays(t *testing.T) {
	t.Run("a one to three day interval", func(t *testing.T) {
		f := rosewood(t)
		rec := f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"water"}, "outcome": {"skipped"}, "again-water": {"2"}, "note": {"  Soil still damp "}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
		}
		e := f.latest(t, nigelID)
		if e.Done || e.OverrideIntervalDays == nil || *e.OverrideIntervalDays != 2 {
			t.Errorf("the event is %+v, want a skip with a reminder in 2 days", e)
		}
		if e.Note == nil || *e.Note != "Soil still damp" {
			t.Errorf("the note is %v, want it trimmed", e.Note)
		}
		if got := careRowIn(rec.Body.String(), nigelID).text(); got != "Nigel Skipped · reminder in 2 days Undo" {
			t.Errorf("the row says %q", got)
		}
	})

	t.Run("the usual interval", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, nigelID.String(), url.Values{"care": {"water"}, "outcome": {"skipped"}, "again-water": {"4"}}, true)
		if e := f.latest(t, nigelID); *e.OverrideIntervalDays != 4 {
			t.Errorf("the override is %d, want Nigel's own 4", *e.OverrideIntervalDays)
		}
	})

	t.Run("the usual interval of the selected care, not the row's", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}, "outcome": {"skipped"}, "again-feed": {"21"}}, true)
		if e := f.latest(t, nigelID); *e.OverrideIntervalDays != 21 || e.CareTypeID != feedID {
			t.Errorf("the event is %+v, want a feed skipped for its own three weeks", e)
		}
	})

	// One fixture per test because a second transaction inserting the same
	// garden waits on the first to finish.
	for _, again := range []string{"0", "-1", "5", "4.5", "usual"} {
		t.Run("an interval the chips did not offer, "+again, func(t *testing.T) {
			f := rosewood(t)
			rec := f.post(t, nigelID.String(), url.Values{"care": {"water"}, "outcome": {"skipped"}, "again-water": {again}}, true)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("again=%s got %d, want %d", again, rec.Code, http.StatusBadRequest)
			}
			if n := len(f.events(t, nigelID)); n != 1 {
				t.Errorf("again=%s wrote an event", again)
			}
		})
	}

	t.Run("a done care stores no override whatever chip was selected", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, nigelID.String(), url.Values{"care": {"water"}, "outcome": {"done"}, "again-water": {"3"}}, true)
		if e := f.latest(t, nigelID); !e.Done || e.OverrideIntervalDays != nil {
			t.Errorf("the event is %+v", e)
		}
	})
}

func TestLog_ASheetOpenedFromOneRowCanLogADifferentCare(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if e := f.latest(t, nigelID); e.CareTypeID != feedID {
		t.Error("the event is not a feed")
	}
	row := careRowIn(rec.Body.String(), nigelID)
	if row == nil {
		t.Fatalf("the swap is not the watering row the sheet was over:\n%s", rec.Body.String())
	}
	if got := row.text(); got != "Nigel Fed just now Undo" {
		t.Errorf("the row says %q", got)
	}
}

func TestLog_AValueTheSheetDoesNotOfferIs400(t *testing.T) {
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

func TestToday_ARowHasASheetLinkAndACareButtonThatSwapsTheRow(t *testing.T) {
	page := rosewood(t).show(t)
	byID, _ := sectionsOf(page)
	row := byID["due-today"].byID(rowID(dorisID))
	if row == nil {
		t.Fatalf("Due today has no row for Doris:\n%s", page)
	}

	link := row.first(isTag("a"))
	for _, want := range []struct{ name, value string }{
		{"href", sheetPath(dorisID, "water")},
		{"hx-get", sheetPath(dorisID, "water")},
		{"hx-target", "#sheet"},
		{"hx-swap", "outerHTML"},
	} {
		if got := link.attr(want.name); got != want.value {
			t.Errorf("the link that opens the sheet has %s %q, want %q", want.name, got, want.value)
		}
	}
	form := row.first(isTag("form"))
	for _, want := range []struct{ name, value string }{
		{"action", logPath(dorisID)},
		{"hx-post", logPath(dorisID)},
		{"hx-target", "#" + rowID(dorisID)},
		{"hx-swap", "outerHTML settle:0ms"},
	} {
		if got := form.attr(want.name); got != want.value {
			t.Errorf("the care button's form has %s %q, want %q", want.name, got, want.value)
		}
	}
	for _, field := range []struct{ name, value string }{{"when", "now"}, {"row", "water"}, {"care", "water"}} {
		if form.first(attrIs("name", field.name), attrIs("value", field.value)) == nil {
			t.Errorf("the care button's form does not post %s=%s", field.name, field.value)
		}
	}
	if sheet := sheetIn(page); sheet == nil || sheet.has("open") || len(sheet.children) != 0 {
		t.Error("the page has no empty element for the sheet to be swapped into")
	}
}

func TestLog_TheLoggedRowHasAnUndoButtonAndAGraceTimer(t *testing.T) {
	f := rosewood(t)
	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	row := careRowIn(rec.Body.String(), dorisID)

	for _, want := range []struct{ name, value, part string }{
		{"style", "--grace:4000ms", "bar"},
		{"hx-get", "/", "end"},
		{"hx-trigger", "load delay:4000ms", "end"},
		{"hx-swap", "delete swap:320ms settle:0ms", "end"},
	} {
		if got := row.attr(want.name); got != want.value {
			t.Errorf("the row's %s is %q, want %q. Its window has no %s.", want.name, got, want.value, want.part)
		}
	}
	undo := row.first(isTag("button"), textIs("Undo"))
	if got, want := undo.attr("hx-delete"), undoPath(dorisID, f.latest(t, dorisID).ID, "water"); got != want {
		t.Errorf("Undo deletes %q, want the event that was just written at %q", got, want)
	}
	if undo.attr("hx-target") != "#"+rowID(dorisID) || undo.attr("hx-swap") != "outerHTML settle:0ms" {
		t.Errorf("Undo does not swap the row it sits in:\n%s", row)
	}
}

func TestLog_TheSwapSaysWhatWasLoggedWhatIsLeftAndWhereUndoIs(t *testing.T) {
	f := rosewood(t)

	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)

	want := "You watered Doris. 2 plants due today, 1 of them overdue. Undo from the row now, or from Activity later."
	if got := announcement(rec.Body.String()); got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestLog_ASkipIsAnnouncedAsSkipped(t *testing.T) {
	f := rosewood(t)

	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}, "outcome": {"skipped"}, "again-water": {"2"}}, true)

	want := "You skipped Doris. 2 plants due today, 1 of them overdue. Undo from the row now, or from Activity later."
	if got := announcement(rec.Body.String()); got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestUndo_TheSwapSaysUndoneAndWhatIsLeft(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)

	rec := f.undo(t, dorisID, event.ID, "water", true)

	if got, want := announcement(rec.Body.String()), "Undone. 3 plants due today, 1 of them overdue."; got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}

func TestUndo_DeletesTheEventAndReturnsTheRowUnlogged(t *testing.T) {
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

	row := careRowIn(rec.Body.String(), dorisID)
	if got := row.text(); got != "Doris Bedroom Water" {
		t.Errorf("the row says %q, want the row that was there before the event", got)
	}
	if strings.Contains(row.attr("style"), "--grace") || row.first(hasAttr("hx-delete")) != nil {
		t.Errorf("the row came back still logged:\n%s", row)
	}
}

// The row on the page is returned even when the sheet logged a different care
// from the one it was opened for.
func TestUndo_ReturnsTheRowNamedInTheQuery(t *testing.T) {
	f := rosewood(t)
	f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}}, true)
	event := f.latest(t, nigelID)

	row := careRowIn(f.undo(t, nigelID, event.ID, "water", true).Body.String(), nigelID)
	if row == nil {
		t.Fatal("the swap is not the watering row")
	}
	if got := row.text(); got != "Nigel Bathroom Water" {
		t.Errorf("the row says %q, want Nigel's watering as it was", got)
	}
}

func TestUndo_AnEventTheReaderMayNotDeleteIs404(t *testing.T) {
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

func TestUndo_AnOwnerMayDeleteAnybodysEvent(t *testing.T) {
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

func TestUndo_AFormPostRedirectsToToday(t *testing.T) {
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

// dayHeadIn returns the summary line at the top of Today in a response body,
// or nil.
func dayHeadIn(body string) *element {
	return readHTML(body).byID("day-head")
}

func TestWindow_TheHeadingIsReturnedWithTheRow(t *testing.T) {
	t.Run("a day with cares outstanding shows the count", func(t *testing.T) {
		f := rosewood(t)
		head := dayHeadIn(f.post(t, dorisID.String(), url.Values{"care": {"water"}}, true).Body.String())
		if got := head.text(); got != "2 plants due today, 1 of them overdue." {
			t.Errorf("the head says %q, want the count without Doris", got)
		}
		if head.attr("hx-swap-oob") != "true" {
			t.Errorf("the head is not swapped out of band, so nothing but the row would move:\n%s", head)
		}
	})

	t.Run("a day whose rows are all logged shows the done heading rather than the empty state", func(t *testing.T) {
		f := rosewood(t)
		f.water(t, bigFellaID)
		f.water(t, nigelID)
		head := dayHeadIn(f.post(t, dorisID.String(), url.Values{"care": {"water"}}, true).Body.String())
		if got := head.text(); got != "All done for today." {
			t.Errorf("the head says %q, want the tick that waits for the windows to close", got)
		}
		if strings.Contains(head.text(), "Nothing else is due.") {
			t.Error("the empty screen was rendered over rows that are still on the page")
		}
	})

	t.Run("an undo restores the count", func(t *testing.T) {
		f := rosewood(t)
		f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
		event := f.latest(t, dorisID)
		head := dayHeadIn(f.undo(t, dorisID, event.ID, "water", true).Body.String())
		if got := head.text(); got != "3 plants due today, 1 of them overdue." {
			t.Errorf("the head says %q, want Doris counted again", got)
		}
	})
}

func TestWindow_AClosedGraceWindowRefreshesWhatTheRowsRemovalChanged(t *testing.T) {
	t.Run("only the heading while another row is still inside its window", func(t *testing.T) {
		f := rosewood(t)
		f.water(t, bigFellaID)
		f.water(t, dorisID)
		f.water(t, nigelID)

		body := f.settled(t, rowID(dorisID)).Body.String()
		if readHTML(body).byID("day") != nil {
			t.Errorf("the feed came back under rows that are still on the page:\n%s", body)
		}
		if got := text(body); got != "All done for today." {
			t.Errorf("the response says %q, want the tick", got)
		}
	})

	t.Run("the whole body once no window is open", func(t *testing.T) {
		f := rosewood(t)
		f.water(t, bigFellaID)
		f.water(t, dorisID)
		f.water(t, nigelID)
		f.exec(t, "UPDATE care_event SET recorded_at = recorded_at - interval '1 minute'")

		body := f.settled(t, rowID(dorisID)).Body.String()
		doc := readHTML(body)
		if doc.byID("day").attr("hx-swap-oob") != "true" {
			t.Errorf("the feed did not come back as an out-of-band swap:\n%.200s", body)
		}
		if !strings.Contains(doc.text(), "All done for today") || !strings.Contains(doc.text(), "Nothing else is due.") {
			t.Errorf("the empty screen is not in the feed:\n%s", doc.text())
		}
		// Watering Nigel moves his row to Coming up rather than off the page,
		// because his watering is every four days.
		for _, plant := range []uuid.UUID{bigFellaID, dorisID} {
			if doc.byID(rowID(plant)) != nil {
				t.Errorf("%s is still on the feed after its window closed", rowID(plant))
			}
		}
		if doc.first(hasAttr("hx-delete")) != nil {
			t.Errorf("a row came back logged, with its Undo:\n%s", body)
		}
		if doc.first(isTag("html")) != nil {
			t.Error("a row that has settled got the whole page back")
		}
	})
}

// A page load never shows a row inside its grace window. The window exists
// only in the page the swap rendered into.
func TestWindow_APageLoadShowsNoRowInsideAGraceWindow(t *testing.T) {
	f := rosewood(t)
	f.post(t, dorisID.String(), url.Values{"care": {"water"}}, true)

	page := readHTML(f.show(t))
	if page.byID(rowID(dorisID)) != nil {
		t.Error("Doris is still on the page after being watered")
	}
	if page.first(hasAttr("hx-delete")) != nil {
		t.Error("the page has a logged row's Undo, and a logged row exists only in the page a swap rendered into")
	}
	for _, e := range page.all(hasAttr("style")) {
		if strings.Contains(e.attr("style"), "--grace") {
			t.Errorf("an element has the grace window's style %q", e.attr("style"))
		}
	}
	if strings.Contains(page.text(), "All done for today.") {
		t.Error("the page says All done for today., the heading shown only while a logged row is on the page")
	}
}

func TestLog_TheFeedShowsTheCareJustLogged(t *testing.T) {
	f := rosewood(t)
	f.principal.Capabilities[auth.CareDeleteOwn] = true

	logged := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true).Body.String()
	if readHTML(logged).byID("activity").attr("hx-swap-oob") != "true" {
		t.Fatalf("the log's response has no feed to swap in:\n%s", logged)
	}
	line := feed(t, logged)[0]
	if got := line.text(); got != "You watered Doris · today, 9:00am Undo" {
		t.Errorf("the feed's newest line reads %q, want the watering just logged", got)
	}
	if got, want := line.first(isTag("form")).attr("action"), undoFormPath(dorisID, f.latest(t, dorisID).ID); got != want {
		t.Errorf("the line's Undo posts to %q, want %q", got, want)
	}
}

func TestUndo_TheFeedNoLongerShowsTheDeletedCare(t *testing.T) {
	f := rosewood(t)
	f.principal.Capabilities[auth.CareDeleteOwn] = true
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)

	undone := f.undo(t, dorisID, event.ID, "water", true).Body.String()
	if readHTML(undone).first(attrIs("action", undoFormPath(dorisID, event.ID))) != nil {
		t.Errorf("the feed still has the watering that was taken back:\n%s", undone)
	}
}

func TestSheet_TheHeadingShowsThePlantsPictureAsItsSquare(t *testing.T) {
	f := rosewood(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	body := readHTML(f.sheet(t, sheetPath(bigFellaID, "water"), true).Body.String())

	if got, want := imageSources(body), []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the sheet's images are %v, want the square at %v", got, want)
	}
}

func TestLog_TheLoggedRowStillShowsThePlantsPicture(t *testing.T) {
	f := rosewood(t)
	photoID := givePicture(t, f.tx, dorisID)

	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}, "when": {"now"}}, true)

	row := careRowIn(rec.Body.String(), dorisID)
	if got, want := imageSources(row), []string{photoSquarePath(dorisID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the logged row's images are %v, want the square at %v", got, want)
	}
}

// notified is one call the handler made to its notify hook.
type notified struct {
	gardenID, actorID uuid.UUID
	n                 push.Notification
}

// captureNotifications replaces the fixture's notify hook with one that
// records each call in the returned slice.
func (f *todayFixture) captureNotifications() *[]notified {
	var got []notified
	f.handler.notify = func(_ context.Context, gardenID, actorID uuid.UUID, n push.Notification) {
		got = append(got, notified{gardenID: gardenID, actorID: actorID, n: n})
	}
	return &got
}

func TestLog_TheNotificationSaysWhoWateredWhichPlantAndOpensThatPlant(t *testing.T) {
	f := rosewood(t)
	got := f.captureNotifications()

	rec := f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	want := []notified{{gardenID: rosewoodID, actorID: readerID, n: push.Notification{Title: "Rosewood", Body: "Ellie watered Doris.", URL: "/plants/" + dorisID.String()}}}
	if !slices.Equal(*got, want) {
		t.Errorf("notified %+v, want %+v", *got, want)
	}
}

func TestLog_ASkipIsNotifiedAsSkipped(t *testing.T) {
	f := rosewood(t)
	got := f.captureNotifications()

	f.post(t, nigelID.String(), url.Values{"row": {"water"}, "care": {"feed"}, "outcome": {"skipped"}, "again-feed": {"3"}}, true)

	if len(*got) != 1 || (*got)[0].n.Body != "Ellie skipped feeding Nigel." {
		t.Errorf("notified %+v, want one notification saying Ellie skipped feeding Nigel.", *got)
	}
}

func TestLog_TheNotificationsIconIsThePlantsProfilePicture(t *testing.T) {
	f := rosewood(t)
	var photoID uuid.UUID
	err := f.tx.QueryRow(t.Context(), `INSERT INTO photo (garden_id, plant_id, uploaded_by, kind, path, width, height, bytes, square_bytes)
		VALUES ($1, $2, $3, 'image/jpeg', 'x', 1, 1, 1024, 512) RETURNING id`, rosewoodID, dorisID, readerID).Scan(&photoID)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, "UPDATE plant SET profile_photo_id = $1 WHERE id = $2", photoID, dorisID)
	got := f.captureNotifications()

	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)

	want := "/plants/" + dorisID.String() + "/photos/" + photoID.String() + "/square"
	if len(*got) != 1 || (*got)[0].n.Icon != want {
		t.Errorf("notified %+v, want one notification with icon %s", *got, want)
	}
}

func TestLog_ARefusedTimeNotifiesNobody(t *testing.T) {
	f := rosewood(t)
	got := f.captureNotifications()

	rec := f.post(t, dorisID.String(), url.Values{"care": {"water"}, "when": {"today"}, "time": {"23:00"}}, true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	if len(*got) != 0 {
		t.Errorf("notified %+v, want nothing: no care was recorded", *got)
	}
}

func TestLog_OnThePlantPageNotifiesTheGarden(t *testing.T) {
	f := rosewood(t)
	got := f.captureNotifications()

	rec := f.post(t, dorisID.String(), url.Values{"over": {overPlant}, "care": {"water"}}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	want := []notified{{gardenID: rosewoodID, actorID: readerID, n: push.Notification{Title: "Rosewood", Body: "Ellie watered Doris.", URL: "/plants/" + dorisID.String()}}}
	if !slices.Equal(*got, want) {
		t.Errorf("notified %+v, want %+v", *got, want)
	}
}

func TestUndo_NotifiesNobody(t *testing.T) {
	f := rosewood(t)
	got := f.captureNotifications()
	f.post(t, dorisID.String(), url.Values{"row": {"water"}, "care": {"water"}}, true)
	event := f.latest(t, dorisID)

	f.undo(t, dorisID, event.ID, "water", true)

	if len(*got) != 1 {
		t.Errorf("notified %d times, want once for the log and nothing for the undo", len(*got))
	}
}
