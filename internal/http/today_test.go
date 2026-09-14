package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	rosewoodID           = uuid.MustParse("00000000-0000-7000-8000-000000000101")
	readerID             = uuid.MustParse("00000000-0000-7000-8000-000000000102")
	rosewoodMembershipID = uuid.MustParse("00000000-0000-7000-8000-000000000105")
	waterID              = uuid.MustParse("00000000-0000-7000-8000-000000000103")
	feedID               = uuid.MustParse("00000000-0000-7000-8000-000000000104")

	bigFellaID = uuid.MustParse("00000000-0000-7000-8000-000000000111")
	dorisID    = uuid.MustParse("00000000-0000-7000-8000-000000000112")
	nigelID    = uuid.MustParse("00000000-0000-7000-8000-000000000113")
	trailMixID = uuid.MustParse("00000000-0000-7000-8000-000000000114")
	opuntiaID  = uuid.MustParse("00000000-0000-7000-8000-000000000115")
	sproutID   = uuid.MustParse("00000000-0000-7000-8000-000000000116")
	spikeID    = uuid.MustParse("00000000-0000-7000-8000-000000000117")
)

// A Thursday morning in London. The fixture is written against it, so every
// plant lands in the same section on every run.
var thursday = time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)

type todayFixture struct {
	handler   *today
	tx        pgx.Tx
	principal auth.Principal
}

// rosewood seeds a garden that fills every section: one plant overdue, two due
// today, three coming up, and one beyond the week for the empty state to name.
// Every schedule is a watering last performed at noon in London.
func rosewood(t *testing.T) *todayFixture {
	t.Helper()

	tx := pgtest.Tx(t, migrateSchema)
	garden := ownedGarden{
		id:           rosewoodID,
		name:         "Rosewood",
		owner:        store.AppUser{ID: readerID, DisplayName: "Ellie", Handle: "ellie", Timezone: "Europe/London"},
		membershipID: rosewoodMembershipID,
		careTypes:    []store.CareType{{ID: waterID, Name: "Water", Slug: "water"}, {ID: feedID, Name: "Feed", Slug: "feed"}},
	}
	garden.insert(t, tx)

	plants := []struct {
		id                                    uuid.UUID
		nickname, common, botanical, location string
		everyDays                             int
		lastWatered                           time.Time
	}{
		{bigFellaID, "Big Fella", "Swiss cheese plant", "Monstera deliciosa", "Living room", 10, day(time.August, 22)}, // 1 September
		{dorisID, "Doris", "Snake plant", "Dracaena trifasciata", "Bedroom", 21, day(time.August, 13)},                 // 3 September
		{nigelID, "Nigel", "Boston fern", "Nephrolepis exaltata", "Bathroom", 4, day(time.August, 30)},                 // 3 September
		{trailMixID, "Trail Mix", "Golden pothos", "Epipremnum aureum", "Kitchen", 9, day(time.August, 26)},            // 4 September
		{opuntiaID, "", "", "Opuntia microdasys", "Windowsill", 35, day(time.August, 3)},                               // 7 September
		{sproutID, "Sprout", "", "", "", 11, day(time.August, 28)},                                                     // 8 September
		{spikeID, "Spike", "Golden barrel cactus", "Echinocactus grusonii", "Windowsill", 30, day(time.August, 16)},    // 15 September
	}
	for _, p := range plants {
		mustExec(t, tx, "INSERT INTO plant (id, garden_id, nickname, common_name, botanical_name, location) VALUES ($1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''), nullif($6, ''))",
			p.id, rosewoodID, p.nickname, p.common, p.botanical, p.location)
		mustExec(t, tx, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, $4, 'day', $5)",
			rosewoodID, p.id, waterID, p.everyDays, p.lastWatered.AddDate(0, 0, -p.everyDays))
		mustExec(t, tx, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
			rosewoodID, p.id, waterID, readerID, p.lastWatered)
	}
	// Nigel gets a second care because the sheet shows the Care field only for
	// a plant with more than one. set_at puts the feed a week out so his row
	// stays a watering.
	mustExec(t, tx, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 3, 'week', $4)",
		rosewoodID, nigelID, feedID, day(time.August, 20))

	principal := garden.principal(auth.CareLog, auth.PlantCreate, auth.PlantEdit, auth.PlantArchive, auth.ScheduleEdit, auth.PhotoAdd, auth.PhotoSetProfile)
	handler := &today{logger: testLogger, queries: store.New(tx), templates: testTemplates(), now: func() time.Time { return thursday }}
	return &todayFixture{handler: handler, tx: tx, principal: principal}
}

