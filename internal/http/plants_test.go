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

type plantsFixture struct {
	handler   *plants
	tx        pgx.Tx
	principal auth.Principal
}

// rosewoodPlants sets up the Plants page on the garden Today is tested on,
// which has seven plants across five rooms and one with no room. The principal
// is given PlantCreate because the Add button depends on it.
func rosewoodPlants(t *testing.T) *plantsFixture {
	t.Helper()

	f := rosewood(t)
	principal := f.principal
	principal.Capabilities = auth.Capabilities{auth.CareLog: true, auth.PlantCreate: true}
	return &plantsFixture{
		handler: &plants{
			logger:    testLogger,
			queries:   store.New(f.tx),
			photos:    testPhotos(t),
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
		tx:        f.tx,
		principal: principal,
	}
}

func (f *plantsFixture) show(t *testing.T) string {
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

func (f *plantsFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tx.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

var (
	roomTitle       = regexp.MustCompile(`(?s)<h2 class="section__title[^"]*"[^>]*>(.*?)</h2>`)
	roomCount       = regexp.MustCompile(`<span class="section__count">(\d+)</span>`)
	plantRowElement = regexp.MustCompile(`(?s)<li>(.*?)</li>`)
	rowLeadName     = regexp.MustCompile(`(?s)<span class="row__name([^"]*)">(.*?)</span>`)
	rowSecond       = regexp.MustCompile(`(?s)<span class="row__meta"><span class="row__part">(.*?)</span></span>`)
	rowStanding     = regexp.MustCompile(`(?s)<span class="standing">(.*?)</span>`)
	rowLinkStart    = regexp.MustCompile(`<a class="row row--link" href="([^"]+)">`)
)

type room struct {
	title string
	count string
	rows  []plantsTestRow
}

type plantsTestRow struct {
	href         string
	lead         string
	leadItalic   bool
	second       string
	secondItalic bool
	standing     string
}

// roomsOf reads the rooms and their plants back out of the page in page order.
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
		for _, m := range plantRowElement.FindAllStringSubmatch(markup, -1) {
			r.rows = append(r.rows, plantsTestRowOf(t, m[1]))
		}
		out = append(out, r)
	}
	return out
}

func plantsTestRowOf(t *testing.T, markup string) plantsTestRow {
	t.Helper()

	link := rowLinkStart.FindStringSubmatch(markup)
	lead := rowLeadName.FindStringSubmatch(markup)
	if link == nil || lead == nil {
		t.Fatalf("the row is not a link with a name:\n%s", markup)
	}
	row := plantsTestRow{href: link[1], lead: text(lead[2]), leadItalic: strings.Contains(lead[1], "row__name--sp")}
	if m := rowSecond.FindStringSubmatch(markup); m != nil {
		row.second = text(m[1])
		row.secondItalic = strings.Contains(m[1], "<i>")
	}
	if m := rowStanding.FindStringSubmatch(markup); m != nil {
		row.standing = text(m[1])
	}
	return row
}

func leadNames(rows []plantsTestRow) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.lead)
	}
	return names
}

