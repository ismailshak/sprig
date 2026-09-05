package http

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

type plantFixture struct {
	*todayFixture
	handler *plants
}

// rosewoodPlant sets up a plant's page on the garden Today and Plants are
// tested on. The embedded fixture provides the sheet and its post, since the
// sheet this page opens is the same one Today opens.
func rosewoodPlant(t *testing.T) *plantFixture {
	t.Helper()

	f := rosewood(t)
	return &plantFixture{
		todayFixture: f,
		handler: &plants{
			logger:    testLogger,
			queries:   store.New(f.tx),
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
	}
}

func (f *plantFixture) page(t *testing.T, plantID uuid.UUID) string {
	t.Helper()

	rec := f.request(t, plantID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *plantFixture) request(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, plantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.plant(rec, req)
	return rec
}

// The garden Today is tested on has one person and two care types. A second
// person and a third care type are added here because Recent names who did
// each care and the sheet offers a care no plant is scheduled for.
var (
	raviID  = uuid.MustParse("00000000-0000-7000-8000-000000000105")
	repotID = uuid.MustParse("00000000-0000-7000-8000-000000000106")
)

var (
	heroName    = regexp.MustCompile(`<h1 class="hero__name([^"]*)">(.*?)</h1>`)
	heroMeta    = regexp.MustCompile(`<p class="hero__meta">(.*?)</p>`)
	heroWhere   = regexp.MustCompile(`<p class="hero__where">(.*?)</p>`)
	schedRow    = regexp.MustCompile(`(?s)<li class="sched(?: sched--add)?" id="[^"]*">(.*?)</li>`)
	schedType   = regexp.MustCompile(`<span class="sched__type">(.*?)</span>`)
	schedEvery  = regexp.MustCompile(`<span class="sched__every">(.*?)</span>`)
	schedWhen   = regexp.MustCompile(`<span class="sched__when([^"]*)">(.*?)</span>`)
	factPair    = regexp.MustCompile(`<dt>(.*?)</dt><dd>(.*?)</dd>`)
	panelNote   = regexp.MustCompile(`<p class="panel__note">(.*?)</p>`)
	panelFact   = regexp.MustCompile(`<p class="panel__fact"><span>Acquired</span>(.*?)</p>`)
	logLine     = regexp.MustCompile(`<li class="logline[^"]*">(.*?)</li>`)
	logCare     = regexp.MustCompile(`<a [^>]*>Log care</a>`)
	sheetForm   = regexp.MustCompile(`(?s)<form class="sheet__form".*?>`)
	sheetChips  = regexp.MustCompile(`<button class="chip" name="care" value="([^"]+)"`)
	sheetPlantH = regexp.MustCompile(`<(a|div) class="sheet__plant"`)
)

type testScheduleRow struct {
	care string
	rule string
	when string
	late bool
	off  bool
}

// scheduleOf returns the closed rows of the Schedule section. A row open as
// the editor has controls rather than a rule and due date, so it is left out.
func scheduleOf(t *testing.T, page string) []testScheduleRow {
	t.Helper()

	var rows []testScheduleRow
	for _, m := range schedRow.FindAllStringSubmatch(page, -1) {
		care := schedType.FindStringSubmatch(m[1])
		when := schedWhen.FindStringSubmatch(m[1])
		if care == nil || when == nil {
			t.Fatalf("a schedule row has no care or due date:\n%s", m[1])
		}
		row := testScheduleRow{
			care: text(care[1]),
			when: text(when[2]),
			late: strings.Contains(when[1], "sched__when--late"),
			off:  strings.Contains(when[1], "sched__when--off"),
		}
		if every := schedEvery.FindStringSubmatch(m[1]); every != nil {
			row.rule = text(every[1])
		}
		rows = append(rows, row)
	}
	return rows
}

func rowFor(t *testing.T, page, care string) testScheduleRow {
	t.Helper()

	for _, row := range scheduleOf(t, page) {
		if row.care == care {
			return row
		}
	}
	t.Fatalf("the schedule has no %s row:\n%v", care, scheduleOf(t, page))
	return testScheduleRow{}
}

func recentOf(page string) []string {
	var lines []string
	for _, m := range logLine.FindAllStringSubmatch(page, -1) {
		lines = append(lines, text(m[1]))
	}
	return lines
}

func TestPlant_TheHeadingIsTheNicknameWithTheOtherNamesBelow(t *testing.T) {
	page := rosewoodPlant(t).page(t, bigFellaID)

	name := heroName.FindStringSubmatch(page)
	if name == nil || text(name[2]) != "Big Fella" {
		t.Fatalf("the heading is not Big Fella:\n%v", name)
	}
	if strings.Contains(name[1], "hero__name--sp") {
		t.Error("a nickname was set in italic")
	}
	meta := heroMeta.FindStringSubmatch(page)
	if meta == nil || text(meta[1]) != "Swiss cheese plant · Monstera deliciosa" {
		t.Errorf("the other names read %v, want Swiss cheese plant · Monstera deliciosa", meta)
	}
	if !strings.Contains(meta[1], "<i>Monstera deliciosa</i>") {
		t.Errorf("the botanical name is not italic:\n%s", meta[1])
	}
	if where := heroWhere.FindStringSubmatch(page); where == nil || text(where[1]) != "Living room" {
		t.Errorf("the room reads %v, want Living room", where)
	}
}

// Opuntia microdasys has only a botanical name.
func TestPlant_ABotanicalOnlyNameIsTheHeadingInItalics(t *testing.T) {
	page := rosewoodPlant(t).page(t, opuntiaID)

	name := heroName.FindStringSubmatch(page)
	if name == nil || text(name[2]) != "Opuntia microdasys" {
		t.Fatalf("the heading is not Opuntia microdasys:\n%v", name)
	}
	if !strings.Contains(name[1], "hero__name--sp") {
		t.Error("the botanical heading is not italic")
	}
	if heroMeta.MatchString(page) {
		t.Error("a plant with one name drew a second line for the names it does not have")
	}
}

// Sprout has a nickname and nothing else: no second name, no room, no
// reference fields.
func TestPlant_APlantWithOneNameAndNoReferenceHasNoEmptySections(t *testing.T) {
	page := rosewoodPlant(t).page(t, sproutID)

	if name := heroName.FindStringSubmatch(page); name == nil || text(name[2]) != "Sprout" {
		t.Fatalf("the heading is not Sprout:\n%v", name)
	}
	if heroMeta.MatchString(page) {
		t.Error("the page drew a line for the names Sprout does not have")
	}
	if heroWhere.MatchString(page) {
		t.Error("the page drew a room for a plant that has none")
	}
	if strings.Contains(page, "Reference") {
		t.Errorf("a plant with nothing written down drew a Reference section:\n%s", text(page))
	}
}

func TestPlant_ReferenceShowsOnlySetFieldsInAFixedOrder(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, `UPDATE plant SET sun = 'Bright indirect.', feed_needs = 'Half strength.', pot = '30cm terracotta.',
		notes = 'Wipe the leaves.', acquired_year = 2024, acquired_month = 3 WHERE id = $1`, bigFellaID)

	page := f.page(t, bigFellaID)
	var facts [][2]string
	for _, m := range factPair.FindAllStringSubmatch(page, -1) {
		facts = append(facts, [2]string{text(m[1]), text(m[2])})
	}
	want := [][2]string{
		{"Sun", "Bright indirect."},
		{"Feed", "Half strength."},
		{"Pot", "30cm terracotta."},
	}
	if !slices.Equal(facts, want) {
		t.Errorf("the reference reads %v, want %v", facts, want)
	}
	if note := panelNote.FindStringSubmatch(page); note == nil || text(note[1]) != "Wipe the leaves." {
		t.Errorf("the note reads %v, want Wipe the leaves.", note)
	}
	if acquired := panelFact.FindStringSubmatch(page); acquired == nil || text(acquired[1]) != "March 2024" {
		t.Errorf("acquired reads %v, want March 2024", acquired)
	}
}

func TestPlant_AnAcquiredYearWithNoMonthShowsTheYearAlone(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET acquired_year = 2019 WHERE id = $1", bigFellaID)

	if acquired := panelFact.FindStringSubmatch(f.page(t, bigFellaID)); acquired == nil || text(acquired[1]) != "2019" {
		t.Errorf("acquired reads %v, want 2019", acquired)
	}
}

func TestPlant_EachScheduleShapeShowsItsRuleAndDueDate(t *testing.T) {
	cases := []struct {
		name string
		// insert is the rest of the INSERT that gives Big Fella a feeding
		// schedule.
		insert string
		args   []any
		want   testScheduleRow
	}{
		{
			name:   "an overdue interval schedule shows how many days late",
			insert: "interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 10, 'day', $4",
			args:   []any{day(time.August, 14)},
			want:   testScheduleRow{care: "Feed", rule: "Every 10 days", when: "10 days late", late: true},
		},
		{
			name:   "an in-season schedule shows its months and days until due",
			insert: "interval_count, interval_unit, season_start_month, season_end_month, set_at) VALUES ($1, $2, $3, 3, 'week', 3, 9, $4",
			args:   []any{day(time.September, 1)},
			want:   testScheduleRow{care: "Feed", rule: "Every 3 weeks · Mar–Sep", when: "Due in 19 days"},
		},
		{
			name:   "an out-of-season schedule says so with no count",
			insert: "interval_count, interval_unit, season_start_month, season_end_month, set_at) VALUES ($1, $2, $3, 3, 'week', 11, 2, $4",
			args:   []any{day(time.August, 1)},
			want:   testScheduleRow{care: "Feed", rule: "Every 3 weeks · Nov–Feb", when: "Out of season", off: true},
		},
		{
			name:   "a fixed date schedule shows its date rather than a count",
			insert: "interval_count, interval_unit, anchor_date, anchor_precision, set_at) VALUES ($1, $2, $3, 1, 'year', '2027-05-01', 'day', $4",
			args:   []any{day(time.August, 1)},
			want:   testScheduleRow{care: "Feed", rule: "Every year", when: "Due 1 May 2027"},
		},
		{
			name:   "a one-off reads Just once and names its month",
			insert: "anchor_date, anchor_precision, set_at) VALUES ($1, $2, $3, '2028-03-01', 'month', $4",
			args:   []any{day(time.August, 1)},
			want:   testScheduleRow{care: "Feed", rule: "Just once", when: "Due in March 2028"},
		},
		{
			// A month-only date has no day to be late against, so it is
			// overdue only once the month has passed.
			name:   "a passed month-only date reads overdue since that month",
			insert: "anchor_date, anchor_precision, set_at) VALUES ($1, $2, $3, '2026-03-01', 'month', $4",
			args:   []any{day(time.January, 5)},
			want:   testScheduleRow{care: "Feed", rule: "Just once", when: "Overdue since March", late: true},
		},
		{
			name:   "a cadence due on the reader's own day says so",
			insert: "interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 7, 'day', $4",
			args:   []any{day(time.August, 27)},
			want:   testScheduleRow{care: "Feed", rule: "Every 7 days", when: "Due today"},
		},
		{
			name:   "a cadence inside the coming week names the weekday",
			insert: "interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 1, 'week', $4",
			args:   []any{day(time.August, 30)},
			want:   testScheduleRow{care: "Feed", rule: "Every week", when: "Due Sunday"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewoodPlant(t)
			args := append([]any{rosewoodID, bigFellaID, feedID}, c.args...)
			f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, "+c.insert+")", args...)

			if got := rowFor(t, f.page(t, bigFellaID), "Feed"); got != c.want {
				t.Errorf("the feeding row reads %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestPlant_ACompletedOneOffShowsAsNotScheduled(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, anchor_date, anchor_precision, set_at) VALUES ($1, $2, $3, '2026-08-20', 'day', $4)",
		rosewoodID, bigFellaID, feedID, day(time.August, 1))
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, bigFellaID, feedID, readerID, day(time.August, 21))

	feed := rowFor(t, f.page(t, bigFellaID), "Feed")

	if feed.rule != "" || feed.when != "Not scheduled" {
		t.Errorf("the feeding row reads %+v, want a care the plant is not scheduled for", feed)
	}
}

func TestPlant_RecentNamesThePersonAndTheActionAndNotThePlant(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ravi', 'ravi', 'Europe/London')", raviID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'member')", rosewoodID, raviID)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, override_interval_days) VALUES ($1, $2, $3, $4, $5, $5, false, 2)",
		rosewoodID, bigFellaID, waterID, raviID, day(time.September, 2))

	want := []string{"Ravi skipped · yesterday", "You watered · 22 Aug"}
	if got := recentOf(f.page(t, bigFellaID)); !slices.Equal(got, want) {
		t.Errorf("Recent reads %v, want %v", got, want)
	}
}

// Recent is ordered by when the care happened, not when it was recorded, so a
// care logged a week late appears where it happened.
func TestPlant_RecentIsOrderedByWhenTheCareHappened(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, bigFellaID, waterID, readerID, day(time.September, 2))
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $6, true)",
		rosewoodID, bigFellaID, waterID, readerID, day(time.August, 25), thursday)

	want := []string{"You watered · yesterday", "You watered · 25 Aug", "You watered · 22 Aug"}
	if got := recentOf(f.page(t, bigFellaID)); !slices.Equal(got, want) {
		t.Errorf("Recent reads %v, want %v", got, want)
	}
}

