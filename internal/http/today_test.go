package http

import (
	"context"
	"html"
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

	ctx := t.Context()
	tx := pgtest.Tx(t, migrateSchema)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, sql)
		}
	}

	exec("INSERT INTO garden (id, name) VALUES ($1, 'Rosewood')", rosewoodID)
	exec("INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ellie', 'ellie', 'Europe/London')", readerID)
	exec("INSERT INTO membership (id, garden_id, user_id, role, digest_hour) VALUES ($1, $2, $3, 'owner', 8)", rosewoodMembershipID, rosewoodID, readerID)
	exec("INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water'), ($3, $2, 'Feed', 'feed')", waterID, rosewoodID, feedID)

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
		exec("INSERT INTO plant (id, garden_id, nickname, common_name, botanical_name, location) VALUES ($1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''), nullif($6, ''))",
			p.id, rosewoodID, p.nickname, p.common, p.botanical, p.location)
		exec("INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, $4, 'day', $5)",
			rosewoodID, p.id, waterID, p.everyDays, p.lastWatered.AddDate(0, 0, -p.everyDays))
		exec("INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
			rosewoodID, p.id, waterID, readerID, p.lastWatered)
	}
	// Nigel gets a second care because the sheet shows the Care field only for
	// a plant with more than one. set_at puts the feed a week out so his row
	// stays a watering.
	exec("INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 3, 'week', $4)",
		rosewoodID, nigelID, feedID, day(time.August, 20))

	queries := store.New(tx)
	principal := auth.Principal{
		User:         store.AppUser{ID: readerID, DisplayName: "Ellie", Handle: "ellie", Timezone: "Europe/London"},
		Garden:       store.Garden{ID: rosewoodID, Name: "Rosewood"},
		Membership:   store.Membership{ID: rosewoodMembershipID, Role: "owner"},
		Capabilities: auth.Capabilities{auth.CareLog: true, auth.PlantCreate: true, auth.PlantEdit: true, auth.PlantArchive: true, auth.ScheduleEdit: true, auth.PhotoAdd: true, auth.PhotoSetProfile: true},
	}
	handler := &today{logger: testLogger, queries: queries, templates: testTemplates(), now: func() time.Time { return thursday }}
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
	if _, err := f.tx.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

func (f *todayFixture) water(t *testing.T, plantID uuid.UUID) {
	t.Helper()
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, plantID, waterID, readerID, thursday)
}

var (
	sectionElement = regexp.MustCompile(`(?s)<section[^>]*aria-labelledby="([a-z0-9-]+)"[^>]*>.*?</section>`)
	rowElement     = regexp.MustCompile(`(?s)<li[^>]*id="(care-[^"]+)"[^>]*>.*?</li>`)
	headElement    = regexp.MustCompile(`(?s)<div[^>]*id="day-head"[^>]*>.*?</div>`)
	bannerElement  = regexp.MustCompile(`(?s)<div[^>]*id="reminders"[^>]*>.*?</div>\n</div>`)
	// The feed's closing </div> is the one after a newline, since each line
	// opens and closes on one line.
	feedElement     = regexp.MustCompile(`(?s)<div[^>]*id="activity"[^>]*>(.*?)\n</div>`)
	feedLineElement = regexp.MustCompile(`(?s)<div>.*?</div>`)
	sectionHead     = regexp.MustCompile(`^(?:Overdue|Due today|Coming up) (\d+)\b`)
	tag             = regexp.MustCompile(`<[^>]+>`)
	spaces          = regexp.MustCompile(`\s+`)
)