func TestPlants_GroupsPlantsByRoomWithNoRoomLast(t *testing.T) {
	rooms := roomsOf(t, rosewoodPlants(t).show(t))

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
		t.Fatalf("the plant list draws %d rooms, want %d", len(rooms), len(want))
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

// The names are written in a case the fixture does not use, because
// compareNames rather than the query's ORDER BY decides how case sorts.
func TestPlants_OrderingIgnoresCase(t *testing.T) {
	f := rosewoodPlants(t)
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

func TestPlants_ARowLinksToItsPlant(t *testing.T) {
	rooms := roomsOf(t, rosewoodPlants(t).show(t))

	for _, r := range rooms {
		if r.title != "Living room" {
			continue
		}
		if got, want := r.rows[0].href, plantPath(bigFellaID); got != want {
			t.Errorf("Big Fella's row goes to %q, want %q", got, want)
		}
		return
	}
	t.Fatal("the plant list has no Living room")
}

func TestPlants_ARowShowsThePlantsOtherNameOnASecondLine(t *testing.T) {
	rooms := roomsOf(t, rosewoodPlants(t).show(t))

	byName := map[string]plantsTestRow{}
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
		// Big Fella has three names. The list shows the nickname and the common
		// name.
		"Big Fella": {second: "Swiss cheese plant"},
		// Opuntia microdasys has only a botanical name, shown in italics.
		"Opuntia microdasys": {leadItalic: true},
		// Sprout has a nickname and no other name.
		"Sprout": {},
	}
	for name, w := range want {
		row, ok := byName[name]
		if !ok {
			t.Errorf("the plant list has no row for %s", name)
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

func TestPlants_OnlyAnOverduePlantShowsAStatusLine(t *testing.T) {
	rooms := roomsOf(t, rosewoodPlants(t).show(t))

	standing := map[string]string{}
	for _, r := range rooms {
		for _, row := range r.rows {
			if row.standing != "" {
				standing[row.lead] = row.standing
			}
		}
	}

	// Doris and Nigel are due today. The Plants page does not mention that,
	// since Today does.
	if want := map[string]string{"Big Fella": "Water 2 days late"}; len(standing) != len(want) || standing["Big Fella"] != want["Big Fella"] {
		t.Errorf("the plant list's standings are %v, want %v", standing, want)
	}
}

func TestPlants_TheStatusLineNamesTheMostOverdueCare(t *testing.T) {
	f := rosewoodPlants(t)
	// The plant is already two days late for water. This adds a feeding two
	// weeks late.
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
	t.Fatal("the plant list has no Living room")
}

func TestPlants_ASitterSeesNoAddButton(t *testing.T) {
	f := rosewoodPlants(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	if page := f.show(t); strings.Contains(page, "/plants/new") {
		t.Errorf("a sitter was offered the add form:\n%s", text(page))
	}
}

func TestPlants_AMemberWhoMayCreatePlantsSeesTheAddButton(t *testing.T) {
	if page := rosewoodPlants(t).show(t); !strings.Contains(page, `href="/plants/new"`) {
		t.Errorf("the plant list carries no Add:\n%s", text(page))
	}
}

// empty deletes every plant, leaving the garden as a new one starts.
func (f *plantsFixture) empty(t *testing.T) {
	t.Helper()
	f.exec(t, "DELETE FROM care_event")
	f.exec(t, "DELETE FROM care_schedule")
	f.exec(t, "DELETE FROM plant")
}

func TestPlants_AGardenWithNoPlantsShowsAnAddPlantLink(t *testing.T) {
	f := rosewoodPlants(t)
	f.empty(t)

	page := f.show(t)
	if _, order := sections(page); len(order) > 0 {
		t.Errorf("an empty garden draws the rooms %v", order)
	}
	for _, want := range []string{"No plants yet", "Add a plant to see its tasks here.", "Add plant"} {
		if !strings.Contains(text(page), want) {
			t.Errorf("the empty plant list does not say %q:\n%s", want, text(page))
		}
	}
}

func TestPlants_ASitterInAnEmptyGardenSeesNoAddPlantLink(t *testing.T) {
	f := rosewoodPlants(t)
	f.empty(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	page := f.show(t)
	if !strings.Contains(text(page), "No plants yet") {
		t.Errorf("the empty plant list says nothing:\n%s", text(page))
	}
	if strings.Contains(page, "/plants/new") {
		t.Errorf("a sitter was offered the add form:\n%s", text(page))
	}
}

func TestPlants_ARowShowsThePlantsPictureAsItsSquare(t *testing.T) {
	f := rosewoodPlants(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	page := f.show(t)

	if got, want := images(page), []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the list's images are %v, want Big Fella's square at %v and none on the other six rows", got, want)
	}
}
