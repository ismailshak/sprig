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

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

type rosterFixture struct {
	handler   *plants
	tx        pgx.Tx
	principal auth.Principal
}

// rosewoodRoster draws the roster over the same garden Today is tested on,
// which spreads seven plants across five rooms and leaves one unplaced. The
// principal gains PlantCreate because the button in the bar is drawn from it.
func rosewoodRoster(t *testing.T) *rosterFixture {
	t.Helper()

	f := rosewood(t)
	principal := f.principal
	principal.Capabilities = auth.Capabilities{auth.CareLog: true, auth.PlantCreate: true}
	return &rosterFixture{
		handler: &plants{
			logger:    testLogger,
			queries:   store.New(f.tx),
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
		tx:        f.tx,
		principal: principal,
	}
}

func (f *rosterFixture) show(t *testing.T) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, plantsPath, nil)
	rec := httptest.NewRecorder()
	f.handler.show(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *rosterFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tx.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

var (
	roomTitle        = regexp.MustCompile(`(?s)<h2 class="section__title[^"]*"[^>]*>(.*?)</h2>`)
	roomCount        = regexp.MustCompile(`<span class="section__count">(\d+)</span>`)
	rosterRowElement = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	rowLeadName      = regexp.MustCompile(`(?s)<span class="row__name([^"]*)">(.*?)</span>`)
	rowSecond        = regexp.MustCompile(`(?s)<span class="row__meta"><span class="row__part">(.*?)</span></span>`)
	rowStanding      = regexp.MustCompile(`(?s)<span class="standing">(.*?)</span>`)
	rowLinkStart     = regexp.MustCompile(`<a class="row row--link" href="([^"]+)">`)
)

type room struct {
	title string
	count string
	rows  []rosterTestRow
}

type rosterTestRow struct {
	href         string
	lead         string
	leadItalic   bool
	second       string
	secondItalic bool
	standing     string
}

// roomsOf reads the roster back out of the page in the order it draws it.
func roomsOf(t *testing.T, page string) []room {
	t.Helper()

	sectionsByID, order := sections(page)
	out := make([]room, 0, len(order))
	for _, id := range order {
		markup := sectionsByID[id]
		title := roomTitle.FindStringSubmatch(markup)
		count := roomCount.FindStringSubmatch(markup)
		if title == nil || count == nil {
			t.Fatalf("the section %s has no title or no count:\n%s", id, markup)
		}
		r := room{title: text(title[1]), count: count[1]}
		for _, m := range rosterRowElement.FindAllStringSubmatch(markup, -1) {
			r.rows = append(r.rows, rosterTestRowOf(t, m[1]))
		}
		out = append(out, r)
	}
	return out
}

func rosterTestRowOf(t *testing.T, markup string) rosterTestRow {
	t.Helper()

	link := rowLinkStart.FindStringSubmatch(markup)
	lead := rowLeadName.FindStringSubmatch(markup)
	if link == nil || lead == nil {
		t.Fatalf("the row is not a link with a name:\n%s", markup)
	}
	row := rosterTestRow{href: link[1], lead: text(lead[2]), leadItalic: strings.Contains(lead[1], "row__name--sp")}
	if m := rowSecond.FindStringSubmatch(markup); m != nil {
		row.second = text(m[1])
		row.secondItalic = strings.Contains(m[1], "<i>")
	}
	if m := rowStanding.FindStringSubmatch(markup); m != nil {
		row.standing = text(m[1])
	}
	return row
}

func leadNames(rows []rosterTestRow) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.lead)
	}
	return names
}

func TestPlants_GroupsTheRosterByRoomWithTheUnplacedLast(t *testing.T) {
	rooms := roomsOf(t, rosewoodRoster(t).show(t))

	want := []struct {
		title  string
		plants []string
	}{
		{"Bathroom", []string{"Nigel"}},
		{"Bedroom", []string{"Doris"}},
		{"Kitchen", []string{"Trail Mix"}},
		{"Living room", []string{"Big Fella"}},
		{"Windowsill", []string{"Opuntia microdasys", "Spike"}},
		{"No room", []string{"Sprout"}},
	}
	if len(rooms) != len(want) {
		t.Fatalf("the roster draws %d rooms, want %d", len(rooms), len(want))
	}
	for i, w := range want {
		got := rooms[i]
		if got.title != w.title {
			t.Errorf("room %d is %q, want %q", i, got.title, w.title)
		}
		if names := leadNames(got.rows); strings.Join(names, ", ") != strings.Join(w.plants, ", ") {
			t.Errorf("%s holds %v, want %v", w.title, names, w.plants)
		}
		if want := strconv.Itoa(len(w.plants)); got.count != want {
			t.Errorf("%s counts %s, want %s", w.title, got.count, want)
		}
	}
}

// The test writes names in a case the fixture does not use because
// compareNames, rather than the query's ORDER BY, decides how case sorts.
func TestPlants_OrderingIgnoresCase(t *testing.T) {
	f := rosewoodRoster(t)
	f.exec(t, "INSERT INTO plant (garden_id, nickname, location) VALUES ($1, 'aloe', 'Windowsill'), ($1, 'Kev', 'attic')", rosewoodID)

	rooms := roomsOf(t, f.show(t))
	titles := make([]string, 0, len(rooms))
	for _, r := range rooms {
		titles = append(titles, r.title)
	}
	if want := []string{"attic", "Bathroom", "Bedroom", "Kitchen", "Living room", "Windowsill", "No room"}; !slices.Equal(titles, want) {
		t.Errorf("the rooms are %v, want %v", titles, want)
	}
	for _, r := range rooms {
		if r.title != "Windowsill" {
			continue
		}
		if got, want := leadNames(r.rows), []string{"aloe", "Opuntia microdasys", "Spike"}; !slices.Equal(got, want) {
			t.Errorf("Windowsill holds %v, want %v", got, want)
		}
	}
}