// day returns noon in London, so a watering and its due day share a date in
// London and in UTC.
func day(month time.Month, d int) time.Time {
	return time.Date(2026, month, d, 12, 0, 0, 0, london())
}

func london() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(err)
	}
	return loc
}

func (f *todayFixture) show(t *testing.T) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	f.handler.show(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *todayFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	mustExec(t, f.tx, sql, args...)
}

func (f *todayFixture) water(t *testing.T, plantID uuid.UUID) {
	t.Helper()
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, plantID, waterID, readerID, thursday)
}

// sectionHead matches the start of a section's text: its title and the count
// beside it.
var sectionHead = regexp.MustCompile(`^(?:Overdue|Due today|Coming up) (\d+)\b`)

// sectionsOf returns each section on Today by the id of its heading, and the
// ids in page order.
func sectionsOf(page string) (map[string]*element, []string) {
	byID := map[string]*element{}
	var order []string
	for _, section := range readHTML(page).all(isTag("section"), hasAttr("aria-labelledby")) {
		id := section.attr("aria-labelledby")
		byID[id] = section
		order = append(order, id)
	}
	return byID, order
}

// rows returns the text of each care row in a section, by the row's id.
func rows(section *element) map[string]string {
	byID := map[string]string{}
	for _, row := range section.all(isTag("li"), hasAttr("id")) {
		byID[row.attr("id")] = row.text()
	}
	return byID
}

// count is the number beside a section's title.
func count(t *testing.T, section *element) string {
	t.Helper()
	m := sectionHead.FindStringSubmatch(section.text())
	if m == nil {
		t.Fatalf("the section has no count:\n%s", section.text())
	}
	return m[1]
}

func rowID(plantID uuid.UUID) string {
	return "care-" + plantID.String() + "-water"
}

// imageSources returns the src of every img in e, in page order.
func imageSources(e *element) []string {
	var srcs []string
	for _, img := range e.all(isTag("img")) {
		srcs = append(srcs, img.attr("src"))
	}
	return srcs
}

func TestToday_PlacesEachPlantInTheSectionItsCareFallsIn(t *testing.T) {
	page := rosewood(t).show(t)

	byID, order := sectionsOf(page)
	if want := []string{"overdue", "due-today", "coming-up"}; strings.Join(order, " ") != strings.Join(want, " ") {
		t.Fatalf("sections = %v, want %v", order, want)
	}

	want := map[string][]uuid.UUID{
		"overdue":   {bigFellaID},
		"due-today": {dorisID, nigelID},
		"coming-up": {trailMixID, opuntiaID, sproutID},
	}
	for id, plants := range want {
		got := rows(byID[id])
		if len(got) != len(plants) {
			t.Errorf("%s holds %d rows, want %d", id, len(got), len(plants))
		}
		for _, plant := range plants {
			if _, ok := got[rowID(plant)]; !ok {
				t.Errorf("%s has no row with the id %s", id, rowID(plant))
			}
		}
		if c := count(t, byID[id]); c != strconv.Itoa(len(plants)) {
			t.Errorf("%s counts %s, want %d", id, c, len(plants))
		}
	}
	if readHTML(page).byID(rowID(spikeID)) != nil {
		t.Error("Spike is on the page, and is not due for another twelve days")
	}
	if !strings.Contains(text(page), "3 plants due today, 1 of them overdue.") {
		t.Errorf("the summary does not say three plants need attention and one is overdue:\n%s", text(page))
	}
}

