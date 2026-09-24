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

	"github.com/jackc/pgx/v5"

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
			photos:    testPhotos(t),
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

// plantHeading returns the text of the page's h1, the plant's name.
func plantHeading(page string) string {
	return readHTML(page).first(isTag("h1")).text()
}

// hero returns the text at the top of a plant's page, above the first
// section's heading: the plant's name, its other names, its room and the day it
// was archived.
func hero(page string) string {
	body := readHTML(page).byID(plantBodyID)
	all := body.text()
	heading := body.first(isTag("h2")).text()
	if heading == "" {
		return all
	}
	top, _, _ := strings.Cut(all, heading)
	return strings.TrimSpace(top)
}

// pointsAt reports whether a link, a form or a button in doc goes to url, with
// or without htmx.
func pointsAt(doc *element, url string) bool {
	for _, name := range []string{"href", "action", "formaction", "hx-get", "hx-post"} {
		if doc.first(attrIs(name, url)) != nil {
			return true
		}
	}
	return false
}

// announcedIn returns the sentence in a swap's response that htmx puts into the
// live region, or "" when the response has none.
func announcedIn(body string) string {
	return readHTML(body).first(attrIs("hx-swap-oob", "innerHTML:#status")).text()
}

// idStartsWith matches an element whose id starts with prefix.
func idStartsWith(prefix string) func(*element) bool {
	return func(e *element) bool { return strings.HasPrefix(e.attr("id"), prefix) }
}

// elementTexts returns the text each element below e holds directly, in
// document order, with whitespace collapsed and empty runs left out. Text a
// template puts in separate elements, such as a care type and its rule, is
// returned as separate strings.
func elementTexts(e *element) []string {
	var runs []string
	for _, below := range e.all() {
		var b strings.Builder
		for _, child := range below.children {
			if s, ok := child.(string); ok {
				b.WriteString(s)
			}
		}
		if run := strings.Join(strings.Fields(b.String()), " "); run != "" {
			runs = append(runs, run)
		}
	}
	return runs
}

type testScheduleRow struct {
	care string
	rule string
	when string
}