func TestPlants_ARowOpensThePlantItNames(t *testing.T) {
	rooms := roomsOf(t, rosewoodRoster(t).show(t))

	for _, r := range rooms {
		if r.title != "Living room" {
			continue
		}
		if got, want := r.rows[0].href, plantPath(bigFellaID); got != want {
			t.Errorf("Big Fella's row goes to %q, want %q", got, want)
		}
		return
	}
	t.Fatal("the roster has no Living room")
}

func TestPlants_ARowCarriesTheNameItsFirstLineDidNotUse(t *testing.T) {
	rooms := roomsOf(t, rosewoodRoster(t).show(t))

	byName := map[string]rosterTestRow{}
	for _, r := range rooms {
		for _, row := range r.rows {
			byName[row.lead] = row
		}
	}

	want := map[string]struct {
		second       string
		leadItalic   bool
		secondItalic bool
	}{
		// Big Fella has three names, of which the roster draws the nickname
		// and the common one.
		"Big Fella": {second: "Swiss cheese plant"},
		// Opuntia microdasys has only a botanical name, which leads in italic.
		"Opuntia microdasys": {leadItalic: true},
		// Sprout has a nickname and no other name.
		"Sprout": {},
	}
	for name, w := range want {
		row, ok := byName[name]
		if !ok {
			t.Errorf("the roster has no row for %s", name)
			continue
		}
		if row.second != w.second {
			t.Errorf("%s reads %q on its second line, want %q", name, row.second, w.second)
		}
		if row.leadItalic != w.leadItalic {
			t.Errorf("%s leads in italic = %v, want %v", name, row.leadItalic, w.leadItalic)
		}
		if row.secondItalic != w.secondItalic {
			t.Errorf("%s carries its second name in italic = %v, want %v", name, row.secondItalic, w.secondItalic)
		}
	}
}

func TestPlants_OnlyAnOverduePlantSaysWhereItStands(t *testing.T) {
	rooms := roomsOf(t, rosewoodRoster(t).show(t))

	standing := map[string]string{}
	for _, r := range rooms {
		for _, row := range r.rows {
			if row.standing != "" {
				standing[row.lead] = row.standing
			}
		}
	}

	// Doris and Nigel are due today, which the roster leaves to Today.
	if want := map[string]string{"Big Fella": "Water 2 days late"}; len(standing) != len(want) || standing["Big Fella"] != want["Big Fella"] {
		t.Errorf("the roster's standings are %v, want %v", standing, want)
	}
}

func TestPlants_TheMostOverdueCareSpeaksForThePlant(t *testing.T) {
	f := rosewoodRoster(t)
	// The schedule puts a feeding a fortnight late on a plant already two days
	// late for water.
	f.exec(t, "INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit, set_at) VALUES ($1, $2, $3, 1, 'week', $4)",
		rosewoodID, bigFellaID, feedID, day(time.August, 13))

	rooms := roomsOf(t, f.show(t))
	for _, r := range rooms {
		if r.title != "Living room" {
			continue
		}
		if got, want := r.rows[0].standing, "Feed 14 days late"; got != want {
			t.Errorf("Big Fella stands at %q, want %q", got, want)
		}
		return
	}
	t.Fatal("the roster has no Living room")
}

func TestPlants_ASitterIsOfferedNoWayToAddOne(t *testing.T) {
	f := rosewoodRoster(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	if page := f.show(t); strings.Contains(page, "/plants/new") {
		t.Errorf("a sitter was offered the add form:\n%s", text(page))
	}
}

func TestPlants_AMemberWhoMayAddOneIsOfferedIt(t *testing.T) {
	if page := rosewoodRoster(t).show(t); !strings.Contains(page, `href="/plants/new"`) {
		t.Errorf("the roster carries no Add:\n%s", text(page))
	}
}

// empty leaves the garden with no plants, which is the state a new garden
// starts in.
func (f *rosterFixture) empty(t *testing.T) {
	t.Helper()
	f.exec(t, "DELETE FROM care_event")
	f.exec(t, "DELETE FROM care_schedule")
	f.exec(t, "DELETE FROM plant")
}

func TestPlants_AGardenWithNoPlantsIsAskedForItsFirst(t *testing.T) {
	f := rosewoodRoster(t)
	f.empty(t)

	page := f.show(t)
	if _, order := sections(page); len(order) > 0 {
		t.Errorf("an empty garden draws the rooms %v", order)
	}
	for _, want := range []string{"No plants yet", "Add one and sprig will tell you when it needs water.", "Add a plant"} {
		if !strings.Contains(text(page), want) {
			t.Errorf("the empty roster does not say %q:\n%s", want, text(page))
		}
	}
}

func TestPlants_ASitterInAnEmptyGardenIsAskedForNothing(t *testing.T) {
	f := rosewoodRoster(t)
	f.empty(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	page := f.show(t)
	if !strings.Contains(text(page), "No plants yet") {
		t.Errorf("the empty roster says nothing:\n%s", text(page))
	}
	if strings.Contains(page, "/plants/new") {
		t.Errorf("a sitter was offered the add form:\n%s", text(page))
	}
}