func TestToday_ARowShowsLatenessOrTheDueDayButNotBoth(t *testing.T) {
	page := rosewood(t).show(t)
	byID, _ := sectionsOf(page)
	all := map[string]string{}
	for _, section := range byID {
		for id, row := range rows(section) {
			all[id] = row
		}
	}

	cases := []struct {
		name  string
		plant uuid.UUID
		want  string
	}{
		{"an overdue row shows how late it is", bigFellaID, "Big Fella Living room · 2 days late Water"},
		{"a row due today shows the location and nothing else", dorisID, "Doris Bedroom Water"},
		{"a coming-up row shows the day and a Log button", trailMixID, "Trail Mix Kitchen · Water tomorrow Log"},
		{"a row inside the week names the weekday", opuntiaID, "Opuntia microdasys Windowsill · Water Monday Log"},
		{"a row with no location has no dot before the day", sproutID, "Sprout Water Tuesday Log"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row, ok := all[rowID(c.plant)]
			if !ok {
				t.Fatalf("no row with the id %s", rowID(c.plant))
			}
			if row != c.want {
				t.Errorf("the row says %q, want %q", row, c.want)
			}
		})
	}
}

func TestToday_TheCountsDropAsCaresAreLogged(t *testing.T) {
	f := rosewood(t)

	f.water(t, dorisID)
	page := f.show(t)
	byID, _ := sectionsOf(page)
	if c := count(t, byID["due-today"]); c != "1" {
		t.Errorf("after watering Doris the section counts %s, want 1", c)
	}
	if readHTML(page).byID(rowID(dorisID)) != nil {
		t.Error("Doris is still on the page after being watered")
	}
	if !strings.Contains(text(page), "2 plants due today, 1 of them overdue.") {
		t.Errorf("the summary did not fall to two:\n%s", text(page))
	}

	f.water(t, nigelID)
	page = f.show(t)
	byID, order := sectionsOf(page)
	if _, ok := byID["due-today"]; ok {
		t.Errorf("Due today is still rendered with nothing in it: %v", order)
	}
	if !strings.Contains(text(page), "1 plant due today, 1 of them overdue.") {
		t.Errorf("the summary did not fall to one plant:\n%s", text(page))
	}

	f.water(t, bigFellaID)
	page = f.show(t)
	words := text(page)
	if strings.Contains(words, "you today") {
		t.Error("the summary bar is rendered with nothing outstanding")
	}
	if !strings.Contains(words, "All done for today") || !strings.Contains(words, "Nothing else is due.") {
		t.Errorf("the page does not say the day is done:\n%s", words)
	}
	if _, order := sectionsOf(page); strings.Join(order, " ") != "coming-up" {
		t.Errorf("sections = %v, want Coming up alone under the empty state", order)
	}
	// The Coming up section already says what is next.
	if strings.Contains(words, "is next") || strings.Contains(words, "See all plants") {
		t.Error("the empty state names what is next over a Coming up list that names it too")
	}
}