// text returns the visible text of some markup, tags removed and whitespace
// collapsed, so a test asserts the words rather than the spans around them.
func text(markup string) string {
	s := html.UnescapeString(tag.ReplaceAllString(markup, " "))
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// sections returns each section's markup by its heading id, and the order they
// appear in.
func sections(page string) (map[string]string, []string) {
	byID := map[string]string{}
	var order []string
	for _, m := range sectionElement.FindAllStringSubmatch(page, -1) {
		byID[m[1]] = m[0]
		order = append(order, m[1])
	}
	return byID, order
}

// rows returns each row's text in a section, by row id.
func rows(section string) map[string]string {
	byID := map[string]string{}
	for _, m := range rowElement.FindAllStringSubmatch(section, -1) {
		byID[m[1]] = text(m[0])
	}
	return byID
}

// count is the number beside a section's title.
func count(t *testing.T, section string) string {
	t.Helper()
	m := sectionHead.FindStringSubmatch(text(section))
	if m == nil {
		t.Fatalf("the section carries no count:\n%s", text(section))
	}
	return m[1]
}

func rowID(plantID uuid.UUID) string {
	return "care-" + plantID.String() + "-water"
}

func TestToday_PlacesEachPlantInTheSectionItsCareFallsIn(t *testing.T) {
	page := rosewood(t).show(t)

	byID, order := sections(page)
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
	if strings.Contains(page, rowID(spikeID)) {
		t.Error("Spike is on the page, and is not due for another twelve days")
	}
	if !strings.Contains(text(page), "3 plants due today, 1 of them overdue.") {
		t.Errorf("the summary does not say three plants need attention and one is overdue:\n%s", text(page))
	}
}

func TestToday_ARowShowsLatenessOrTheDueDayButNotBoth(t *testing.T) {
	page := rosewood(t).show(t)
	byID, _ := sections(page)
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
	byID, _ := sections(page)
	if c := count(t, byID["due-today"]); c != "1" {
		t.Errorf("after watering Doris the section counts %s, want 1", c)
	}
	if strings.Contains(page, rowID(dorisID)) {
		t.Error("Doris is still on the page after being watered")
	}
	if !strings.Contains(text(page), "2 plants due today, 1 of them overdue.") {
		t.Errorf("the summary did not fall to two:\n%s", text(page))
	}

	f.water(t, nigelID)
	page = f.show(t)
	byID, order := sections(page)
	if _, ok := byID["due-today"]; ok {
		t.Errorf("Due today is still drawn with nothing in it: %v", order)
	}
	if !strings.Contains(text(page), "1 plant due today, 1 of them overdue.") {
		t.Errorf("the summary did not fall to one plant:\n%s", text(page))
	}

	f.water(t, bigFellaID)
	page = f.show(t)
	if strings.Contains(text(page), "you today") {
		t.Error("the summary bar is drawn with nothing outstanding")
	}
	if !strings.Contains(page, "All done for today") || !strings.Contains(page, "Nothing else is due.") {
		t.Errorf("the page does not say the day is done:\n%s", text(page))
	}
	if _, order := sections(page); strings.Join(order, " ") != "coming-up" {
		t.Errorf("sections = %v, want Coming up alone under the empty state", order)
	}
	// The Coming up section already says what is next.
	if strings.Contains(page, "is next") || strings.Contains(page, "See all plants") {
		t.Error("the empty state names what is next over a Coming up list that names it too")
	}
}

func TestToday_TheEmptyStateDependsOnWhyTheListIsEmpty(t *testing.T) {
	clearTheWeek := func(t *testing.T, f *todayFixture) {
		t.Helper()
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = ANY($1)", []uuid.UUID{bigFellaID, dorisID, nigelID, trailMixID, opuntiaID, sproutID})
	}

	t.Run("a finished day with nothing coming up shows done and names the next care", func(t *testing.T) {
		f := rosewood(t)
		for _, plant := range []uuid.UUID{bigFellaID, dorisID, nigelID} {
			f.water(t, plant)
		}
		// Watering Nigel would put his four-day schedule back into Coming up.
		clearTheWeek(t, f)

		page := f.show(t)
		for _, want := range []string{"All done for today", "Spike is next, in 12 days.", `href="/plants"`, "See all plants"} {
			if !strings.Contains(page, want) {
				t.Errorf("the page lacks %s:\n%s", want, text(page))
			}
		}
	})

	t.Run("two plants due the same day beyond the week are both named", func(t *testing.T) {
		f := rosewood(t)
		// Doris was last watered on 13 August, so a 33-day interval puts the next
		// watering on 15 September, the day Spike's is due.
		f.exec(t, "UPDATE care_schedule SET interval_count = 33 WHERE plant_id = $1", dorisID)
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = ANY($1)", []uuid.UUID{bigFellaID, nigelID, trailMixID, opuntiaID, sproutID})

		page := f.show(t)
		if want := "Doris and Spike are next, in 12 days."; !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, text(page))
		}
	})

	t.Run("a day with nothing scheduled shows the leaf rather than the tick", func(t *testing.T) {
		f := rosewood(t)
		clearTheWeek(t, f)

		page := f.show(t)
		for _, want := range []string{"Nothing due today", "Spike is next, in 12 days.", `href="/plants"`, "See all plants"} {
			if !strings.Contains(page, want) {
				t.Errorf("the page lacks %s:\n%s", want, text(page))
			}
		}
		if strings.Contains(page, "All done") {
			t.Error("a day with nothing scheduled says All done")
		}
	})

	t.Run("a garden with nothing scheduled at all has the heading and no line under it", func(t *testing.T) {
		f := rosewood(t)
		clearTheWeek(t, f)
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = $1", spikeID)

		page := f.show(t)
		if !strings.Contains(page, "Nothing due today") {
			t.Errorf("the page does not say nothing is due:\n%s", text(page))
		}
		if strings.Contains(page, "Nothing is due or overdue.") {
			t.Errorf("the page repeats the heading in a line under it:\n%s", text(page))
		}
		if strings.Contains(page, "is next") {
			t.Error("the page names a next plant when nothing is scheduled")
		}
	})

	t.Run("a garden with no plants shows an add plant link", func(t *testing.T) {
		f := rosewood(t)
		f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

		page := f.show(t)
		for _, want := range []string{"No plants yet", "Add a plant to see its tasks here.", `href="/plants/new"`, ">Add plant<"} {
			if !strings.Contains(page, want) {
				t.Errorf("the page lacks %s:\n%s", want, text(page))
			}
		}
	})

	// The link points at a route a sitter may not open.
	t.Run("a sitter in an empty garden gets no add plant link", func(t *testing.T) {
		f := rosewood(t)
		f.principal.Capabilities = auth.Capabilities{}
		f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

		page := f.show(t)
		if !strings.Contains(page, "No plants yet") {
			t.Errorf("the page does not say the garden is empty:\n%s", text(page))
		}
		if strings.Contains(page, newPlantPath) {
			t.Errorf("a sitter is offered the way to add a plant:\n%s", text(page))
		}
	})

	t.Run("an archived plant is 404", func(t *testing.T) {
		f := rosewood(t)
		f.exec(t, "UPDATE plant SET archived_at = now() WHERE garden_id = $1", rosewoodID)

		if page := f.show(t); !strings.Contains(page, "No plants yet") {
			t.Errorf("a garden of archived plants is not the first run:\n%s", text(page))
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
	if !strings.Contains(page, "Thursday 3 September") {
		t.Errorf("London's date is not Thursday 3 September:\n%s", text(page))
	}
	byID, _ := sections(page)
	if _, ok := rows(byID["coming-up"])[rowID(trailMixID)]; !ok {
		t.Error("in London on Thursday evening Trail Mix is not coming up")
	}

	f.principal.User.Timezone = "Pacific/Auckland"
	page = f.show(t)
	if !strings.Contains(page, "Friday 4 September") {
		t.Errorf("Auckland's date is not Friday 4 September:\n%s", text(page))
	}
	byID, _ = sections(page)
	if _, ok := rows(byID["due-today"])[rowID(trailMixID)]; !ok {
		t.Error("in Auckland on Friday morning Trail Mix is not due today")
	}
}

func TestToday_ThePageHeadingShowsTheDateAndTheGardenName(t *testing.T) {
	page := rosewood(t).show(t)
	for _, want := range []string{`<title>Today · sprig</title>`, "Thursday 3 September", `aria-current="page"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if !regexp.MustCompile(`<h1[^>]*>Rosewood</h1>`).MatchString(page) {
		t.Error("the garden's name is not the page's heading")
	}
}

// feed returns each line of the feed as markup, in page order.
func feed(t *testing.T, page string) []string {
	t.Helper()
	m := feedElement.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the page carries no feed:\n%s", page)
	}
	return feedLineElement.FindAllString(m[1], -1)
}

// says returns each feed line's text.
func says(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, text(line))
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
			if strings.Contains(line, "<form") {
				t.Errorf("a reader with no delete capability was offered %q", text(line))
			}
		}
	})

	t.Run("a member sees undo on their own care", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		line := feed(t, f.show(t))[1]
		if !strings.Contains(line, undoFormPath(nigelID, f.events(t, nigelID)[0].ID)) {
			t.Errorf("the reader's own watering carries no undo: %q", line)
		}
	})

	t.Run("nobody sees undo on somebody else's care", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		f.principal.Capabilities[auth.CareDeleteAny] = true
		if strings.Contains(feed(t, f.show(t))[0], undoFormPath(dorisID, f.events(t, dorisID)[1].ID)) {
			t.Errorf("an owner was offered Sam's watering back as an undo of their own")
		}
	})

	t.Run("nobody sees undo on a care recorded before the window", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		f.exec(t, "UPDATE care_event SET recorded_at = $1 WHERE plant_id = $2 AND performed_by = $3",
			thursday.Add(-undoWindow), nigelID, readerID)

		for _, line := range feed(t, f.show(t)) {
			if strings.Contains(line, "<form") {
				t.Errorf("a care recorded a window ago was offered back: %q", text(line))
			}
		}
	})
}

// The feed element has to be on the page for a swap to fill it. No whitespace
// is allowed inside the div because the stylesheet hides the feed with :empty.
func TestFeed_IsEmptyInAGardenWithNothingLogged(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)

	if page := f.show(t); !strings.Contains(page, `<div class="activity" id="activity"></div>`) {
		t.Errorf("the page draws no empty feed for a swap to land on:\n%s", page)
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

// switchLink matches the link after the garden's name in Today's top bar and
// captures its href.
var switchLink = regexp.MustCompile(`<a class="topbar__switch" href="([^"]+)" aria-label="Switch garden"`)

var gardenSheet = regexp.MustCompile(`(?s)<dialog open class="sheet" id="sheet" aria-labelledby="garden-sheet-title" tabindex="-1">(.*?)</dialog>`)

var (
	gardenRowName     = regexp.MustCompile(`(?s)<span class="row__name">(.*?)</span>\s*<span class="row__meta">`)
	gardenRowMeta     = regexp.MustCompile(`(?s)<span class="row__meta">(.*?)</span>\s*</span>`)
	gardenRowSwitch   = regexp.MustCompile(`<button class="row row--setting row--stack row--switch" type="submit">`)
	sheetItem         = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	gardenRowPosts    = regexp.MustCompile(`<input type="hidden" name="garden" value="([^"]+)">`)
	currentGardenMark = regexp.MustCompile(`<span class="row__you">Current</span>`)
)

type renderedGardenRow struct {
	name    string
	meta    string
	current bool
	// button is true when the row is a button that posts the switch. The
	// current garden's row is not one.
	button bool
	// posts is the garden id in the row's hidden input, and empty for the
	// current garden.
	posts string
}

// gardenSheetOf returns the rows of the open garden sheet, in page order. It
// returns nil when the page has no open sheet.
func gardenSheetOf(page string) []renderedGardenRow {
	sheet := gardenSheet.FindStringSubmatch(page)
	if sheet == nil {
		return nil
	}
	var out []renderedGardenRow
	for _, m := range sheetItem.FindAllStringSubmatch(sheet[1], -1) {
		row := renderedGardenRow{current: currentGardenMark.MatchString(m[1])}
		if name := gardenRowName.FindStringSubmatch(m[1]); name != nil {
			row.name = text(currentGardenMark.ReplaceAllString(name[1], ""))
		}
		if meta := gardenRowMeta.FindStringSubmatch(m[1]); meta != nil {
			row.meta = text(meta[1])
		}
		row.button = gardenRowSwitch.MatchString(m[1])
		if posts := gardenRowPosts.FindStringSubmatch(m[1]); posts != nil {
			row.posts = posts[1]
		}
		out = append(out, row)
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
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewGardenID)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Robin', 'robin', 'Europe/Lisbon')", robinUserID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", fairviewGardenID, robinUserID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour, expires_at) VALUES ($1, $2, 'sitter', 8, $3)",
		fairviewGardenID, readerID, expiresAt)
}

func TestToday_TheTopBarHasNoSwitchIconForAnAccountInOneGarden(t *testing.T) {
	page := rosewood(t).show(t)

	if m := switchLink.FindStringSubmatch(page); m != nil {
		t.Errorf("the top bar has a switch icon linking to %s, and Ellie is in one garden", m[1])
	}
}

func TestToday_TheSwitchIconOpensTheGardenSheetAndThePageLoadsWithoutIt(t *testing.T) {
	f := rosewood(t)
	f.joinFairview(t, nil)

	page := f.show(t)

	m := switchLink.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the top bar has no switch icon")
	}
	if m[1] != gardensPath {
		t.Errorf("the icon links to %s, want %s", m[1], gardensPath)
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
		{name: "Rosewood", meta: "Your garden · Owner", current: true},
		{name: "Fairview", meta: "Robin’s garden · Sitter · until 8 Sep", button: true, posts: fairviewGardenID.String()},
	}
	for _, hx := range []bool{false, true} {
		page := f.gardens(t, hx)
		if got := gardenSheetOf(page); !slices.Equal(got, want) {
			t.Errorf("with htmx = %v the sheet is\n%+v\nwant\n%+v", hx, got, want)
		}
		hasHeading := strings.Contains(page, "<h1")
		if hasHeading == hx {
			t.Errorf("with htmx = %v the response has a page heading = %v, want %v", hx, hasHeading, !hx)
		}
	}
}