// scheduleOf returns the closed schedule rows, found by their id prefix. A row
// open for editing holds a select and is left out.
func scheduleOf(t *testing.T, page string) []testScheduleRow {
	t.Helper()

	var rows []testScheduleRow
	for _, li := range readHTML(page).all(isTag("li"), idStartsWith(scheduleRowPrefix)) {
		if li.first(isTag("select")) != nil {
			continue
		}
		// A closed row reads the care type, the rule when there is one, and
		// when the care is due.
		runs := elementTexts(li)
		if len(runs) != 2 && len(runs) != 3 {
			t.Fatalf("a schedule row reads %q, want the care type, a rule and a due date", runs)
		}
		row := testScheduleRow{care: runs[0], when: runs[len(runs)-1]}
		if len(runs) == 3 {
			row.rule = runs[1]
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

// recentOf returns the lines of the Recent activity section.
func recentOf(page string) []string {
	var lines []string
	for _, li := range readHTML(page).first(attrIs("aria-labelledby", "recent")).all(isTag("li")) {
		lines = append(lines, li.text())
	}
	return lines
}

// details returns the Details section of a plant's page, or nil.
func details(page string) *element {
	return readHTML(page).first(attrIs("aria-labelledby", "details"))
}

func TestPlant_TheHeadingIsTheNicknameWithTheOtherNamesBelow(t *testing.T) {
	page := rosewoodPlant(t).page(t, bigFellaID)

	if got := plantHeading(page); got != "Big Fella" {
		t.Fatalf("the heading reads %q, want Big Fella", got)
	}
	if got, want := hero(page), "Big Fella Swiss cheese plant · Monstera deliciosa Living room"; got != want {
		t.Errorf("the top of the page reads %q, want %q", got, want)
	}
}

// Opuntia microdasys has only a botanical name.
func TestPlant_ABotanicalOnlyNameIsTheHeadingWithNoLineOfOtherNames(t *testing.T) {
	page := rosewoodPlant(t).page(t, opuntiaID)

	if got := plantHeading(page); got != "Opuntia microdasys" {
		t.Fatalf("the heading reads %q, want Opuntia microdasys", got)
	}
	if got, want := hero(page), "Opuntia microdasys Windowsill"; got != want {
		t.Errorf("the top of the page reads %q, want %q, with no line for the names the plant does not have", got, want)
	}
}

// Sprout has a nickname and nothing else: no second name, no room, no
// detail fields.
func TestPlant_APlantWithOneNameAndNoDetailsHasNoEmptySections(t *testing.T) {
	page := rosewoodPlant(t).page(t, sproutID)

	if got := hero(page); got != "Sprout" {
		t.Fatalf("the top of the page reads %q, want the name Sprout with no other names and no room", got)
	}
	if details(page) != nil {
		t.Errorf("a plant with nothing written down has a Details section:\n%s", text(page))
	}
}

func TestPlant_DetailsShowsOnlySetFieldsInAFixedOrder(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, `UPDATE plant SET sun = 'Bright indirect.', feed_needs = 'Half strength.', pot = '30cm terracotta.',
		notes = 'Wipe the leaves.', acquired_year = 2024, acquired_month = 3 WHERE id = $1`, bigFellaID)

	section := details(f.page(t, bigFellaID))

	var facts [][2]string
	terms, definitions := section.all(isTag("dt")), section.all(isTag("dd"))
	for i := range min(len(terms), len(definitions)) {
		facts = append(facts, [2]string{terms[i].text(), definitions[i].text()})
	}
	want := [][2]string{
		{"Sun", "Bright indirect."},
		{"Feed", "Half strength."},
		{"Pot", "30cm terracotta."},
	}
	if !slices.Equal(facts, want) {
		t.Errorf("the details read %v, want %v", facts, want)
	}
	if got, want := section.text(), "Details Sun Bright indirect. Feed Half strength. Pot 30cm terracotta. Wipe the leaves. Acquired March 2024"; got != want {
		t.Errorf("the Details section reads %q, want %q", got, want)
	}
}

func TestPlant_AnAcquiredYearWithNoMonthShowsTheYearAlone(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET acquired_year = 2019 WHERE id = $1", bigFellaID)

	if got := details(f.page(t, bigFellaID)).text(); got != "Details Acquired 2019" {
		t.Errorf("the Details section reads %q, want Details Acquired 2019", got)
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
			want:   testScheduleRow{care: "Feed", rule: "Every 10 days", when: "10 days late"},
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
			want:   testScheduleRow{care: "Feed", rule: "Every 3 weeks · Nov–Feb", when: "Out of season"},
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
			want:   testScheduleRow{care: "Feed", rule: "Just once", when: "Overdue since March"},
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
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'member', 8)", rosewoodID, raviID)
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

// allActivity is the text and href of the link in the Recent activity section,
// empty when the section has no link.
func allActivity(page string) (label, href string) {
	link := readHTML(page).first(attrIs("aria-labelledby", "recent")).first(isTag("a"))
	return link.text(), link.attr("href")
}

func TestPlant_APlantWithCareEventsLinksToItsOwnActivityLog(t *testing.T) {
	f := rosewoodPlant(t)

	label, href := allActivity(f.page(t, bigFellaID))

	if want := "All activity"; label != want {
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

func TestPlant_APlantWithNoEventsSaysNoActivityYet(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "DELETE FROM care_event WHERE plant_id = $1", sproutID)

	if got := recentOf(f.page(t, sproutID)); !slices.Equal(got, []string{"No activity yet."}) {
		t.Errorf("Recent reads %v, want No activity yet.", got)
	}
}

func TestPlant_AReaderWhoMayNotLogCareSeesNoLogButton(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{}

	if page := f.page(t, bigFellaID); strings.Contains(text(page), "Log care") {
		t.Errorf("a reader without care.log was offered Log care:\n%s", text(page))
	}
}

func TestPlant_LogCareOpensTheSheetOnThePlantsPage(t *testing.T) {
	page := rosewoodPlant(t).page(t, bigFellaID)

	doc := readHTML(page)
	button := doc.first(isTag("a"), textIs("Log care"))
	if button == nil {
		t.Fatalf("the page has no Log care:\n%s", text(page))
	}
	for name, want := range map[string]string{"href": plantSheetPath(bigFellaID), "hx-get": plantSheetPath(bigFellaID), "hx-target": "#sheet"} {
		if got := button.attr(name); got != want {
			t.Errorf("Log care's %s is %q, want %q", name, got, want)
		}
	}
	if slot := doc.byID("sheet"); slot == nil || slot.text() != "" {
		t.Errorf("the page has no empty element with the id sheet for the sheet to replace, got %s", slot)
	}
}

// An archived plant keeps its page. Keeping the history is why plants are
// archived rather than deleted.
func TestPlant_AnArchivedPlantHasAPageButNoLogButton(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	page := f.page(t, bigFellaID)
	if got := plantHeading(page); got != "Big Fella" {
		t.Fatalf("the heading reads %q, want Big Fella", got)
	}
	if got := recentOf(page); !slices.Equal(got, []string{"You watered · 22 Aug"}) {
		t.Errorf("Recent reads %v, want the plant's history", got)
	}
	if readHTML(page).first(isTag("a"), textIs("Log care")) != nil {
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
	for _, radio := range readHTML(rec.Body.String()).all(isTag("input"), attrIs("type", "radio"), attrIs("name", "care")) {
		chips = append(chips, radio.attr("value"))
	}
	if want := []string{"water", "feed", "repot"}; !slices.Equal(chips, want) {
		t.Errorf("the sheet offers %v, want %v", chips, want)
	}
}

func TestPlantSheet_ThePlantHeadingIsNotALink(t *testing.T) {
	f := rosewoodPlant(t)

	body := f.sheet(t, plantSheetPath(bigFellaID), false).Body.String()

	sheet := readHTML(body).byID("sheet")
	if !strings.Contains(sheet.text(), "Big Fella") {
		t.Fatalf("the sheet does not name the plant:\n%s", body)
	}
	if sheet.first(isTag("a"), attrIs("href", plantPath(bigFellaID))) != nil {
		t.Error("the sheet's heading links to the page it is on")
	}
}

// The sheet on a plant's page has no row to swap, so its form is an ordinary
// post and the flow is the same with and without JavaScript.
func TestPlantSheet_TheFormPostsWithoutAnHTMXSwap(t *testing.T) {
	f := rosewoodPlant(t)

	body := f.sheet(t, plantSheetPath(bigFellaID), true).Body.String()

	form := readHTML(body).first(isTag("form"), attrIs("action", logPath(bigFellaID)))
	if form == nil {
		t.Fatalf("the sheet has no form posting to the plant's log:\n%s", body)
	}
	if form.has("hx-post") {
		t.Errorf("the form swaps a row this page does not have:\n%s", form)
	}
}

// Big Fella is two days late for water. The feeding inserted here is due in
// 19 days.
func TestPlantSheet_OpensOnTheCareDueSoonest(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 3, 'week', $4)",
		rosewoodID, bigFellaID, feedID, day(time.September, 1))

	body := f.sheet(t, plantSheetPath(bigFellaID), false).Body.String()
	if !readHTML(body).first(isTag("input"), attrIs("name", "care"), attrIs("value", "water")).has("checked") {
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
	if got := rowFor(t, f.page(t, bigFellaID), "Water"); got.when != "Due in 10 days" {
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
	if !strings.Contains(text(body), "That time is in the future.") {
		t.Errorf("the refusal says nothing:\n%s", text(body))
	}
	if got := plantHeading(body); got != "Big Fella" {
		t.Errorf("the refusal came back under the heading %q, want the plant's page:\n%s", got, text(body))
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

// givePicture inserts a photo of the plant, with a square variant, and makes it
// the plant's profile picture. It returns the photo's id.
func givePicture(t *testing.T, tx pgx.Tx, plantID uuid.UUID) uuid.UUID {
	t.Helper()

	var photoID uuid.UUID
	err := tx.QueryRow(t.Context(),
		`INSERT INTO photo (garden_id, plant_id, uploaded_by, kind, path, width, height, bytes, square_bytes)
		 VALUES ($1, $2, $3, 'image/jpeg', $4, 3, 2, 100, 40) RETURNING id`,
		rosewoodID, plantID, readerID, plantID.String()+".jpg").Scan(&photoID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), "UPDATE plant SET profile_photo_id = $1 WHERE id = $2", photoID, plantID); err != nil {
		t.Fatal(err)
	}
	return photoID
}

// images returns the src of every img in the markup that has one, in page
// order.
func images(markup string) []string {
	var srcs []string
	for _, img := range readHTML(markup).all(isTag("img"), hasAttr("src")) {
		srcs = append(srcs, img.attr("src"))
	}
	return srcs
}

// picture returns the img showing the photo at its full size, or nil.
func picture(page string, plantID, photoID uuid.UUID) *element {
	return readHTML(page).first(isTag("img"), attrIs("src", photoFullPath(plantID, photoID)))
}

func TestPlant_APlantWithAPictureShowsItAtTheTop(t *testing.T) {
	f := rosewoodPlant(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	page := f.page(t, bigFellaID)

	if got := images(page); len(got) == 0 || got[0] != photoFullPath(bigFellaID, photoID) {
		t.Errorf("the page's images are %v, want the picture at %s first", got, photoFullPath(bigFellaID, photoID))
	}
	if got := picture(page, bigFellaID, photoID).attr("alt"); got != "Picture of Big Fella" {
		t.Errorf("the picture's alt text is %q, want Picture of Big Fella", got)
	}
	link := readHTML(page).first(isTag("a"), attrIs("href", photoPath(bigFellaID, photoID)))
	if link.first(isTag("img"), attrIs("src", photoFullPath(bigFellaID, photoID))) == nil {
		t.Error("the picture is not a link to its photo's page")
	}
}

func TestPlant_ThePictureIsPositionedAtItsFocalPoint(t *testing.T) {
	f := rosewoodPlant(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	centred := f.page(t, bigFellaID)
	f.exec(t, "UPDATE photo SET focus_x = 0, focus_y = 100 WHERE id = $1", photoID)
	moved := f.page(t, bigFellaID)

	if got := picture(centred, bigFellaID, photoID).attr("style"); got != "object-position:50% 50%" {
		t.Errorf("a photo with no focal point set has the style %q, want it centred", got)
	}
	if got := picture(moved, bigFellaID, photoID).attr("style"); got != "object-position:0% 100%" {
		t.Errorf("the picture has the style %q, want it positioned at the bottom left", got)
	}
}

func TestPlant_APlantWithSeveralPhotosPositionsThePictureAtItsOwnFocalPoint(t *testing.T) {
	f := rosewoodPlant(t)
	pictureID := givePicture(t, f.tx, bigFellaID)
	newer := givePhoto(t, f.tx, bigFellaID, readerID, time.Now().AddDate(0, 0, 1))
	f.exec(t, "UPDATE photo SET focus_x = 0, focus_y = 100 WHERE id = $1", pictureID)
	f.exec(t, "UPDATE photo SET focus_x = 100, focus_y = 0 WHERE id = $1", newer)

	page := f.page(t, bigFellaID)

	if got := picture(page, bigFellaID, pictureID).attr("style"); got != "object-position:0% 100%" {
		t.Errorf("the picture has the style %q, want it positioned at the bottom left", got)
	}
}

func TestPlant_APlantWithNoPictureHasNoImage(t *testing.T) {
	page := rosewoodPlant(t).page(t, bigFellaID)

	if got := images(page); len(got) != 0 {
		t.Errorf("the page's images are %v, want none", got)
	}
}

// givePhoto inserts a photo of the plant uploaded by the user at the given
// time and returns its id. Nothing is written to disk.
func givePhoto(t *testing.T, tx pgx.Tx, plantID, uploadedBy uuid.UUID, uploadedAt time.Time) uuid.UUID {
	t.Helper()

	var photoID uuid.UUID
	err := tx.QueryRow(t.Context(),
		`INSERT INTO photo (garden_id, plant_id, uploaded_by, uploaded_at, kind, path, width, height, bytes, square_bytes)
		 VALUES ($1, $2, $3, $4, 'image/jpeg', $5, 3, 2, 100, 40) RETURNING id`,
		rosewoodID, plantID, uploadedBy, uploadedAt, plantID.String()+"/"+uploadedAt.Format(time.RFC3339Nano)+".jpg").Scan(&photoID)
	if err != nil {
		t.Fatal(err)
	}
	return photoID
}

// photosSection returns the Photos section of a plant's page, or nil.
func photosSection(page string) *element {
	return readHTML(page).first(attrIs("aria-labelledby", "photos"))
}

// strip returns the href of every link in the Photos section other than See
// all, in order.
func strip(t *testing.T, page string, plantID uuid.UUID) []string {
	t.Helper()

	section := photosSection(page)
	if section == nil {
		t.Fatal("the page has no Photos section")
	}
	var hrefs []string
	for _, link := range section.all(isTag("a")) {
		if href := link.attr("href"); href != photosPath(plantID) {
			hrefs = append(hrefs, href)
		}
	}
	return hrefs
}

func TestPlant_TheStripShowsTheNewestPhotosFirstAfterTheAddTile(t *testing.T) {
	f := rosewoodPlant(t)
	older := givePhoto(t, f.tx, bigFellaID, readerID, thursday.AddDate(0, 0, -30))
	newer := givePhoto(t, f.tx, bigFellaID, readerID, thursday.AddDate(0, 0, -1))
	givePhoto(t, f.tx, dorisID, readerID, thursday)

	page := f.page(t, bigFellaID)

	want := []string{newPhotoPath(bigFellaID), photoPath(bigFellaID, newer), photoPath(bigFellaID, older)}
	if got := strip(t, page, bigFellaID); !slices.Equal(got, want) {
		t.Errorf("the strip links to %v, want %v", got, want)
	}
	if got, want := photosSection(page).text(), "Photos Add yesterday 4 Aug"; got != want {
		t.Errorf("the Photos section reads %q, want %q", got, want)
	}
}

func TestPlant_TheStripShowsEachPhotosSquare(t *testing.T) {
	f := rosewoodPlant(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	page := f.page(t, bigFellaID)

	var got []string
	for _, img := range photosSection(page).all(isTag("img"), hasAttr("src")) {
		got = append(got, img.attr("src"))
	}
	if want := []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the strip's images are %v, want %v", got, want)
	}
}

func TestPlant_TheStripHoldsSixPhotosAndThenLinksToSeeAll(t *testing.T) {
	f := rosewoodPlant(t)
	for i := range 7 {
		givePhoto(t, f.tx, bigFellaID, readerID, thursday.AddDate(0, 0, -i))
	}

	page := f.page(t, bigFellaID)

	if got := strip(t, page, bigFellaID); len(got) != 7 {
		t.Errorf("the strip has %d links, want the add tile and 6 photos", len(got))
	}
	if got := photosSection(page).first(isTag("a"), attrIs("href", photosPath(bigFellaID))).text(); got != "See all 7" {
		t.Errorf("the link to %s reads %q, want See all 7", photosPath(bigFellaID), got)
	}
}

func TestPlant_TheStripHasNoSeeAllLinkWhileItHoldsEveryPhoto(t *testing.T) {
	f := rosewoodPlant(t)
	givePhoto(t, f.tx, bigFellaID, readerID, thursday)

	page := f.page(t, bigFellaID)

	if strings.Contains(text(page), "See all") {
		t.Error("the Photos head links to See all for a plant whose photos all fit in the strip")
	}
}

func TestPlant_ASitterSeesTheStripWithoutTheAddTile(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}
	photoID := givePhoto(t, f.tx, bigFellaID, readerID, thursday)

	page := f.page(t, bigFellaID)

	if got, want := strip(t, page, bigFellaID), []string{photoPath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the strip links to %v, want %v", got, want)
	}
}

func TestPlant_ASitterOnAPlantWithNoPhotosReadsNoPhotosYet(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	page := f.page(t, bigFellaID)

	if got := photosSection(page).text(); got != "Photos No photos yet." {
		t.Errorf("the Photos section reads %q, want the heading and No photos yet.", got)
	}
}

func TestPlant_AnArchivedPlantHasNoAddTile(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)
	photoID := givePhoto(t, f.tx, bigFellaID, readerID, thursday)

	page := f.page(t, bigFellaID)

	if got, want := strip(t, page, bigFellaID), []string{photoPath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the strip links to %v, want %v", got, want)
	}
}