func TestToday_TheEmptyStateDependsOnWhyTheListIsEmpty(t *testing.T) {
	clearTheWeek := func(t *testing.T, f *todayFixture) {
		t.Helper()
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = ANY($1)", []uuid.UUID{bigFellaID, dorisID, nigelID, trailMixID, opuntiaID, sproutID})
	}
	// linkNamed returns the href of the link whose text is label, or "" when
	// the page has no such link.
	linkNamed := func(page *element, label string) string {
		return page.first(isTag("a"), textIs(label)).attr("href")
	}

	t.Run("a finished day with nothing coming up shows done and names the next care", func(t *testing.T) {
		f := rosewood(t)
		for _, plant := range []uuid.UUID{bigFellaID, dorisID, nigelID} {
			f.water(t, plant)
		}
		// Watering Nigel would put his four-day schedule back into Coming up.
		clearTheWeek(t, f)

		page := readHTML(f.show(t))
		for _, want := range []string{"All done for today", "Spike is next, in 12 days."} {
			if !strings.Contains(page.text(), want) {
				t.Errorf("the page lacks %s:\n%s", want, page.text())
			}
		}
		if got := linkNamed(page, "See all plants"); got != plantsPath {
			t.Errorf("See all plants points at %q, want %q", got, plantsPath)
		}
	})

	t.Run("two plants due the same day beyond the week are both named", func(t *testing.T) {
		f := rosewood(t)
		// Doris was last watered on 13 August, so a 33-day interval puts the next
		// watering on 15 September, the day Spike's is due.
		f.exec(t, "UPDATE care_schedule SET interval_count = 33 WHERE plant_id = $1", dorisID)
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = ANY($1)", []uuid.UUID{bigFellaID, nigelID, trailMixID, opuntiaID, sproutID})

		page := text(f.show(t))
		if want := "Doris and Spike are next, in 12 days."; !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	})

	t.Run("a day with nothing scheduled shows the leaf rather than the tick", func(t *testing.T) {
		f := rosewood(t)
		clearTheWeek(t, f)

		page := readHTML(f.show(t))
		for _, want := range []string{"Nothing due today", "Spike is next, in 12 days."} {
			if !strings.Contains(page.text(), want) {
				t.Errorf("the page lacks %s:\n%s", want, page.text())
			}
		}
		if got := linkNamed(page, "See all plants"); got != plantsPath {
			t.Errorf("See all plants points at %q, want %q", got, plantsPath)
		}
		if strings.Contains(page.text(), "All done") {
			t.Error("a day with nothing scheduled says All done")
		}
	})

	t.Run("a garden with nothing scheduled at all has the heading and no line under it", func(t *testing.T) {
		f := rosewood(t)
		clearTheWeek(t, f)
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = $1", spikeID)

		page := text(f.show(t))
		if !strings.Contains(page, "Nothing due today") {
			t.Errorf("the page does not say nothing is due:\n%s", page)
		}
		if strings.Contains(page, "Nothing is due or overdue.") {
			t.Errorf("the page repeats the heading in a line under it:\n%s", page)
		}
		if strings.Contains(page, "is next") {
			t.Error("the page names a next plant when nothing is scheduled")
		}
	})

	t.Run("a garden with no plants shows an add plant link", func(t *testing.T) {
		f := rosewood(t)
		f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

		page := readHTML(f.show(t))
		for _, want := range []string{"No plants yet", "Add a plant to see its tasks here."} {
			if !strings.Contains(page.text(), want) {
				t.Errorf("the page lacks %s:\n%s", want, page.text())
			}
		}
		if got := linkNamed(page, "Add plant"); got != newPlantPath {
			t.Errorf("Add plant points at %q, want %q", got, newPlantPath)
		}
	})

	// The link points at a route a sitter may not open.
	t.Run("a sitter in an empty garden gets no add plant link", func(t *testing.T) {
		f := rosewood(t)
		f.principal.Capabilities = auth.Capabilities{}
		f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

		page := readHTML(f.show(t))
		if !strings.Contains(page.text(), "No plants yet") {
			t.Errorf("the page does not say the garden is empty:\n%s", page.text())
		}
		if page.first(attrIs("href", newPlantPath)) != nil {
			t.Errorf("a sitter is offered the way to add a plant:\n%s", page.text())
		}
	})

	t.Run("an archived plant is 404", func(t *testing.T) {
		f := rosewood(t)
		f.exec(t, "UPDATE plant SET archived_at = now() WHERE garden_id = $1", rosewoodID)

		if page := text(f.show(t)); !strings.Contains(page, "No plants yet") {
			t.Errorf("a garden of archived plants is not the first run:\n%s", page)
		}
	})
}

// 22:00 UTC on Thursday is 23:00 in London the same evening and 10:00 in
// Auckland on Friday, when Trail Mix's watering is due.
func TestToday_ReadsTheDayInTheReadersTimezone(t *testing.T) {
	f := rosewood(t)
	late := thursday.Add(14 * time.Hour)
	f.handler.now = func() time.Time { return late }

	page := f.show(t)
	if !strings.Contains(text(page), "Thursday 3 September") {
		t.Errorf("London's date is not Thursday 3 September:\n%s", text(page))
	}
	byID, _ := sectionsOf(page)
	if _, ok := rows(byID["coming-up"])[rowID(trailMixID)]; !ok {
		t.Error("in London on Thursday evening Trail Mix is not coming up")
	}

	f.principal.User.Timezone = "Pacific/Auckland"
	page = f.show(t)
	if !strings.Contains(text(page), "Friday 4 September") {
		t.Errorf("Auckland's date is not Friday 4 September:\n%s", text(page))
	}
	byID, _ = sectionsOf(page)
	if _, ok := rows(byID["due-today"])[rowID(trailMixID)]; !ok {
		t.Error("in Auckland on Friday morning Trail Mix is not due today")
	}
}