func TestToday_AnEndedSecondMembershipPutsNoSwitchIconInTheTopBar(t *testing.T) {
	f := rosewood(t)
	ended := thursday.AddDate(0, 0, -1)
	f.joinFairview(t, &ended)

	if m := switchLink.FindStringSubmatch(f.show(t)); m != nil {
		t.Errorf("the top bar has a switch icon linking to %s, and the only other membership has ended", m[1])
	}
}

// Robin owns Fairview and Ellie is not in it. A query that stopped filtering
// on the signed-in account would list Fairview.
func TestToday_AnotherAccountsGardenIsNotInTheGardenSheet(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewGardenID)
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Robin', 'robin', 'Europe/Lisbon')", robinUserID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", fairviewGardenID, robinUserID)

	want := []renderedGardenRow{{name: "Rosewood", meta: "Your garden · Owner", current: true}}
	if got := gardenSheetOf(f.gardens(t, false)); !slices.Equal(got, want) {
		t.Errorf("the sheet is\n%+v\nwant\n%+v", got, want)
	}
}

// ownerLine matches the text under the garden's name in Today's top bar.
var ownerLine = regexp.MustCompile(`<div class="topbar__owner">(.*?)</div>`)

func TestToday_TheOwnersOwnGardenSaysNothingUnderItsName(t *testing.T) {
	page := rosewood(t).show(t)

	if m := ownerLine.FindStringSubmatch(page); m != nil {
		t.Errorf("the top bar says %q under the name, and Ellie owns Rosewood", m[1])
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

	m := ownerLine.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the top bar has no line under the garden's name")
	}
	if got := text(m[1]); got != "Robin’s garden" {
		t.Errorf("the line under the name is %q, want %q", got, "Robin’s garden")
	}
}