func TestPlant_RecentStopsAtFourEvents(t *testing.T) {
	f := rosewoodPlant(t)
	for d := 1; d <= 6; d++ {
		f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
			rosewoodID, bigFellaID, waterID, readerID, day(time.August, 10+d))
	}

	if got := recentOf(f.page(t, bigFellaID)); len(got) != recentLength {
		t.Errorf("Recent draws %d lines, want %d: %v", len(got), recentLength, got)
	}
}

// allActivity is the text and href of the link under Recent, the only link of
// its kind on a plant's page.
func allActivity(page string) (label, href string) {
	m := footLink.FindStringSubmatch(page)
	if m == nil {
		return "", ""
	}
	return text(m[2]), html.UnescapeString(m[1])
}

func TestPlant_APlantWithCareEventsLinksToItsOwnActivityLog(t *testing.T) {
	f := rosewoodPlant(t)

	label, href := allActivity(f.page(t, bigFellaID))

	if want := "All activity for Big Fella"; label != want {
		t.Errorf("the link under Recent reads %q, want %q", label, want)
	}
	if want := plantActivityPath(bigFellaID); href != want {
		t.Errorf("the link under Recent points at %q, want %q", href, want)
	}
}

func TestPlant_APlantWithNoCareEventsHasNoActivityLink(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "DELETE FROM care_event WHERE plant_id = $1", sproutID)

	if label, _ := allActivity(f.page(t, sproutID)); label != "" {
		t.Errorf("Recent has a %q link, but the plant has no care events", label)
	}
}