func TestToday_ThePageHeadingShowsTheDateAndTheGardenName(t *testing.T) {
	page := readHTML(rosewood(t).show(t))

	if got := page.first(isTag("title")).text(); got != "Today · sprig" {
		t.Errorf("the title is %q, want %q", got, "Today · sprig")
	}
	if !strings.Contains(page.text(), "Thursday 3 September") {
		t.Errorf("the page does not say the date:\n%s", page.text())
	}
	if got := page.first(isTag("a"), attrIs("aria-current", "page")).attr("href"); got != todayPath {
		t.Errorf("the tab marked as the current page points at %q, want Today at %q", got, todayPath)
	}
	if got := page.first(isTag("h1")).text(); got != "Rosewood" {
		t.Errorf("the page's heading is %q, want the garden's name", got)
	}
}

// feed returns each line of the feed, in page order.
func feed(t *testing.T, page string) []*element {
	t.Helper()
	activity := readHTML(page).byID("activity")
	if activity == nil {
		t.Fatalf("the page has no feed:\n%s", page)
	}
	return activity.all(isTag("div"))
}

// says returns each feed line's text.
func says(lines []*element) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line.text())
	}
	return out
}

func TestFeed_ListsEventsNewestFirst(t *testing.T) {
	got := says(feed(t, rosewood(t).show(t)))
	want := []string{
		"You watered Nigel · Sunday",
		"You watered Sprout · Friday",
		"You watered Trail Mix · 26 Aug",
		"You watered Big Fella · 22 Aug",
		"You watered Spike · 16 Aug",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the feed reads\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFeed_NamesAnotherPersonByTheirDisplayName(t *testing.T) {
	f := rosewood(t)
	sam := uuid.MustParse("00000000-0000-7000-8000-000000000198")
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", sam)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, dorisID, feedID, sam, thursday.Add(-time.Hour))

	if got := says(feed(t, f.show(t)))[0]; got != "Sam fed Doris · today, 8:00am" {
		t.Errorf("the feed's newest line reads %q, want Sam named as the one who fed Doris", got)
	}
}

func TestFeed_ShowsASkipAsSkipped(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, override_interval_days) VALUES ($1, $2, $3, $4, $5, $5, false, 2)",
		rosewoodID, dorisID, waterID, readerID, thursday)

	if got := says(feed(t, f.show(t)))[0]; got != "You skipped Doris · today, 9:00am" {
		t.Errorf("the feed's newest line reads %q, want the skip said as a skip", got)
	}
}

func TestFeed_ShowsUndoOnlyOnTheReadersOwnCareInsideTheWindow(t *testing.T) {
	sam := uuid.MustParse("00000000-0000-7000-8000-000000000198")
	// Sam's watering of Doris is lines[0] and the reader's watering of Nigel is
	// lines[1], both logged a moment ago. Every seeded event is days old.
	setup := func(t *testing.T) *todayFixture {
		f := rosewood(t)
		f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", sam)
		f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
			rosewoodID, dorisID, waterID, sam, thursday)
		f.exec(t, "UPDATE care_event SET recorded_at = $1 WHERE plant_id = $2 AND performed_by = $3", thursday.Add(-time.Second), nigelID, readerID)
		return f
	}

	t.Run("a sitter who may delete nothing sees no undo button", func(t *testing.T) {
		f := setup(t)
		for _, line := range feed(t, f.show(t)) {
			if line.first(isTag("form")) != nil {
				t.Errorf("a reader with no delete capability was offered %q", line.text())
			}
		}
	})

	t.Run("a member sees undo on their own care", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		line := feed(t, f.show(t))[1]
		if got, want := line.first(isTag("form")).attr("action"), undoFormPath(nigelID, f.events(t, nigelID)[0].ID); got != want {
			t.Errorf("the reader's own watering has an undo posting to %q, want %q", got, want)
		}
	})

	t.Run("nobody sees undo on somebody else's care", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		f.principal.Capabilities[auth.CareDeleteAny] = true
		if feed(t, f.show(t))[0].first(attrIs("action", undoFormPath(dorisID, f.events(t, dorisID)[1].ID))) != nil {
			t.Errorf("an owner was offered Sam's watering back as an undo of their own")
		}
	})

	t.Run("nobody sees undo on a care recorded before the window", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		f.exec(t, "UPDATE care_event SET recorded_at = $1 WHERE plant_id = $2 AND performed_by = $3",
			thursday.Add(-undoWindow), nigelID, readerID)

		for _, line := range feed(t, f.show(t)) {
			if line.first(isTag("form")) != nil {
				t.Errorf("a care recorded a window ago was offered back: %q", line.text())
			}
		}
	})
}