func TestToday_AGardenWithNoOwnerLeftSaysNothingUnderItsName(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "UPDATE membership SET role = 'member' WHERE garden_id = $1 AND user_id = $2", rosewoodID, readerID)
	f.principal.Membership.Role = "member"

	page := f.show(t)

	if m := ownerLine.FindStringSubmatch(page); m != nil {
		t.Errorf("the top bar says %q under the name, and nobody owns Rosewood", m[1])
	}
	if got := gardenSheetOf(f.gardens(t, false)); len(got) != 1 || got[0].meta != "Member" {
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

	if switchLink.FindStringSubmatch(page) == nil {
		t.Error("the sheet page has no switch icon in the top bar")
	}
	if m := ownerLine.FindStringSubmatch(page); m == nil || text(m[1]) != "Jo’s garden" {
		t.Errorf("the sheet page's line under the name is %v, want Jo’s garden", m)
	}
}

func TestToday_ARowShowsThePlantsPictureAsItsSquare(t *testing.T) {
	f := rosewood(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	page := f.show(t)

	if got, want := images(page), []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
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

			banner := bannerElement.FindString(page)
			if (banner != "") != c.rendered {
				t.Fatalf("the banner is rendered = %v, want %v:\n%s", banner != "", c.rendered, page)
			}
			if !c.rendered {
				return
			}
			// The banner is hidden until the script decides this browser
			// should see it.
			if !strings.Contains(banner, " hidden") {
				t.Errorf("the banner is not hidden, and the script has not run:\n%s", banner)
			}
			if !strings.Contains(banner, `data-key="`+testPushKey+`"`) {
				t.Errorf("the banner has no data-key for the browser to subscribe with:\n%s", banner)
			}
			if !strings.Contains(text(banner), "Turn on notifications") || !strings.Contains(text(banner), "Not now") {
				t.Errorf("the banner does not offer Turn on notifications and Not now:\n%s", text(banner))
			}
		})
	}
}