func TestPlant_APlantWithNoEventsSaysNothingRecorded(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "DELETE FROM care_event WHERE plant_id = $1", sproutID)

	if got := recentOf(f.page(t, sproutID)); !slices.Equal(got, []string{"Nothing recorded yet."}) {
		t.Errorf("Recent reads %v, want Nothing recorded yet.", got)
	}
}

func TestPlant_AReaderWhoMayNotLogCareSeesNoLogButton(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{}

	if page := f.page(t, bigFellaID); strings.Contains(page, "Log care") {
		t.Errorf("a reader without care.log was offered Log care:\n%s", text(page))
	}
}

func TestPlant_LogCareOpensTheSheetOnThePlantsPage(t *testing.T) {
	page := rosewoodPlant(t).page(t, bigFellaID)

	button := logCare.FindString(page)
	if button == "" {
		t.Fatalf("the page has no Log care:\n%s", text(page))
	}
	for _, attr := range []string{`href="` + plantSheetPath(bigFellaID) + `"`, `hx-get="` + plantSheetPath(bigFellaID) + `"`, `hx-target="#sheet"`} {
		if !strings.Contains(button, attr) {
			t.Errorf("Log care lacks %s:\n%s", attr, button)
		}
	}
	if !strings.Contains(page, `<div id="sheet"></div>`) {
		t.Error("the page has no slot for the sheet to land in")
	}
}

