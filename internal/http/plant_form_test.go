package http

import (
	"bytes"
	"context"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

type formFixture struct {
	*plantFixture
	photoDir string
}

// plantFormOn sets up the add and edit forms on the garden the plant page is
// tested on, with a third care type so the schedule list includes a care no
// plant here is scheduled for.
func plantFormOn(t *testing.T) *formFixture {
	t.Helper()

	f := rosewoodPlant(t)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Repot', 'repot')", repotID, rosewoodID)
	dir := t.TempDir()
	photos, err := photo.NewStore(dir, testPhotoQuota)
	if err != nil {
		t.Fatal(err)
	}
	f.handler.photos = photos
	return &formFixture{plantFixture: f, photoDir: dir}
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

// addMultipart posts the add form as multipart/form-data, the encoding the
// form uses to post a photo. An empty filename gives the photo part a browser
// sends when nothing was chosen.
func (f *formFixture) addMultipart(t *testing.T, values url.Values, filename string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, held := range values {
		for _, v := range held {
			if err := form.WriteField(name, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	part, err := form.CreateFormFile("photo", filename)
	if err != nil {
		t.Fatal(err)
	}
	if filename != "" {
		if _, err := part.Write([]byte("the resized bytes")); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, newPlantPath, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	f.handler.create(rec, req)
	return rec
}

// postPhoto posts values with the two photo parts filled, to the add form when
// plantID is nil and to that plant's edit form otherwise. A nil square leaves
// that part out.
func (f *formFixture) postPhoto(t *testing.T, plantID *uuid.UUID, values url.Values, image, square []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, held := range values {
		for _, v := range held {
			if err := form.WriteField(name, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, file := range map[string][]byte{"photo": image, "photo-square": square} {
		if file == nil {
			continue
		}
		part, err := form.CreateFormFile(name, name+".jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	rec := httptest.NewRecorder()
	if plantID == nil {
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, newPlantPath, &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		f.handler.create(rec, req)
		return rec
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, editPlantPath(*plantID), &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.SetPathValue("plant", plantID.String())
	f.handler.update(rec, req)
	return rec
}

// photoOf returns the one photo row on plantID, failing when there is not
// exactly one.
func (f *formFixture) photoOf(t *testing.T, plantID uuid.UUID) store.Photo {
	t.Helper()

	rows, err := f.tx.Query(t.Context(), "SELECT id FROM photo WHERE plant_id = $1", plantID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("the plant has %d photos, want 1", len(ids))
	}
	row, err := store.New(f.tx).GetPhoto(t.Context(), rosewoodID, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// storedFiles lists every file under the photo directory, relative to it.
func (f *formFixture) storedFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(f.photoDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(f.photoDir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
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

// keep requests the plant's page the way the confirmation's Cancel button does.
// The response is the buttons at the bottom with the confirmation closed.
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
	formError  = regexp.MustCompile(`<p class="field__error"([^>]*)>(.*?)</p>`)
	formRow    = regexp.MustCompile(`<li class="sched sched--(editing|add)" id="sched-([a-z]+)"`)
	formRooms  = regexp.MustCompile(`(?s)<datalist id="rooms">(.*?)</datalist>`)
	formRoom   = regexp.MustCompile(`<option value="([^"]*)">`)
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

// roomsOffered returns the rooms the Room field lists, in page order.
func roomsOffered(t *testing.T, page string) []string {
	t.Helper()

	list := formRooms.FindStringSubmatch(page)
	if list == nil {
		t.Fatal("the form offers no rooms under Location")
	}
	var rooms []string
	for _, m := range formRoom.FindAllStringSubmatch(list[1], -1) {
		rooms = append(rooms, text(m[1]))
	}
	return rooms
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

// errorsOn returns the error lines the page shows. A <p> with the hidden
// attribute is left out because the server renders it whatever was posted and
// only the page's script shows it.
func errorsOn(page string) []string {
	var lines []string
	for _, m := range formError.FindAllStringSubmatch(page, -1) {
		if strings.Contains(m[1], "hidden") {
			continue
		}
		lines = append(lines, text(m[2]))
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
		"room":           {""},
		"notes":          {""},
		"acquired-month": {"0"},
		"acquired-year":  {"0"},
	}
	for _, ref := range detailFields {
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

	if title := formTitle.FindStringSubmatch(page); title == nil || text(title[1]) != "Add plant" {
		t.Errorf("the bar reads %v, want Add plant", title)
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
	values.Set("room", "Study")
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

func TestPlantForm_AMultipartPostWithNoPhotoCreatesThePlant(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("room", "Study")

	plant := f.created(t, f.addMultipart(t, values, ""))

	if plant.DisplayName() != "Ada" || value(plant.Location) != "Study" {
		t.Errorf("the plant reads %+v, want Ada in the study", plant)
	}
}

func TestPlantForm_APostOverTheBodyCapIsRefusedInEitherEncoding(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("notes", strings.Repeat("x", formMaxBytes+1))
	before := f.countPlants(t)

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"query string": f.add(t, values),
		"multipart":    f.addMultipart(t, values, "photo.jpg"),
	} {
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusRequestEntityTooLarge)
		}
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the garden has %d plants, want the %d it started with", after, before)
	}
}

func TestPlantForm_ARefusedPostWithAPhotoSaysThePhotoNeedsChoosingAgain(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("room", "Study")

	rec := f.addMultipart(t, values, "monstera.jpg")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorsOn(rec.Body.String()); !slices.Contains(got, "Choose the photo again.") {
		t.Errorf("the refused form says %v, want the line about the photo", got)
	}
}

func TestPlantForm_ARefusedPostWithNoPhotoDoesNotAskForOne(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("room", "Study")

	rec := f.addMultipart(t, values, "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorsOn(rec.Body.String()); slices.Contains(got, "Choose the photo again.") {
		t.Errorf("the refused form says %v, and no photo was chosen", got)
	}
}

func TestPlantForm_APlantWithNoNameIsRefusedAndTheFormKeepsItsValues(t *testing.T) {
	f := plantFormOn(t)
	before := f.countPlants(t)
	values := addValues()
	values.Set("room", "Study")
	values.Set("notes", "The one from Ravi.")

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if got := errorsOn(page); !slices.Contains(got, "Enter at least one name.") {
		t.Errorf("the form says %v, want the line about the three names", got)
	}
	if room, notes := filled(t, page, "room"), filled(t, page, "notes"); room != "Study" || notes != "The one from Ravi." {
		t.Errorf("the refusal came back with %q and %q, and both were typed in", room, notes)
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
	if got := errorsOn(page); !slices.Contains(got, "Choose a year as well as a month.") {
		t.Errorf("the form says %v, want the line about the year", got)
	}
	// The message is inside the Details disclosure. The disclosure has to be
	// open for it to be seen.
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
	if got := errorsOn(page); !slices.Contains(got, "Enter a number between 1 and 999.") {
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
	if got := errorsOn(page); !slices.Contains(got, "Choose a date.") {
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
	if got := errorsOn(rec.Body.String()); !slices.Contains(got, "Invalid date.") {
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
	values.Set("room", "Study")
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
	if name, room := filled(t, page, "nickname"), filled(t, page, "room"); name != "Ada" || room != "Study" {
		t.Errorf("the re-render came back with %q in %q, and Ada in the study was typed", name, room)
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

func TestPlantForm_BothFormsOfferEveryRoomThePlantsAreIn(t *testing.T) {
	f := plantFormOn(t)

	// Windowsill holds two plants and Sprout has no room, so a room appears
	// once and a plant with no room adds nothing.
	want := []string{"Bathroom", "Bedroom", "Kitchen", "Living room", "Windowsill"}
	if got := roomsOffered(t, f.open(t, newPlantPath, false).Body.String()); !slices.Equal(got, want) {
		t.Errorf("the add form offers %v, want %v", got, want)
	}
	if got := roomsOffered(t, f.editForm(t, bigFellaID).Body.String()); !slices.Equal(got, want) {
		t.Errorf("the edit form offers %v, want %v", got, want)
	}
}

func TestPlantForm_ARoomOnlyAnArchivedPlantIsInIsNotOffered(t *testing.T) {
	f := plantFormOn(t)
	// Trail Mix is the only plant in the Kitchen, so archiving it leaves the
	// Kitchen with nothing in it.
	f.archive(t, trailMixID)

	rooms := roomsOffered(t, f.open(t, newPlantPath, false).Body.String())

	if slices.Contains(rooms, "Kitchen") {
		t.Errorf("the form offers %v, and the Kitchen is empty", rooms)
	}
}

func TestPlantForm_ARoomTypedInAnotherCaseIsStoredAsTheRoomTheGardenHas(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("room", "bathroom")

	plant := f.created(t, f.add(t, values))

	if value(plant.Location) != "Bathroom" {
		t.Errorf("the plant is in %q, want Bathroom, the room the garden already has", value(plant.Location))
	}
}

func TestPlantForm_ARoomMatchingNoneTheGardenHasIsStoredAsItWasTyped(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("room", "Potting shed")

	plant := f.created(t, f.add(t, values))

	if value(plant.Location) != "Potting shed" {
		t.Errorf("the plant is in %q, want the Potting shed it was given", value(plant.Location))
	}
}

func TestPlantForm_SavingARoomInAnotherCaseKeepsTheGardensSpelling(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Big Fella")
	values.Set("room", "LIVING ROOM")

	rec := f.save(t, bigFellaID, values)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID)
	if err != nil {
		t.Fatalf("reading the plant the save wrote: %v", err)
	}
	if value(plant.Location) != "Living room" {
		t.Errorf("the plant is in %q, want Living room, the room the garden already has", value(plant.Location))
	}
}

func TestPlantForm_ARefusedPostShowsTheRoomInTheGardensSpelling(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("room", "bathroom")

	rec := f.add(t, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := filled(t, rec.Body.String(), "room"); got != "Bathroom" {
		t.Errorf("the Location field reads %q, want Bathroom", got)
	}
}

func TestPlantForm_OpeningARowShowsTheRoomInTheGardensSpelling(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	values.Set("room", "bathroom")
	values.Add("open", "water")

	// With no script the button that opens a schedule row submits the whole
	// form as a GET, so the server re-renders the Room field.
	rec := f.open(t, newPlantPath+"?"+values.Encode(), false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := filled(t, rec.Body.String(), "room"); got != "Bathroom" {
		t.Errorf("the Location field reads %q, want Bathroom", got)
	}
}

func TestPlantForm_TheEditFormIsFilledInWithTheDetailsSectionOpen(t *testing.T) {
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
		{"room", "Living room"},
		{"sun", "Bright indirect."},
		{"acquired-month", "3"},
		{"acquired-year", "2024"},
	} {
		if got := filled(t, page, field.id); got != field.want {
			t.Errorf("%s reads %q, want %q", field.id, got, field.want)
		}
	}
	if !strings.Contains(page, `<details class="disclosure" open>`) {
		t.Error("the Details disclosure is shut on a plant that has something in it")
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
func TestPlantForm_APlantWithNoDetailsHasTheDetailsSectionClosed(t *testing.T) {
	f := plantFormOn(t)

	page := f.editForm(t, sproutID).Body.String()

	if !strings.Contains(page, `<details class="disclosure">`) {
		t.Error("the Details disclosure opened on a plant with nothing in it")
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
	if !strings.Contains(page, "keeps its activity and photos") {
		t.Errorf("the question does not say what archiving keeps:\n%s", text(page))
	}
	if !strings.Contains(page, ">Cancel<") {
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

func TestPlantForm_CancelOnTheArchiveQuestionRestoresTheButtons(t *testing.T) {
	f := plantFormOn(t)

	rec := f.keep(t, bigFellaID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()
	if strings.Contains(page, "Archive Big Fella?") {
		t.Errorf("the question is still up after Cancel:\n%s", page)
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

func TestPlantForm_APlantAddedWithAPhotoHasTheFileAndTheRow(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	image, square := testJPEG(t, 30, 20), testJPEG(t, 8, 8)

	plant := f.created(t, f.postPhoto(t, nil, values, image, square))

	row := f.photoOf(t, plant.ID)
	if row.Kind != "image/jpeg" || row.Width != 30 || row.Height != 20 {
		t.Errorf("the row reads %s %d by %d, want image/jpeg 30 by 20", row.Kind, row.Width, row.Height)
	}
	if row.Bytes != int64(len(image)) || row.SquareBytes == nil || *row.SquareBytes != int64(len(square)) {
		t.Errorf("the row counts %d and %v bytes, want %d and %d", row.Bytes, row.SquareBytes, len(image), len(square))
	}
	if row.UploadedBy != readerID || row.TakenAt != nil {
		t.Errorf("the row was uploaded by %s at a taken time of %v, want the reader and no taken time", row.UploadedBy, row.TakenAt)
	}
	if want := photo.Path(rosewoodID, plant.ID, row.ID, photo.JPEG); row.Path != want {
		t.Errorf("path = %q, want %q", row.Path, want)
	}
	stored, err := os.ReadFile(filepath.Join(f.photoDir, row.Path))
	if err != nil || !bytes.Equal(stored, image) {
		t.Errorf("the file at %s holds %d bytes, want the %d posted: %v", row.Path, len(stored), len(image), err)
	}
	storedSquare, err := os.ReadFile(filepath.Join(f.photoDir, photo.SquarePath(row.Path)))
	if err != nil || !bytes.Equal(storedSquare, square) {
		t.Errorf("the square at %s holds %d bytes, want the %d posted: %v", photo.SquarePath(row.Path), len(storedSquare), len(square), err)
	}
}

// Every list shows a plant's picture as its square, and the form's script
// posts both files. A post with no square did not come from the form.
func TestPlantForm_APhotoPostedWithoutItsSquareIsRefusedAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Big Fella")
	id := bigFellaID

	rec := f.postPhoto(t, &id, values, testJPEG(t, 30, 20), nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if n := countRows(t, f.tx, "photo"); n != 0 {
		t.Errorf("the garden has %d photos, want none", n)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID)
	if err != nil {
		t.Fatal(err)
	}
	if plant.ProfilePhotoID != nil {
		t.Errorf("the plant's picture is %s, want none", *plant.ProfilePhotoID)
	}
}

func TestPlantForm_APhotoThatIsNotAnImageIsRefusedAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	before := f.countPlants(t)

	rec := f.postPhoto(t, nil, values, []byte("the resized bytes"), nil)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the garden has %d plants, want the %d it started with", after, before)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPlantForm_APhotoOverTheFileLimitIsRefusedAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	// A valid JPEG padded past the limit, so the size is the only thing wrong
	// with it.
	image := append(testJPEG(t, 30, 20), make([]byte, photo.MaxBytes)...)
	before := f.countPlants(t)

	rec := f.postPhoto(t, nil, values, image, testJPEG(t, 8, 8))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the garden has %d plants, want the %d it started with", after, before)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

// The photo becomes the plant's profile picture, so adding photos is not enough
// on its own.
func TestPlantForm_APhotoFromAMemberWhoMayNotSetThePictureIs404(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities = auth.Capabilities{auth.PlantCreate: true, auth.PlantEdit: true, auth.PhotoAdd: true}
	values := addValues()
	values.Set("nickname", "Ada")
	before := f.countPlants(t)
	id := bigFellaID
	photoID := givePicture(t, f.tx, bigFellaID)
	removing := addValues()
	removing.Set("nickname", "Big Fella")
	removing.Set("photo-removed", "1")

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"add":    f.postPhoto(t, nil, values, testJPEG(t, 30, 20), testJPEG(t, 8, 8)),
		"edit":   f.postPhoto(t, &id, values, testJPEG(t, 30, 20), testJPEG(t, 8, 8)),
		"remove": f.save(t, bigFellaID, removing),
	} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, http.StatusNotFound)
		}
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the garden has %d plants, want the %d it started with", after, before)
	}
	if n := countRows(t, f.tx, "photo"); n != 1 {
		t.Errorf("the garden has %d photos, want the one it started with", n)
	}
	if got := f.pictureOf(t, bigFellaID); got == nil || *got != photoID {
		t.Errorf("Big Fella's picture is %v, want the one the post could not remove", got)
	}
}

func TestPlantForm_ThePhotoFieldIsRenderedOnlyForAMemberWhoMaySetThePicture(t *testing.T) {
	f := plantFormOn(t)

	with := f.open(t, newPlantPath, false).Body.String()
	f.principal.Capabilities = auth.Capabilities{auth.PlantCreate: true, auth.PhotoAdd: true}
	without := f.open(t, newPlantPath, false).Body.String()

	if !strings.Contains(with, `id="photo-field"`) {
		t.Error("a member who may set the picture got a form with no photo field")
	}
	if strings.Contains(without, `id="photo-field"`) {
		t.Error("a member who may add photos but not set the picture got a form with the photo field")
	}
}

// pictureOf returns the plant's profile_photo_id, nil for no picture.
func (f *formFixture) pictureOf(t *testing.T, plantID uuid.UUID) *uuid.UUID {
	t.Helper()

	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, plantID)
	if err != nil {
		t.Fatal(err)
	}
	return plant.ProfilePhotoID
}

func TestPlantForm_APlantAddedWithAPhotoHasItAsItsPicture(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")

	plant := f.created(t, f.postPhoto(t, nil, values, testJPEG(t, 30, 20), testJPEG(t, 8, 8)))

	row := f.photoOf(t, plant.ID)
	if plant.ProfilePhotoID == nil || *plant.ProfilePhotoID != row.ID {
		t.Errorf("the plant's picture is %v, want the photo posted, %s", plant.ProfilePhotoID, row.ID)
	}
}

func TestPlantForm_APhotoSavedOnEditBecomesThePictureAndTheOldOneStaysAPhoto(t *testing.T) {
	f := plantFormOn(t)
	old := givePicture(t, f.tx, bigFellaID)
	values := addValues()
	values.Set("nickname", "Big Fella")
	id := bigFellaID

	rec := f.postPhoto(t, &id, values, testJPEG(t, 30, 20), testJPEG(t, 8, 8))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	got := f.pictureOf(t, bigFellaID)
	if got == nil || *got == old {
		t.Errorf("the plant's picture is %v, want the new photo", got)
	}
	if n := countRows(t, f.tx, "photo"); n != 2 {
		t.Errorf("the garden has %d photos, want both", n)
	}
}

func TestPlantForm_ASaveWithNoPhotoKeepsThePicture(t *testing.T) {
	f := plantFormOn(t)
	photoID := givePicture(t, f.tx, bigFellaID)
	values := addValues()
	values.Set("nickname", "Big Fella")

	if rec := f.save(t, bigFellaID, values); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	if got := f.pictureOf(t, bigFellaID); got == nil || *got != photoID {
		t.Errorf("the plant's picture is %v, want %s still", got, photoID)
	}
}

func TestPlantForm_RemoveTakesThePictureOffThePlantAndKeepsThePhoto(t *testing.T) {
	f := plantFormOn(t)
	givePicture(t, f.tx, bigFellaID)
	values := addValues()
	values.Set("nickname", "Big Fella")
	values.Set("photo-removed", "1")

	if rec := f.save(t, bigFellaID, values); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	if got := f.pictureOf(t, bigFellaID); got != nil {
		t.Errorf("the plant's picture is %s, want none", *got)
	}
	if n := countRows(t, f.tx, "photo"); n != 1 {
		t.Errorf("the garden has %d photos, want the removed picture kept as one", n)
	}
}

func TestPlantForm_TheEditFormShowsTheCurrentPictureAndTheAddFormShowsNone(t *testing.T) {
	f := plantFormOn(t)
	photoID := givePicture(t, f.tx, bigFellaID)

	edit := f.editForm(t, bigFellaID).Body.String()
	add := f.open(t, newPlantPath, false).Body.String()

	if got, want := images(edit), []string{photoFullPath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the edit form's images are %v, want the picture at %v", got, want)
	}
	if !strings.Contains(edit, `alt="Current photo"`) {
		t.Error("the edit form does not say the picture is the current one")
	}
	if got := images(add); len(got) != 0 {
		t.Errorf("the add form's images are %v, want none", got)
	}
}

func TestPlantForm_ARefusedSaveThatRemovedThePictureShowsNoPictureAndLeavesItStored(t *testing.T) {
	f := plantFormOn(t)
	photoID := givePicture(t, f.tx, bigFellaID)
	values := addValues()
	values.Set("photo-removed", "1")

	rec := f.save(t, bigFellaID, values)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := images(rec.Body.String()); len(got) != 0 {
		t.Errorf("the form's images are %v, want none after Remove", got)
	}
	if got := hiddenFields(rec.Body.String()).Get("photo-removed"); got != "1" {
		t.Errorf("photo-removed = %q, want 1, so the next save removes the picture", got)
	}
	if got := f.pictureOf(t, bigFellaID); got == nil || *got != photoID {
		t.Errorf("the plant's picture is %v, want %s, since the save was refused", got, photoID)
	}
}

func TestPlantForm_APhotoWithASquareOfADifferentKindIsRefusedAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	before := f.countPlants(t)

	rec := f.postPhoto(t, nil, values, testJPEG(t, 30, 20), testWebP(8, 8))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the garden has %d plants, want the %d it started with", after, before)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPlantForm_AWebPPhotoIsStoredWithTheWebPKindAndExtension(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	image := testWebP(2048, 1536)

	plant := f.created(t, f.postPhoto(t, nil, values, image, testWebP(192, 192)))

	row := f.photoOf(t, plant.ID)
	if row.Kind != "image/webp" || row.Width != 2048 || row.Height != 1536 {
		t.Errorf("the row reads %s %d by %d, want image/webp 2048 by 1536", row.Kind, row.Width, row.Height)
	}
	if !strings.HasSuffix(row.Path, ".webp") {
		t.Errorf("path = %q, want it to end in .webp", row.Path)
	}
	stored, err := os.ReadFile(filepath.Join(f.photoDir, row.Path))
	if err != nil || !bytes.Equal(stored, image) {
		t.Errorf("the file at %s holds %d bytes, want the %d posted: %v", row.Path, len(stored), len(image), err)
	}
}

func TestPlantForm_APhotoOfExactlyTheFileLimitIsStored(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	image := testJPEG(t, 30, 20)
	image = append(image, make([]byte, photo.MaxBytes-len(image))...)

	plant := f.created(t, f.postPhoto(t, nil, values, image, testJPEG(t, 8, 8)))

	if row := f.photoOf(t, plant.ID); row.Bytes != photo.MaxBytes {
		t.Errorf("the row counts %d bytes, want the %d posted", row.Bytes, photo.MaxBytes)
	}
}

// photosWithRoomFor gives the form a photo store whose quota is room bytes.
func (f *formFixture) photosWithRoomFor(t *testing.T, room int) {
	t.Helper()

	photos, err := photo.NewStore(f.photoDir, int64(room))
	if err != nil {
		t.Fatal(err)
	}
	f.handler.photos = photos
}

func TestPlantForm_APhotoTheGardenHasNoRoomForIsRefusedWithTheReasonUnderThePhotoField(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	image := testJPEG(t, 30, 20)
	f.photosWithRoomFor(t, len(image)-1)
	before := f.countPlants(t)

	rec := f.postPhoto(t, nil, values, image, testJPEG(t, 8, 8))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if got := errorsOn(rec.Body.String()); !slices.Contains(got, photoQuotaFull) {
		t.Errorf("the form's errors are %v, want %q", got, photoQuotaFull)
	}
	if got := filled(t, rec.Body.String(), "nickname"); got != "Ada" {
		t.Errorf("nickname reads %q, want Ada", got)
	}
	if after := f.countPlants(t); after != before {
		t.Errorf("the garden has %d plants, want the %d it started with", after, before)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPlantForm_APlantIsSavedWithNoPhotoWhenTheGardensPhotoStorageIsFull(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	f.photosWithRoomFor(t, 0)

	plant := f.created(t, f.add(t, values))

	if plant.DisplayName() != "Ada" {
		t.Errorf("the plant is %q, want Ada", plant.DisplayName())
	}
}

func TestPlantForm_APhotoAndItsSquareOfExactlyTheRoomLeftAreStored(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	image, square := testJPEG(t, 30, 20), testJPEG(t, 8, 8)
	f.photosWithRoomFor(t, len(image)+len(square))

	plant := f.created(t, f.postPhoto(t, nil, values, image, square))

	if got := f.storedFiles(t); len(got) != 2 {
		t.Errorf("the directory holds %v, want the photo and its square", got)
	}
	if row := f.photoOf(t, plant.ID); row.Bytes+*row.SquareBytes != int64(len(image)+len(square)) {
		t.Errorf("the row counts %d and %d bytes, want %d and %d", row.Bytes, *row.SquareBytes, len(image), len(square))
	}
}

func TestPlantForm_AnEditWithAPhotoTheGardenHasNoRoomForLeavesThePlantUnchanged(t *testing.T) {
	f := plantFormOn(t)
	values := addValues()
	values.Set("nickname", "Ada")
	image := testJPEG(t, 30, 20)
	f.photosWithRoomFor(t, len(image)-1)
	id := bigFellaID

	rec := f.postPhoto(t, &id, values, image, testJPEG(t, 8, 8))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorsOn(rec.Body.String()); !slices.Contains(got, photoQuotaFull) {
		t.Errorf("the form's errors are %v, want %q", got, photoQuotaFull)
	}
	plant, err := store.New(f.tx).GetPlant(t.Context(), rosewoodID, bigFellaID)
	if err != nil {
		t.Fatal(err)
	}
	if plant.DisplayName() != "Big Fella" {
		t.Errorf("the plant is now %q, and the post was refused", plant.DisplayName())
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}