// The feed element has to be on the page for a swap to fill it. No whitespace
// is allowed inside it because the stylesheet hides the feed with :empty.
func TestFeed_IsEmptyInAGardenWithNothingLogged(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)

	if activity := readHTML(f.show(t)).byID("activity"); activity == nil || len(activity.children) != 0 {
		t.Errorf("the page has no empty feed for a swap to fill: %s", activity)
	}
}

var (
	fairviewGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000118")
	// robinUserID is Robin, who owns Fairview. The name robinID is taken by
	// the development sign-in tests.
	robinUserID = uuid.MustParse("00000000-0000-7000-8000-000000000119")
	// joUserID is Jo, given an owner membership of Rosewood dated before
	// Ellie's in one test.
	joUserID = uuid.MustParse("00000000-0000-7000-8000-000000000120")
)

// fairview is a second garden, owned by Robin. Ellie is not in it unless a
// test adds her.
var fairview = ownedGarden{
	id:    fairviewGardenID,
	name:  "Fairview",
	owner: store.AppUser{ID: robinUserID, DisplayName: "Robin", Handle: "robin", Timezone: "Europe/Lisbon"},
}

// switchLink returns the link after the garden's name in Today's top bar, or
// nil when the page has none.
func switchLink(page string) *element {
	return readHTML(page).first(isTag("a"), attrIs("aria-label", "Switch garden"))
}

// topBar returns the text of Today's top bar: the date, the garden's name, and
// whose garden it is when somebody other than the reader created it.
func topBar(page string) string {
	return readHTML(page).first(isTag("header")).text()
}

type renderedGardenRow struct {
	// says is the row's text: the garden's name, Current on the garden the
	// session is on, whose garden it is and the reader's role.
	says string
	// button is true when the row is a button that posts the switch. The
	// current garden's row is not one.
	button bool
	// posts is the garden id the row's form posts, and empty for the current
	// garden.
	posts string
}

// gardenSheetOf returns the rows of the open garden sheet, in page order. It
// returns nil when the page has no open garden sheet.
func gardenSheetOf(page string) []renderedGardenRow {
	sheet := readHTML(page).byID("sheet")
	if !sheet.has("open") || sheet.attr("aria-labelledby") != "garden-sheet-title" {
		return nil
	}
	var out []renderedGardenRow
	for _, item := range sheet.all(isTag("li")) {
		out = append(out, renderedGardenRow{
			says:   item.text(),
			button: item.first(isTag("button"), attrIs("type", "submit")) != nil,
			posts:  item.first(isTag("input"), attrIs("name", "garden")).attr("value"),
		})
	}
	return out
}

// gardens calls GET /gardens and returns the body. With hx set, the request
// has the htmx headers that target the sheet, so the handler renders the sheet
// alone.
func (f *todayFixture) gardens(t *testing.T, hx bool) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, gardensPath, nil)
	if hx {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "sheet")
	}
	rec := httptest.NewRecorder()
	f.handler.gardenSheet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *todayFixture) joinFairview(t *testing.T, expiresAt *time.Time) {
	t.Helper()
	fairview.insert(t, f.tx)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour, expires_at) VALUES ($1, $2, 'sitter', 8, $3)",
		fairviewGardenID, readerID, expiresAt)
}