// An archived plant keeps its page. Keeping the history is why plants are
// archived rather than deleted.
func TestPlant_AnArchivedPlantHasAPageButNoLogButton(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	page := f.page(t, bigFellaID)
	if name := heroName.FindStringSubmatch(page); name == nil || text(name[2]) != "Big Fella" {
		t.Fatalf("the heading is not Big Fella:\n%v", name)
	}
	if got := recentOf(page); !slices.Equal(got, []string{"You watered · 22 Aug"}) {
		t.Errorf("Recent reads %v, want the plant's history", got)
	}
	if logCare.MatchString(page) {
		t.Errorf("an archived plant was offered Log care:\n%s", text(page))
	}
}

func TestPlantSheet_Is404ForAnArchivedPlant(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if rec := f.sheet(t, plantSheetPath(bigFellaID), false); rec.Code != http.StatusNotFound {
		t.Errorf("the sheet answered %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPlantSheet_RecordsNothingAgainstAnArchivedPlant(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	rec := f.post(t, bigFellaID.String(), url.Values{"over": {overPlant}, "care": {"water"}, "when": {whenNow}}, false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("the post answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	if n := len(f.events(t, bigFellaID)); n != 1 {
		t.Errorf("the plant holds %d events, want the seeded one alone", n)
	}
}

// A repot can be logged even though the plant has no repot schedule.
func TestPlantSheet_OffersEveryCareTypeTheGardenHas(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_type (garden_id, name, slug) VALUES ($1, 'Repot', 'repot')", rosewoodID)

	rec := f.sheet(t, plantSheetPath(bigFellaID), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}
	var chips []string
	for _, m := range sheetChips.FindAllStringSubmatch(rec.Body.String(), -1) {
		chips = append(chips, m[1])
	}
	if want := []string{"water", "feed", "repot"}; !slices.Equal(chips, want) {
		t.Errorf("the sheet offers %v, want %v", chips, want)
	}
}

func TestPlantSheet_ThePlantHeadingIsNotALink(t *testing.T) {
	f := rosewoodPlant(t)

	body := f.sheet(t, plantSheetPath(bigFellaID), false).Body.String()
	if head := sheetPlantH.FindStringSubmatch(body); head == nil || head[1] != "div" {
		t.Errorf("the sheet's heading is %v, want a plain block", head)
	}
	if strings.Contains(body, `<a class="sheet__plant"`) {
		t.Error("the sheet's heading leads to the page it is on")
	}
}

// The sheet on a plant's page has no row to swap, so its form is an ordinary
// post and the flow is the same with and without JavaScript.
func TestPlantSheet_TheFormPostsWithoutAnHTMXSwap(t *testing.T) {
	f := rosewoodPlant(t)

	form := sheetForm.FindString(f.sheet(t, plantSheetPath(bigFellaID), true).Body.String())
	if form == "" {
		t.Fatal("the sheet has no form")
	}
	if strings.Contains(form, "hx-post") {
		t.Errorf("the form swaps a row this page does not have:\n%s", form)
	}
	if !strings.Contains(form, `action="`+logPath(bigFellaID)+`"`) {
		t.Errorf("the form does not post to the plant's log:\n%s", form)
	}
}

// Big Fella is two days late for water. The feeding inserted here is due in
// 19 days.
func TestPlantSheet_OpensOnTheCareDueSoonest(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 3, 'week', $4)",
		rosewoodID, bigFellaID, feedID, day(time.September, 1))

	body := f.sheet(t, plantSheetPath(bigFellaID), false).Body.String()
	if !strings.Contains(body, `<button class="sheet__default" name="care" value="water"`) {
		t.Errorf("the sheet did not open on the watering:\n%s", body)
	}
}

