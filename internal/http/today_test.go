package http

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	rosewoodID = uuid.MustParse("00000000-0000-7000-8000-000000000101")
	readerID   = uuid.MustParse("00000000-0000-7000-8000-000000000102")
	waterID    = uuid.MustParse("00000000-0000-7000-8000-000000000103")
	feedID     = uuid.MustParse("00000000-0000-7000-8000-000000000104")

	bigFellaID = uuid.MustParse("00000000-0000-7000-8000-000000000111")
	dorisID    = uuid.MustParse("00000000-0000-7000-8000-000000000112")
	nigelID    = uuid.MustParse("00000000-0000-7000-8000-000000000113")
	trailMixID = uuid.MustParse("00000000-0000-7000-8000-000000000114")
	opuntiaID  = uuid.MustParse("00000000-0000-7000-8000-000000000115")
	sproutID   = uuid.MustParse("00000000-0000-7000-8000-000000000116")
	spikeID    = uuid.MustParse("00000000-0000-7000-8000-000000000117")
)

// A Thursday morning in London. The garden below is written against it, so
// every plant lands in the same section on every run.
var thursday = time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)

type todayFixture struct {
	handler   *today
	tx        pgx.Tx
	principal auth.Principal
}

// rosewood seeds a garden that fills every section, one plant overdue, two
// due today, three coming up, and one beyond the week for the empty state to
// name. Every cadence is a watering last performed at noon in London.
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
	exec("INSERT INTO membership (garden_id, user_id, role) VALUES ($1, $2, 'owner')", rosewoodID, readerID)
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
	// Nigel gets a second care because the sheet draws What only for a plant
	// with more than one. The set_at puts the feed a week out so his row stays
	// a watering.
	exec("INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 3, 'week', $4)",
		rosewoodID, nigelID, feedID, day(time.August, 20))

	queries := store.New(tx)
	principal := auth.Principal{
		User:         store.AppUser{ID: readerID, DisplayName: "Ellie", Handle: "ellie", Timezone: "Europe/London"},
		Garden:       store.Garden{ID: rosewoodID, Name: "Rosewood"},
		Membership:   store.Membership{Role: "owner"},
		Capabilities: auth.Capabilities{auth.CareLog: true},
	}
	handler := &today{logger: testLogger, queries: queries, templates: testTemplates(), now: func() time.Time { return thursday }}
	return &todayFixture{handler: handler, tx: tx, principal: principal}
}

// day is noon in London, so a watering and the day it counts towards share a
// date in London and in UTC alike.
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
	// The feed's close is the one </div> after a newline because a line opens
	// and closes on one line.
	feedElement     = regexp.MustCompile(`(?s)<div[^>]*id="activity"[^>]*>(.*?)\n</div>`)
	feedLineElement = regexp.MustCompile(`(?s)<div>.*?</div>`)
	sectionHead     = regexp.MustCompile(`^(?:Overdue|Due today|Coming up) (\d+)\b`)
	tag             = regexp.MustCompile(`<[^>]+>`)
	spaces          = regexp.MustCompile(`\s+`)
)

// text is what a reader sees in a piece of markup, with the tags gone and
// the whitespace collapsed, so a test asserts the words and not the spans
// around them.
func text(markup string) string {
	s := html.UnescapeString(tag.ReplaceAllString(markup, " "))
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// sections returns each section's markup by the id of its heading, and the
// order the page draws them in.
func sections(page string) (map[string]string, []string) {
	byID := map[string]string{}
	var order []string
	for _, m := range sectionElement.FindAllStringSubmatch(page, -1) {
		byID[m[1]] = m[0]
		order = append(order, m[1])
	}
	return byID, order
}

// rows returns what each row of a section says, by the row's id.
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
	if !strings.Contains(text(page), "3 plants need you today, 1 of them overdue.") {
		t.Errorf("the summary does not say three plants need attention and one is overdue:\n%s", text(page))
	}
}