func TestToday_TheTopBarHasNoSwitchIconForAnAccountInOneGarden(t *testing.T) {
	page := rosewood(t).show(t)

	if link := switchLink(page); link != nil {
		t.Errorf("the top bar has a switch icon linking to %s, and Ellie is in one garden", link.attr("href"))
	}
}

func TestToday_TheSwitchIconOpensTheGardenSheetAndThePageLoadsWithoutIt(t *testing.T) {
	f := rosewood(t)
	f.joinFairview(t, nil)

	page := f.show(t)

	link := switchLink(page)
	if link == nil {
		t.Fatal("the top bar has no switch icon")
	}
	if got := link.attr("href"); got != gardensPath {
		t.Errorf("the icon links to %s, want %s", got, gardensPath)
	}
	if got := gardenSheetOf(page); got != nil {
		t.Errorf("Today loaded with the garden sheet open: %+v", got)
	}
}

// Ellie's membership of Fairview ends at 23:30 UTC on 7 September. That is
// 00:30 on 8 September in Europe/London, so the row reads "until 8 Sep" only
// when the date is formatted in the reader's timezone.
func TestToday_TheGardenSheetListsEveryLiveGardenWithItsOwnerAndRoleAndMarksTheCurrentOne(t *testing.T) {
	f := rosewood(t)
	ends := time.Date(2026, time.September, 7, 23, 30, 0, 0, time.UTC)
	f.joinFairview(t, &ends)

	want := []renderedGardenRow{
		{says: "Rosewood Current Your garden · Owner"},
		{says: "Fairview Robin’s garden · Sitter · until 8 Sep", button: true, posts: fairviewGardenID.String()},
	}
	for _, hx := range []bool{false, true} {
		page := f.gardens(t, hx)
		if got := gardenSheetOf(page); !slices.Equal(got, want) {
			t.Errorf("with htmx = %v the sheet is\n%+v\nwant\n%+v", hx, got, want)
		}
		hasHeading := readHTML(page).first(isTag("h1")) != nil
		if hasHeading == hx {
			t.Errorf("with htmx = %v the response has a page heading = %v, want %v", hx, hasHeading, !hx)
		}
	}
}

func TestToday_AnEndedSecondMembershipPutsNoSwitchIconInTheTopBar(t *testing.T) {
	f := rosewood(t)
	ended := thursday.AddDate(0, 0, -1)
	f.joinFairview(t, &ended)

	if link := switchLink(f.show(t)); link != nil {
		t.Errorf("the top bar has a switch icon linking to %s, and the only other membership has ended", link.attr("href"))
	}
}

// Robin owns Fairview and Ellie is not in it. A query that stopped filtering
// on the signed-in account would list Fairview.
func TestToday_AnotherAccountsGardenIsNotInTheGardenSheet(t *testing.T) {
	f := rosewood(t)
	fairview.insert(t, f.tx)

	want := []renderedGardenRow{{says: "Rosewood Current Your garden · Owner"}}
	if got := gardenSheetOf(f.gardens(t, false)); !slices.Equal(got, want) {
		t.Errorf("the sheet is\n%+v\nwant\n%+v", got, want)
	}
}

func TestToday_TheOwnersOwnGardenSaysNothingUnderItsName(t *testing.T) {
	page := rosewood(t).show(t)

	if got, want := topBar(page), "Thursday 3 September Rosewood"; got != want {
		t.Errorf("the top bar says %q, want %q, and Ellie owns Rosewood", got, want)
	}
}

// Robin's owner membership is dated before Ellie's, so Robin is the garden's
// owner even though Ellie is an owner too. The line names whoever created the
// garden, not the reader's role.
func TestToday_AGardenAnotherPersonCreatedSaysWhoseItIsUnderItsNameEvenToASecondOwner(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Robin', 'robin', 'Europe/Lisbon')", robinUserID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour, created_at) VALUES ($1, $2, 'owner', 8, $3)",
		rosewoodID, robinUserID, thursday.AddDate(-1, 0, 0))

	page := f.show(t)

	if got, want := topBar(page), "Thursday 3 September Rosewood Robin’s garden"; got != want {
		t.Errorf("the top bar says %q, want %q", got, want)
	}
}