func TestPlantSheet_RecordsTheCareAndRedirectsToThePlant(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.post(t, bigFellaID.String(), url.Values{"over": {overPlant}, "care": {"water"}, "when": {whenNow}}, false)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != plantPath(bigFellaID) {
		t.Fatalf("the post answered %d to %q, want %d to the plant", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	events := f.events(t, bigFellaID)
	if len(events) != 2 {
		t.Fatalf("the plant holds %d events, want the seeded one and the new one", len(events))
	}
	if latest := events[len(events)-1]; latest.CareTypeID != waterID || !latest.Done {
		t.Errorf("the event recorded is %+v, want a watering that was done", latest)
	}
	// The watering resets the interval, so the row that read two days late
	// now counts from today.
	if got := rowFor(t, f.page(t, bigFellaID), "Water"); got.when != "Due in 10 days" || got.late {
		t.Errorf("the schedule row reads %+v, want the watering ten days out", got)
	}
}

// The sheet lists every care type in the garden, so the post has to accept a
// care the plant is not scheduled for.
func TestPlantSheet_RecordsACareThePlantIsNotScheduledFor(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Repot', 'repot')", repotID, rosewoodID)

	rec := f.post(t, bigFellaID.String(), url.Values{"over": {overPlant}, "care": {"repot"}, "when": {whenNow}}, false)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	events := f.events(t, bigFellaID)
	if latest := events[len(events)-1]; latest.CareTypeID != repotID {
		t.Errorf("the event recorded is %+v, want a repot", latest)
	}
	if got := recentOf(f.page(t, bigFellaID))[0]; got != "You repotted · today" {
		t.Errorf("Recent reads %q, want You repotted · today", got)
	}
}

func TestPlantSheet_ARefusedTimeReRendersThePlantsPage(t *testing.T) {
	f := rosewoodPlant(t)

	rec := f.post(t, bigFellaID.String(), url.Values{"over": {overPlant}, "care": {"water"}, "when": {whenOther}, "at": {"2026-09-04T09:00"}}, false)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "That is later than now.") {
		t.Errorf("the refusal says nothing:\n%s", text(body))
	}
	if name := heroName.FindStringSubmatch(body); name == nil || text(name[2]) != "Big Fella" {
		t.Errorf("the refusal did not come back on the plant's page:\n%s", text(body))
	}
	if len(f.events(t, bigFellaID)) != 1 {
		t.Error("the refused post wrote an event")
	}
}

// A plant Rosewood does not have is 404 whether it exists in another garden or
// nowhere. The sheet for it is 404 too.
func TestPlant_APlantTheGardenDoesNotHaveIs404(t *testing.T) {
	f := rosewoodPlant(t)
	stranger := uuid.MustParse("00000000-0000-7000-8000-0000000009ff")

	if rec := f.request(t, stranger); rec.Code != http.StatusNotFound {
		t.Errorf("the page answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := f.sheet(t, plantSheetPath(stranger), false); rec.Code != http.StatusNotFound {
		t.Errorf("the sheet answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := f.post(t, stranger.String(), url.Values{"over": {overPlant}, "care": {"water"}}, false); rec.Code != http.StatusNotFound {
		t.Errorf("the post answered %d, want %d", rec.Code, http.StatusNotFound)
	}
}
