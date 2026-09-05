package http

import (
	"context"
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

type formFixture struct {
	*plantFixture
}

// plantFormOn sets up the add and edit forms on the garden the plant page is
// tested on, with a third care type so the schedule list includes a care no
// plant here is scheduled for.
func plantFormOn(t *testing.T) *formFixture {
	t.Helper()

	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Repot', 'repot')", repotID, rosewoodID)
	return &formFixture{plantFixture: f}
}

func (f *formFixture) open(t *testing.T, path string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	f.handler.newPlant(rec, req)
	return rec
}

func (f *formFixture) add(t *testing.T, values url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, newPlantPath, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.handler.create(rec, req)
	return rec
}

func (f *formFixture) editForm(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, editPlantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.edit(rec, req)
	return rec
}

func (f *formFixture) save(t *testing.T, plantID uuid.UUID, values url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, editPlantPath(plantID), strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.update(rec, req)
	return rec
}

func (f *formFixture) archive(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, archivePlantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.archive(rec, req)
	return rec
}

// ask GETs the archive route, which renders the confirmation rather than
// archiving. An empty target makes the request a page navigation rather than an
// htmx swap.
func (f *formFixture) ask(t *testing.T, plantID uuid.UUID, target string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, archivePlantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	if target != "" {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", target)
	}
	rec := httptest.NewRecorder()
	f.handler.confirmArchive(rec, req)
	return rec
}

// keep requests the plant's page the way the Keep it link does, which renders
// the buttons at the bottom closed again.
func (f *formFixture) keep(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, plantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", plantFootID)
	rec := httptest.NewRecorder()
	f.handler.plant(rec, req)
	return rec
}

// created returns the plant a successful post redirected to.
func (f *formFixture) created(t *testing.T, rec *httptest.ResponseRecorder) store.Plant {
	t.Helper()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	id, err := uuid.Parse(strings.TrimPrefix(rec.Header().Get("Location"), plantsPath+"/"))
	if err != nil {
		t.Fatalf("the post went to %q, which names no plant", rec.Header().Get("Location"))
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, id)
	if err != nil {
		t.Fatalf("reading the plant the post created: %v", err)
	}
	return plant
}

// scheduleFor returns the plant's schedule for one care type.
func (f *formFixture) scheduleFor(t *testing.T, plantID uuid.UUID, slug string) store.CareSchedule {
	t.Helper()

	for _, row := range f.schedulesOf(t, plantID) {
		if row.CareType.Slug == slug {
			return row.CareSchedule
		}
	}
	t.Fatalf("the plant has no %s schedule", slug)
	return store.CareSchedule{}
}

func (f *formFixture) schedulesOf(t *testing.T, plantID uuid.UUID) []store.ListCareSchedulesRow {
	t.Helper()

	all, err := store.New(f.tx).ListCareSchedules(t.Context(), rosewoodID)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.ListCareSchedulesRow
	for _, row := range all {
		if row.Plant.ID == plantID {
			out = append(out, row)
		}
	}
	return out
}

func (f *formFixture) countPlants(t *testing.T) int64 {
	t.Helper()

	n, err := store.New(f.tx).CountPlants(t.Context(), rosewoodID)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var (
	formInput  = regexp.MustCompile(`<input class="input[^"]*" id="([^"]+)"[^>]*value="([^"]*)"`)
	formArea   = regexp.MustCompile(`(?s)<textarea[^>]*id="([^"]+)"[^>]*>(.*?)</textarea>`)
	formSelect = regexp.MustCompile(`(?s)<select[^>]*id="([^"]+)"[^>]*>(.*?)</select>`)
	formPicked = regexp.MustCompile(`<option value="([^"]+)" selected>`)
	formError  = regexp.MustCompile(`<p class="field__error">(.*?)</p>`)
	formRow    = regexp.MustCompile(`<li class="sched sched--(editing|add)" id="sched-([a-z]+)"`)
	formTitle  = regexp.MustCompile(`<h1 class="topbar__title">(.*?)</h1>`)
)

// filled returns a field's value, whichever kind of control it is.
func filled(t *testing.T, page, id string) string {
	t.Helper()

	for _, pattern := range []*regexp.Regexp{formInput, formArea} {
		for _, m := range pattern.FindAllStringSubmatch(page, -1) {
			if m[1] == id {
				return text(m[2])
			}
		}
	}
	for _, m := range formSelect.FindAllStringSubmatch(page, -1) {
		if m[1] != id {
			continue
		}
		picked := formPicked.FindStringSubmatch(m[2])
		if picked == nil {
			t.Fatalf("the %s select has nothing selected", id)
		}
		return picked[1]
	}
	t.Fatalf("the form has no field called %s", id)
	return ""
}

func hasField(page, id string) bool {
	for _, pattern := range []*regexp.Regexp{formInput, formArea, formSelect} {
		for _, m := range pattern.FindAllStringSubmatch(page, -1) {
			if m[1] == id {
				return true
			}
		}
	}
	return false
}

// openRows returns the care types whose schedule rows are open, in page order.
func openRows(page string) []string {
	var open []string
	for _, m := range formRow.FindAllStringSubmatch(page, -1) {
		if m[1] == "editing" {
			open = append(open, m[2])
		}
	}
	return open
}

func closedRows(page string) []string {
	var closed []string
	for _, m := range formRow.FindAllStringSubmatch(page, -1) {
		if m[1] == "add" {
			closed = append(closed, m[2])
		}
	}
	return closed
}

func errorsOn(page string) []string {
	var lines []string
	for _, m := range formError.FindAllStringSubmatch(page, -1) {
		lines = append(lines, text(m[1]))
	}
	return lines
}

// addValues is the add form as a browser posts it, with every field the page
// renders and no schedule row open. A shape changed without JavaScript posts
// without that shape's field group, so a test that wants that leaves the group
// out on purpose.
func addValues() url.Values {
	values := url.Values{
		"nickname":       {""},
		"common":         {""},
		"botanical":      {""},
		"where":          {""},
		"notes":          {""},
		"acquired-month": {"0"},
		"acquired-year":  {"0"},
	}
	for _, ref := range referenceFields {
		values.Set(ref.name, "")
	}
	return values
}

// openValues adds one care type's open schedule row to a post, using the
// names of that row's controls.
func openValues(values url.Values, slug string, fields url.Values) url.Values {
	values.Add("open", slug)
	for name, held := range fields {
		values[slug+"-"+name] = held
	}
	return values
}

func cadenceValues(count, unit string) url.Values {
	return url.Values{"shape": {shapeCadence}, "every": {count}, "unit": {unit}}
}

func onceValues(day, month, year string) url.Values {
	return url.Values{"shape": {shapeOnce}, "day": {day}, "month": {month}, "year": {year}}
}

func TestPlantForm_TheAddFormOpensWithAWeeklyWateringRow(t *testing.T) {
	f := plantFormOn(t)

	rec := f.open(t, newPlantPath, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()

	if title := formTitle.FindStringSubmatch(page); title == nil || text(title[1]) != "Add a plant" {
		t.Errorf("the bar reads %v, want Add a plant", title)
	}
	if got := openRows(page); !slices.Equal(got, []string{"water"}) {
		t.Errorf("the form opens with %v scheduled, want water alone", got)
	}
	if got := closedRows(page); !slices.Equal(got, []string{"feed", "repot"}) {
		t.Errorf("the list names %v as not scheduled, want feed and repot", got)
	}
	if shape, every, unit := filled(t, page, "water-shape"), filled(t, page, "water-every"), filled(t, page, "water-unit"); shape != shapeCadence || every != "1" || unit != "week" {
		t.Errorf("the watering row opens on %s %s %s, want a weekly cadence", shape, every, unit)
	}
	if strings.Contains(page, "Archive") {
		t.Error("the add form offers to archive a plant that does not exist")
	}
}

func TestPlantForm_OnePostCreatesThePlantAndItsSchedules(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("botanical", "Ficus lyrata")
	values.Set("where", "Study")
	openValues(values, "water", cadenceValues("10", "day"))
	openValues(values, "repot", onceValues("0", "3", "2028"))

	plant := f.created(t, f.add(t, values))

	if plant.DisplayName() != "Ada" || value(plant.BotanicalName) != "Ficus lyrata" || value(plant.Location) != "Study" {
		t.Errorf("the plant reads %+v, want Ada, a fig, in the study", plant)
	}
	water := f.scheduleFor(t, plant.ID, "water")
	if water.IntervalCount == nil || *water.IntervalCount != 10 || *water.IntervalUnit != "day" || water.AnchorDate != nil {
		t.Errorf("the watering is %+v, want every 10 days and no anchor", water)
	}
	repot := f.scheduleFor(t, plant.ID, "repot")
	if repot.IntervalCount != nil {
		t.Errorf("the repot repeats every %v, and it was asked for once", repot.IntervalCount)
	}
	// Any day means a month rather than a date, so the anchor is stored on the
	// 1st of that month at month precision.
	want := time.Date(2028, time.March, 1, 0, 0, 0, 0, time.UTC)
	if repot.AnchorDate == nil || !repot.AnchorDate.Equal(want) || *repot.AnchorPrecision != "month" {
		t.Errorf("the repot is anchored to %v at %v precision, want %v at month", repot.AnchorDate, repot.AnchorPrecision, want)
	}
	if got := len(f.schedulesOf(t, plant.ID)); got != 2 {
		t.Errorf("the plant arrived with %d schedules, want the two the form opened", got)
	}
}

func TestPlantForm_APlantWithNoNameIsRefusedAndTheFormKeepsItsValues(t *testing.T) {
	f := plantFormOn(t)
	before := f.countPlants(t)
	values := addValues()
	values.Set("where", "Study")
	values.Set("notes", "The one from Ravi.")

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if got := errorsOn(page); !slices.Contains(got, "Give it at least one name. Any of the three will do.") {
		t.Errorf("the form says %v, want the line about the three names", got)
	}
	if where, notes := filled(t, page, "where"), filled(t, page, "notes"); where != "Study" || notes != "The one from Ravi." {
		t.Errorf("the refusal came back with %q and %q, and both were typed in", where, notes)
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the refused post wrote %d plants", after-before)
	}
}

func TestPlantForm_AnAcquiredMonthNeedsItsYear(t *testing.T) {
	f := plantFormOn(t)
	before := f.countPlants(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("acquired-month", "3")

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if got := errorsOn(page); !slices.Contains(got, "Give the year as well as the month.") {
		t.Errorf("the form says %v, want the line about the year", got)
	}
	// The message is inside the Reference disclosure, which has to be open
	// for it to be seen.
	if !strings.Contains(page, `<details class="disclosure" open>`) {
		t.Error("the refusal is behind a closed disclosure")
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the refused post wrote %d plants", after-before)
	}
}

func TestPlantForm_AnIntervalOfZeroIsRefusedWithTheMessageOnItsRow(t *testing.T) {
	f := plantFormOn(t)
	before := f.countPlants(t)
	values := addValues()
	values.Set("nickname", "Ada")
	openValues(values, "water", cadenceValues("0", "week"))

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if got := errorsOn(page); !slices.Contains(got, "Give a number between 1 and 999.") {
		t.Errorf("the row says %v, want the line about the count", got)
	}
	if got := filled(t, page, "water-every"); got != "0" {
		t.Errorf("the count came back as %q, and 0 was typed", got)
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the refused post wrote %d plants", after-before)
	}
}

// A shape changed without JavaScript posts without the date fields, because
// they are rendered only for the shape that needs them.
func TestPlantForm_AShapeChosenWithoutJavaScriptReRendersWithTheFieldsItNeeds(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	openValues(values, "water", url.Values{"shape": {shapeOnce}})

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if got := errorsOn(page); !slices.Contains(got, "Give it a date.") {
		t.Errorf("the row says %v, want the line about the date", got)
	}
	for _, id := range []string{"water-day", "water-month", "water-year"} {
		if !hasField(page, id) {
			t.Errorf("the row came back without %s, so there is nothing to answer with", id)
		}
	}
	if hasField(page, "water-every") {
		t.Error("a one-off came back asking how often it repeats")
	}
}

func TestPlantForm_ADayTheMonthDoesNotHaveIsRefused(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	openValues(values, "repot", onceValues("31", "2", "2027"))

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorsOn(rec.Body.String()); !slices.Contains(got, "There is no 31 February.") {
		t.Errorf("the row says %v, want the line about February", got)
	}
}

// A fixed date schedule is the one shape with both an interval and an anchor,
// and the schema refuses a season on a row with an anchor. The box is ticked
// here because a shape changed without JavaScript leaves it on the row, and the
// post has to drop the season rather than hit the constraint.
func TestPlantForm_AFixedDateScheduleStoresItsIntervalAndDateAndNoSeason(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	dated := url.Values{"shape": {shapeDate}, "every": {"1"}, "unit": {"year"}, "day": {"15"}, "month": {"4"}, "year": {"2027"}}
	dated["seasonal"] = []string{"on"}
	dated["from"] = []string{"3"}
	dated["to"] = []string{"9"}
	openValues(values, "feed", dated)

	plant := f.created(t, f.add(t, values))

	feed := f.scheduleFor(t, plant.ID, "feed")
	if feed.IntervalCount == nil || *feed.IntervalCount != 1 || *feed.IntervalUnit != "year" {
		t.Errorf("the feeding repeats %v %v, want every year", feed.IntervalCount, feed.IntervalUnit)
	}
	want := time.Date(2027, time.April, 15, 0, 0, 0, 0, time.UTC)
	if feed.AnchorDate == nil || !feed.AnchorDate.Equal(want) || *feed.AnchorPrecision != "day" {
		t.Errorf("the feeding is anchored to %v at %v precision, want %v at day", feed.AnchorDate, feed.AnchorPrecision, want)
	}
	if feed.SeasonStartMonth != nil || feed.SeasonEndMonth != nil {
		t.Errorf("the feeding runs %v to %v, and a date already names its month", feed.SeasonStartMonth, feed.SeasonEndMonth)
	}
}

func TestPlantForm_ASeasonalScheduleStoresItsMonths(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	season := cadenceValues("3", "week")
	season["seasonal"] = []string{"on"}
	season["from"] = []string{"4"}
	season["to"] = []string{"8"}
	openValues(values, "feed", season)

	plant := f.created(t, f.add(t, values))

	feed := f.scheduleFor(t, plant.ID, "feed")
	if feed.SeasonStartMonth == nil || *feed.SeasonStartMonth != 4 || *feed.SeasonEndMonth != 8 {
		t.Errorf("the feeding runs %v to %v, want April to August", feed.SeasonStartMonth, feed.SeasonEndMonth)
	}
}

// A post can have the box ticked and no months, because ticking the box renders
// the month selects only with JavaScript. The default months are used rather
// than refusing a field nobody saw.
func TestPlantForm_ASeasonTickedWithoutMonthsStoresTheDefaultMonths(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	season := cadenceValues("3", "week")
	season["seasonal"] = []string{"on"}
	openValues(values, "feed", season)

	plant := f.created(t, f.add(t, values))

	feed := f.scheduleFor(t, plant.ID, "feed")
	if feed.SeasonStartMonth == nil || *feed.SeasonStartMonth != 3 || *feed.SeasonEndMonth != 9 {
		t.Errorf("the feeding runs %v to %v, want March to September", feed.SeasonStartMonth, feed.SeasonEndMonth)
	}
}

func TestPlantForm_AClosedRowWritesNoSchedule(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	// The water row is open by default. Here it is closed by its close button.
	openValues(values, "water", cadenceValues("1", "week"))
	values.Set("close", "water")

	plant := f.created(t, f.add(t, values))

	if got := f.schedulesOf(t, plant.ID); len(got) != 0 {
		t.Errorf("the plant arrived with %d schedules, and every row was closed", len(got))
	}
}

func TestPlantForm_OpeningARowKeepsTheFormsValues(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("where", "Study")
	openValues(values, "water", cadenceValues("10", "day"))
	values.Add("open", "repot")

	rec := f.open(t, newPlantPath+"?"+values.Encode(), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()

	if got := openRows(page); !slices.Equal(got, []string{"water", "repot"}) {
		t.Errorf("the form has %v open, want the watering and the repot", got)
	}
	if name, where := filled(t, page, "nickname"), filled(t, page, "where"); name != "Ada" || where != "Study" {
		t.Errorf("the re-render came back with %q in %q, and Ada in the study was typed", name, where)
	}
	if every := filled(t, page, "water-every"); every != "10" {
		t.Errorf("the watering came back every %s, and 10 days was typed", every)
	}
}

func TestPlantForm_AnHTMXRequestGetsTheRowAlone(t *testing.T) {
	f := plantFormOn(t)

	rec := f.open(t, newPlantPath+"?open=repot", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()

	if got := openRows(page); !slices.Equal(got, []string{"repot"}) {
		t.Errorf("the swap answered with %v open, want the repot alone", got)
	}
	if strings.Contains(page, "<form") || strings.Contains(page, "sched--add") {
		t.Errorf("the swap answered with more of the page than the row:\n%s", page)
	}
	if !strings.Contains(page, `id="sched-repot"`) {
		t.Error("the row came back without the id the swap replaces")
	}
}

func TestPlantForm_TheEditFormIsFilledInWithTheReferenceSectionOpen(t *testing.T) {
	f := plantFormOn(t)
	f.exec(t, "UPDATE plant SET sun = 'Bright indirect.', acquired_year = 2024, acquired_month = 3 WHERE id = $1", bigFellaID)

	rec := f.editForm(t, bigFellaID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()

	if title := formTitle.FindStringSubmatch(page); title == nil || text(title[1]) != "Edit plant" {
		t.Errorf("the bar reads %v, want Edit plant rather than the plant's name", title)
	}
	for _, field := range []struct{ id, want string }{
		{"nickname", "Big Fella"},
		{"common", "Swiss cheese plant"},
		{"botanical", "Monstera deliciosa"},
		{"where", "Living room"},
		{"sun", "Bright indirect."},
		{"acquired-month", "3"},
		{"acquired-year", "2024"},
	} {
		if got := filled(t, page, field.id); got != field.want {
			t.Errorf("%s reads %q, want %q", field.id, got, field.want)
		}
	}
	if !strings.Contains(page, `<details class="disclosure" open>`) {
		t.Error("the reference is shut on a plant that has something in it")
	}
	// Schedules are edited on the plant's page, beside the due date they
	// change.
	if len(openRows(page)) != 0 || len(closedRows(page)) != 0 {
		t.Error("the edit form draws schedule rows")
	}
	if !strings.Contains(page, archivePlantPath(bigFellaID)) {
		t.Error("the edit form has no way to archive the plant")
	}
}

func TestPlantForm_AReaderWhoMayNotArchiveSeesNoArchiveLink(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities = auth.Capabilities{auth.PlantEdit: true}

	rec := f.editForm(t, bigFellaID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), archivePlantPath(bigFellaID)) {
		t.Error("the edit form offers to archive a plant to a reader who may not")
	}
}

// Sprout has a nickname and nothing else.
func TestPlantForm_APlantWithNoReferenceFieldsHasTheReferenceSectionClosed(t *testing.T) {
	f := plantFormOn(t)

	page := f.editForm(t, sproutID).Body.String()

	if !strings.Contains(page, `<details class="disclosure">`) {
		t.Error("the reference opened on a plant with nothing in it")
	}
}

func TestPlantForm_SavingClearsEmptiedFields(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Big Fella")
	values.Set("botanical", "Monstera deliciosa")
	values.Set("notes", "Wipe the leaves.")

	rec := f.save(t, bigFellaID, values)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != plantPath(bigFellaID) {
		t.Fatalf("the save answered %d to %q, want %d to the plant", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID)
	if err != nil {
		t.Fatal(err)
	}
	if plant.CommonName != nil || plant.Location != nil {
		t.Errorf("the plant kept %v and %v, and both fields were emptied", plant.CommonName, plant.Location)
	}
	if value(plant.Notes) != "Wipe the leaves." {
		t.Errorf("the note reads %q, want the one that was typed", value(plant.Notes))
	}
}

func TestPlantForm_ASaveWithNoNameIsRefusedAndWritesNothing(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()

	rec := f.save(t, bigFellaID, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID)
	if err != nil {
		t.Fatal(err)
	}
	if plant.DisplayName() != "Big Fella" {
		t.Errorf("the plant is now %q, and the post was refused", plant.DisplayName())
	}
}

func TestPlantForm_AnArchivedPlantsEditFormIs404(t *testing.T) {
	f := plantFormOn(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if rec := f.editForm(t, bigFellaID); rec.Code != http.StatusNotFound {
		t.Errorf("the form answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	values := addValues()
	values.Set("nickname", "Big Fella")
	if rec := f.save(t, bigFellaID, values); rec.Code != http.StatusNotFound {
		t.Errorf("the save answered %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPlantForm_ArchivingRemovesThePlantFromThePlantsList(t *testing.T) {
	f := plantFormOn(t)

	rec := f.archive(t, bigFellaID)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != plantsPath {
		t.Fatalf("archiving answered %d to %q, want %d to the plant list", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	list, err := store.New(f.tx).ListPlants(t.Context(), rosewoodID)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(list, func(p store.Plant) bool { return p.ID == bigFellaID }) {
		t.Error("the archived plant is still on the plant list")
	}
	// The history is kept. That is why plants are archived rather than deleted.
	if _, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID); err != nil {
		t.Errorf("the archived plant is gone: %v", err)
	}
	if rec := f.archive(t, bigFellaID); rec.Code != http.StatusNotFound {
		t.Errorf("archiving twice answered %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPlantForm_ArchiveShowsAConfirmationFirst(t *testing.T) {
	f := plantFormOn(t)

	rec := f.ask(t, bigFellaID, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "Archive Big Fella?") {
		t.Errorf("the foot does not ask about the plant:\n%s", text(page))
	}
	// The confirmation says the history is kept, not only that the plant goes.
	if !strings.Contains(page, "Its history is kept") {
		t.Errorf("the question does not say what archiving keeps:\n%s", text(page))
	}
	if !strings.Contains(page, "Keep it") {
		t.Error("the question has no way out of it")
	}
	// The plant's page is still shown above the confirmation.
	if name := heroName.FindStringSubmatch(page); name == nil || text(name[2]) != "Big Fella" {
		t.Errorf("the question was asked away from the plant:\n%v", name)
	}
	if got, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID); err != nil || got.ArchivedAt != nil {
		t.Error("asking the question archived the plant")
	}
}

func TestPlantForm_AnHTMXRequestGetsTheConfirmationAlone(t *testing.T) {
	f := plantFormOn(t)

	rec := f.ask(t, bigFellaID, plantFootID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()
	if !strings.Contains(page, `id="plant-foot"`) {
		t.Error("the swap came back without the id it replaces")
	}
	if !strings.Contains(page, "Archive Big Fella?") {
		t.Errorf("the swap does not carry the question:\n%s", page)
	}
	// Swapping a larger element than the change re-renders what did not change.
	if strings.Contains(page, "hero__name") || strings.Contains(page, "Recent") {
		t.Errorf("the swap answered with more of the page than the foot:\n%s", page)
	}
}

func TestPlantForm_KeepItRestoresTheButtons(t *testing.T) {
	f := plantFormOn(t)

	rec := f.keep(t, bigFellaID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()
	if strings.Contains(page, "Archive Big Fella?") {
		t.Errorf("the question is still up after Keep it:\n%s", page)
	}
	for _, want := range []string{`id="plant-foot"`, "Edit plant", "Archive"} {
		if !strings.Contains(page, want) {
			t.Errorf("the foot came back without %s:\n%s", want, page)
		}
	}
}

func TestPlantForm_TheArchiveConfirmationIs404ForAnArchivedPlant(t *testing.T) {
	f := plantFormOn(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if rec := f.ask(t, bigFellaID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the question answered %d for an archived plant, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPlantForm_AnotherGardensPlantCannotBeArchived(t *testing.T) {
	f := plantFormOn(t)
	fairview := uuid.MustParse("00000000-0000-7000-8000-0000000009f1")
	stranger := uuid.MustParse("00000000-0000-7000-8000-0000000009f2")
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairview)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Gerald')", stranger, fairview)

	if rec := f.archive(t, stranger); rec.Code != http.StatusNotFound {
		t.Errorf("archiving another garden's plant answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), fairview, stranger)
	if err != nil {
		t.Fatal(err)
	}
	if plant.ArchivedAt != nil {
		t.Error("another garden's plant was archived")
	}
}

func TestPlant_ASitterSeesNoEditOrArchiveButtons(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{}

	page := f.page(t, bigFellaID)

	if strings.Contains(page, "Edit plant") || strings.Contains(page, "Archive") {
		t.Errorf("the page offers a sitter the foot:\n%s", text(page))
	}
}

func TestPlant_TheBottomButtonsLinkToEditAndArchive(t *testing.T) {
	f := rosewoodPlant(t)

	page := f.page(t, bigFellaID)

	if !strings.Contains(page, `href="`+editPlantPath(bigFellaID)+`"`) {
		t.Error("the page does not lead to the form that edits it")
	}
	if !strings.Contains(page, `action="`+archivePlantPath(bigFellaID)+`"`) {
		t.Error("the page does not lead to archiving")
	}
}

func TestPlant_AnArchivedPlantHasNoEditOrArchiveButtons(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	page := f.page(t, bigFellaID)

	if strings.Contains(page, "Edit plant") || strings.Contains(page, "Archive") {
		t.Errorf("an archived plant offers the foot:\n%s", text(page))
	}
}