func TestToday_AGardenWithNoOwnerLeftSaysNothingUnderItsName(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "UPDATE membership SET role = 'member' WHERE garden_id = $1 AND user_id = $2", rosewoodID, readerID)
	f.principal.Membership.Role = "member"

	page := f.show(t)

	if got, want := topBar(page), "Thursday 3 September Rosewood"; got != want {
		t.Errorf("the top bar says %q, want %q, and nobody owns Rosewood", got, want)
	}
	if got := gardenSheetOf(f.gardens(t, false)); len(got) != 1 || got[0].says != "Rosewood Current Member" {
		t.Errorf("the sheet is %+v, want one row reading Member with no owner", got)
	}
}

// The log-care sheet renders the whole page around it without JavaScript, so
// the top bar has to be loaded there too.
func TestToday_TheLogCareSheetPageKeepsTheSwitchIconAndTheOwnerLine(t *testing.T) {
	f := rosewood(t)
	f.joinFairview(t, nil)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Jo', 'jo', 'Europe/London')", joUserID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour, created_at) VALUES ($1, $2, 'owner', 8, $3)",
		rosewoodID, joUserID, thursday.AddDate(-1, 0, 0))

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, logPath(bigFellaID), nil)
	req.SetPathValue("plant", bigFellaID.String())
	rec := httptest.NewRecorder()
	f.handler.sheet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()

	if switchLink(page) == nil {
		t.Error("the sheet page has no switch icon in the top bar")
	}
	if got, want := topBar(page), "Thursday 3 September Rosewood Jo’s garden"; got != want {
		t.Errorf("the sheet page's top bar says %q, want %q", got, want)
	}
}

func TestToday_ARowShowsThePlantsPictureAsItsSquare(t *testing.T) {
	f := rosewood(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	page := readHTML(f.show(t))

	if got, want := imageSources(page), []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the page's images are %v, want Big Fella's square at %v and none on the other rows", got, want)
	}
}

func TestToday_TheRemindersBannerIsRenderedOnlyWithPushOnAndANotificationTypeOn(t *testing.T) {
	cases := []struct {
		name     string
		pushKey  string
		digest   bool
		activity bool
		rendered bool
	}{
		{"push on and the digest on", testPushKey, true, false, true},
		{"push on and only activity on", testPushKey, false, true, true},
		{"push on and both types off", testPushKey, false, false, false},
		{"push off", "", true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := rosewood(t)
			f.handler.pushKey = c.pushKey
			f.exec(t, "INSERT INTO notification_preference (membership_id, kind, enabled) VALUES ($1, 'digest', $2), ($1, 'activity', $3)", rosewoodMembershipID, c.digest, c.activity)

			page := f.show(t)

			banner := readHTML(page).byID("reminders")
			if (banner != nil) != c.rendered {
				t.Fatalf("the banner is rendered = %v, want %v:\n%s", banner != nil, c.rendered, page)
			}
			if !c.rendered {
				return
			}
			// The banner is hidden until the script decides this browser
			// should see it.
			if !banner.has("hidden") {
				t.Errorf("the banner is not hidden, and the script has not run:\n%s", banner)
			}
			if got := banner.attr("data-key"); got != testPushKey {
				t.Errorf("the banner's data-key is %q, want the key the browser subscribes with", got)
			}
			if !strings.Contains(banner.text(), "Turn on notifications") || !strings.Contains(banner.text(), "Not now") {
				t.Errorf("the banner does not offer Turn on notifications and Not now:\n%s", banner.text())
			}
		})
	}
}

func TestToday_OnlyComingUpLinksToTheCalendar(t *testing.T) {
	f := rosewood(t)

	sections, _ := sectionsOf(f.show(t))

	for id, want := range map[string]string{"overdue": "", "due-today": "", "coming-up": "See more in the calendar"} {
		if got := sections[id].first(isTag("a"), attrIs("href", calendarPath)).text(); got != want {
			t.Errorf("the link to the calendar in %s reads %q, want %q", id, got, want)
		}
	}
}