func TestToday_ARowSaysOnlyWhatIsUsefulInTheMoment(t *testing.T) {
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
		{"an overdue row gives how late it is", bigFellaID, "Big Fella Living room · 2 days late Water"},
		{"a row due today gives the location and nothing else", dorisID, "Doris Bedroom Water"},
		{"a coming-up row gives the day and offers Log", trailMixID, "Trail Mix Kitchen · Water tomorrow Log"},
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

func TestToday_TheCountsFallAsRowsAreDealtWith(t *testing.T) {
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
	if !strings.Contains(text(page), "2 plants need you today, 1 of them overdue.") {
		t.Errorf("the summary did not fall to two:\n%s", text(page))
	}

	f.water(t, nigelID)
	page = f.show(t)
	byID, order := sections(page)
	if _, ok := byID["due-today"]; ok {
		t.Errorf("Due today is still drawn with nothing in it: %v", order)
	}
	if !strings.Contains(text(page), "1 plant needs you today, 1 of them overdue.") {
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
	// Coming up already says what is next.
	if strings.Contains(page, "is next") || strings.Contains(page, "See all plants") {
		t.Error("the empty state names what is next over a Coming up list that names it too")
	}
}

func TestToday_AnEmptyListIsThreeDifferentPiecesOfNews(t *testing.T) {
	clearTheWeek := func(t *testing.T, f *todayFixture) {
		t.Helper()
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = ANY($1)", []uuid.UUID{bigFellaID, dorisID, nigelID, trailMixID, opuntiaID, sproutID})
	}

	t.Run("a day finished with nothing coming up is done, and names what is next", func(t *testing.T) {
		f := rosewood(t)
		for _, plant := range []uuid.UUID{bigFellaID, dorisID, nigelID} {
			f.water(t, plant)
		}
		// Watering Nigel would put his four-day cadence back into Coming up.
		clearTheWeek(t, f)

		page := f.show(t)
		for _, want := range []string{"All done for today", "Spike is next, in 12 days.", `href="/plants"`, "See all plants"} {
			if !strings.Contains(page, want) {
				t.Errorf("the page lacks %s:\n%s", want, text(page))
			}
		}
	})

	t.Run("a day with nothing scheduled is not congratulated", func(t *testing.T) {
		f := rosewood(t)
		clearTheWeek(t, f)

		page := f.show(t)
		for _, want := range []string{"Nothing needs you today", "Spike is next, in 12 days.", `href="/plants"`, "See all plants"} {
			if !strings.Contains(page, want) {
				t.Errorf("the page lacks %s:\n%s", want, text(page))
			}
		}
		if strings.Contains(page, "All done") {
			t.Error("a day where nothing was scheduled was congratulated")
		}
	})

	t.Run("a garden with nothing scheduled at all says so", func(t *testing.T) {
		f := rosewood(t)
		clearTheWeek(t, f)
		f.exec(t, "DELETE FROM care_schedule WHERE plant_id = $1", spikeID)

		page := f.show(t)
		if !strings.Contains(page, "Nothing is due, and nothing is overdue.") {
			t.Errorf("the page does not say nothing is due:\n%s", text(page))
		}
		if strings.Contains(page, "is next") {
			t.Error("the page names a next plant when nothing is scheduled")
		}
	})

	t.Run("a garden with no plants offers the one action that starts the app", func(t *testing.T) {
		f := rosewood(t)
		f.exec(t, "DELETE FROM plant WHERE garden_id = $1", rosewoodID)

		page := f.show(t)
		for _, want := range []string{"No plants yet", "Add one and sprig will tell you when it needs water.", `href="/plants/new"`, "Add a plant"} {
			if !strings.Contains(page, want) {
				t.Errorf("the page lacks %s:\n%s", want, text(page))
			}
		}
	})

	t.Run("an archived plant is no plant", func(t *testing.T) {
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

func TestToday_TheDateAndTheGardenHeadThePage(t *testing.T) {
	page := rosewood(t).show(t)
	for _, want := range []string{`<title>sprig — today</title>`, "Thursday 3 September", `aria-current="page"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
	if !regexp.MustCompile(`<h1[^>]*>Rosewood</h1>`).MatchString(page) {
		t.Error("the garden's name is not the page's heading")
	}
}

// feed is each line of the feed as markup, in drawn order.
func feed(t *testing.T, page string) []string {
	t.Helper()
	m := feedElement.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the page carries no feed:\n%s", page)
	}
	return feedLineElement.FindAllString(m[1], -1)
}

// says is what each line of the feed reads as.
func says(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, text(line))
	}
	return out
}

func TestFeed_NamesWhatWasRecordedNewestFirst(t *testing.T) {
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

func TestFeed_NamesThePersonWhoIsNotTheReader(t *testing.T) {
	f := rosewood(t)
	sam := uuid.MustParse("00000000-0000-7000-8000-000000000198")
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", sam)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
		rosewoodID, dorisID, feedID, sam, thursday.Add(-time.Hour))

	if got := says(feed(t, f.show(t)))[0]; got != "Sam fed Doris · today, 8:00am" {
		t.Errorf("the feed's newest line reads %q, want Sam named as the one who fed Doris", got)
	}
}

func TestFeed_SaysASkipWasASkip(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done, override_interval_days) VALUES ($1, $2, $3, $4, $5, $5, false, 2)",
		rosewoodID, dorisID, waterID, readerID, thursday)

	if got := says(feed(t, f.show(t)))[0]; got != "You skipped Doris · today, 9:00am" {
		t.Errorf("the feed's newest line reads %q, want the skip said as a skip", got)
	}
}

func TestFeed_OffersUndoOnlyOnTheReadersOwnCareInsideItsWindow(t *testing.T) {
	sam := uuid.MustParse("00000000-0000-7000-8000-000000000198")
	// Sam's watering of Doris is lines[0] and the reader's of Nigel is
	// lines[1], both recorded a moment ago. Every seeded event behind them is
	// days old.
	setup := func(t *testing.T) *todayFixture {
		f := rosewood(t)
		f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Sam', 'sam', 'Europe/London')", sam)
		f.exec(t, "INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, recorded_at, done) VALUES ($1, $2, $3, $4, $5, $5, true)",
			rosewoodID, dorisID, waterID, sam, thursday)
		f.exec(t, "UPDATE care_event SET recorded_at = $1 WHERE plant_id = $2 AND performed_by = $3", thursday.Add(-time.Second), nigelID, readerID)
		return f
	}

	t.Run("a sitter who may delete nothing is offered no button", func(t *testing.T) {
		f := setup(t)
		for _, line := range feed(t, f.show(t)) {
			if strings.Contains(line, "<form") {
				t.Errorf("a reader with no delete capability was offered %q", text(line))
			}
		}
	})

	t.Run("a member is offered it on their own", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		line := feed(t, f.show(t))[1]
		if !strings.Contains(line, undoFormPath(nigelID, f.events(t, nigelID)[0].ID)) {
			t.Errorf("the reader's own watering carries no undo: %q", line)
		}
	})

	t.Run("nobody is offered it on somebody else's", func(t *testing.T) {
		f := setup(t)
		f.principal.Capabilities[auth.CareDeleteOwn] = true
		f.principal.Capabilities[auth.CareDeleteAny] = true
		if strings.Contains(feed(t, f.show(t))[0], undoFormPath(dorisID, f.events(t, dorisID)[1].ID)) {
			t.Errorf("an owner was offered Sam's watering back as an undo of their own")
		}
	})

	t.Run("nobody is offered it on a care recorded before the window", func(t *testing.T) {
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

// A swap needs the element on the page before there is anything to put in it.
// The assertion allows no whitespace inside the div because the stylesheet
// hides the feed on :empty.
func TestFeed_IsDrawnEmptyInAGardenWithNothingRecorded(t *testing.T) {
	f := rosewood(t)
	f.exec(t, "DELETE FROM care_event WHERE garden_id = $1", rosewoodID)

	if page := f.show(t); !strings.Contains(page, `<div class="activity" id="activity"></div>`) {
		t.Errorf("the page draws no empty feed for a swap to land on:\n%s", page)
	}
}
