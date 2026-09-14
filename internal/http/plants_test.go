package http

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	mustExec(t, f.tx, sql, args...)
}

type room struct {
	title string
	count string
	rows  []plantsTestRow
}

// plantsTestRow is one row on the Plants page.
type plantsTestRow struct {
	// href is the URL the row links to.
	href string
	// text is everything the row says: the plant's name, its second line and
	// the overdue care, in that order.
	text string
}

// roomsOf reads the rooms and their plants back out of the page in page order.
// A room is a section named by its heading. Its count is the first word of
// the section's text after the title.
func roomsOf(t *testing.T, page string) []room {
	t.Helper()

	doc := readHTML(page)
	var out []room
	for _, section := range doc.all(isTag("section"), hasAttr("aria-labelledby")) {
		title := doc.byID(section.attr("aria-labelledby")).text()
		after := strings.Fields(strings.TrimPrefix(section.text(), title))
		if title == "" || len(after) == 0 {
			t.Fatalf("the section has no title or no count:\n%s", section)
		}
		r := room{title: title, count: after[0]}
		for _, item := range section.all(isTag("li")) {
			link := item.first(isTag("a"))
			if link == nil {
				t.Fatalf("the row is not a link:\n%s", item)
			}
			r.rows = append(r.rows, plantsTestRow{href: link.attr("href"), text: item.text()})
		}
		out = append(out, r)
	}
	return out
}

func rowTexts(rows []plantsTestRow) []string {
	texts := make([]string, 0, len(rows))
	for _, r := range rows {
		texts = append(texts, r.text)
	}
	return texts
}

// rowsStartWith reports whether each row's text starts with the name at the
// same place in names.
func rowsStartWith(rows []plantsTestRow, names []string) bool {
	if len(rows) != len(names) {
		return false
	}
	for i, r := range rows {
		if r.text != names[i] && !strings.HasPrefix(r.text, names[i]+" ") {
			return false
		}
	}
	return true
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
		t.Fatalf("the plant list shows %d rooms, want %d", len(rooms), len(want))
	}
	for i, w := range want {
		got := rooms[i]
		if got.title != w.title {
			t.Errorf("room %d is %q, want %q", i, got.title, w.title)
		}
		if !rowsStartWith(got.rows, w.plants) {
			t.Errorf("%s holds %v, want %v", w.title, rowTexts(got.rows), w.plants)
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
		if want := []string{"aloe", "Opuntia microdasys", "Spike"}; !rowsStartWith(r.rows, want) {
			t.Errorf("Windowsill holds %v, want %v", rowTexts(r.rows), want)
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

	var rows []plantsTestRow
	for _, r := range rooms {
		rows = append(rows, r.rows...)
	}

	want := map[string]string{
		// Big Fella has three names. The list shows the nickname and the common
		// name, then the watering it is late for.
		"Big Fella": "Big Fella Swiss cheese plant Water 2 days late",
		// Opuntia microdasys has only a botanical name. Its row has no second
		// line.
		"Opuntia microdasys": "Opuntia microdasys",
		// Sprout has a nickname and no other name.
		"Sprout": "Sprout",
	}
	for name, w := range want {
		i := slices.IndexFunc(rows, func(r plantsTestRow) bool { return rowsStartWith([]plantsTestRow{r}, []string{name}) })
		if i < 0 {
			t.Errorf("the plant list has no row for %s", name)
			continue
		}
		if rows[i].text != w {
			t.Errorf("%s's row reads %q, want %q", name, rows[i].text, w)
		}
	}
}

// Doris and Nigel are due today. The Plants page does not mention that, since
// Today does.
func TestPlants_OnlyAnOverduePlantShowsAStatusLine(t *testing.T) {
	rooms := roomsOf(t, rosewoodPlants(t).show(t))

	var got []string
	for _, r := range rooms {
		got = append(got, rowTexts(r.rows)...)
	}

	want := []string{
		"Nigel Boston fern",
		"Doris Snake plant",
		"Trail Mix Golden pothos",
		"Big Fella Swiss cheese plant Water 2 days late",
		"Opuntia microdasys",
		"Spike Golden barrel cactus",
		"Sprout",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the plant list's rows read\n%q\nwant\n%q", got, want)
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
		if got, want := r.rows[0].text, "Big Fella Swiss cheese plant Feed 14 days late"; got != want {
			t.Errorf("Big Fella's row reads %q, want %q", got, want)
		}
		return
	}
	t.Fatal("the plant list has no Living room")
}

// addLink returns the link on the page to the add form, or nil.
func addLink(page string) *element {
	return readHTML(page).first(isTag("a"), attrIs("href", newPlantPath))
}

func TestPlants_ASitterSeesNoAddButton(t *testing.T) {
	f := rosewoodPlants(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	if page := f.show(t); addLink(page) != nil {
		t.Errorf("a sitter was offered the add form:\n%s", text(page))
	}
}

func TestPlants_AMemberWhoMayCreatePlantsSeesTheAddButton(t *testing.T) {
	if page := rosewoodPlants(t).show(t); addLink(page) == nil {
		t.Errorf("the plant list has no Add:\n%s", text(page))
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
	if rooms := roomsOf(t, page); len(rooms) > 0 {
		t.Errorf("an empty garden shows %d rooms", len(rooms))
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
	if addLink(page) != nil {
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
