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
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
)

var (
	moreWaterID  = uuid.MustParse("00000000-0000-7000-8000-000000000320")
	moreFeedID   = uuid.MustParse("00000000-0000-7000-8000-000000000321")
	moreMistID   = uuid.MustParse("00000000-0000-7000-8000-000000000322")
	morePlantID  = uuid.MustParse("00000000-0000-7000-8000-000000000323")
	otherPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000324")
)

// careTypeGarden gives Ellie's garden the four care types the Garden page has
// to tell apart: Water with two events behind it, Feed with none, Mist turned
// off with one event still logged against it, and Prune in the other garden.
func careTypeGarden(t *testing.T) *moreFixture {
	t.Helper()

	f := moreGarden(t)
	f.principal.Capabilities[auth.CareTypeManage] = true
	f.exec(t, `INSERT INTO care_type (id, garden_id, name, slug, created_at, archived_at) VALUES
		($1, $2, 'Water', 'water', '2026-01-01T00:00:00Z', NULL),
		($3, $2, 'Feed',  'feed',  '2026-01-02T00:00:00Z', NULL),
		($4, $2, 'Mist',  'mist',  '2026-01-03T00:00:00Z', now())`,
		moreWaterID, moreGardenID, moreFeedID, moreMistID)
	f.exec(t, "INSERT INTO care_type (garden_id, name, slug) VALUES ($1, 'Prune', 'prune')", otherGardenID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern')", morePlantID, moreGardenID)
	f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done) VALUES
		($1, $2, $3, $4, now(), true),
		($1, $2, $3, $4, now(), true),
		($1, $2, $5, $4, now(), true)`,
		moreGardenID, morePlantID, moreWaterID, moreUserID, moreMistID)
	return f
}

// careType posts to one of the open row's buttons, or renders the page with
// that row open when form is nil. These tests call the handler rather than the
// mux, so nothing else sets the slug in the URL.
func (f *moreFixture) careType(t *testing.T, handler http.HandlerFunc, slug, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	method, body := http.MethodGet, strings.NewReader("")
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.SetPathValue("care", slug)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

var (
	careTypeLink   = regexp.MustCompile(`(?s)<a class="row row--link row--setting[^"]*" href="([^"]+)">(.*?)</a>`)
	careTypeForm   = regexp.MustCompile(`(?s)<form class="[^"]*row--editing[^"]*" method="post" action="([^"]+)"[^>]*>(.*?)</form>`)
	careTypeField  = regexp.MustCompile(`<input class="input" type="text" name="name" value="([^"]*)"`)
	careTypeWhyRow = regexp.MustCompile(`(?s)<p class="row__why">(.*?)</p>`)
	careTypeDrop   = regexp.MustCompile(`(?s)<button class="row__drop" type="submit" formaction="([^"]+)">(.*?)</button>`)
	gardenNameRow  = regexp.MustCompile(`<input class="input" id="name" name="name" type="text" value="([^"]*)">`)
)

type listedType struct {
	name string
	// note is what the row says at its right-hand end, "Off" for a care type
	// that has been turned off and empty for the rest.
	note string
	edit string
}

// listedTypesOf reads the closed rows of the Care types list, in page order.
func listedTypesOf(page string) []listedType {
	var out []listedType
	for _, m := range careTypeLink.FindAllStringSubmatch(page, -1) {
		row := listedType{edit: m[1]}
		if label := linkRowLabel.FindStringSubmatch(m[2]); label != nil {
			row.name = text(label[1])
		}
		if note := linkRowNote.FindStringSubmatch(m[2]); note != nil {
			row.note = text(note[1])
		}
		out = append(out, row)
	}
	return out
}

func listedNames(rows []listedType) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.name)
	}
	return out
}

// editor is the one row of the list that is open as a form.
type editor struct {
	// action is where Save posts.
	action string
	// name is what the field holds.
	name string
	// why is the sentence under the field, and message the error under it.
	why     string
	message string
	// drop is the word on the button beside Save, and dropTo where it posts.
	drop   string
	dropTo string
}

// editorOn returns the open row, and fails when no row on the page is open.
func editorOn(t *testing.T, page string) editor {
	t.Helper()

	m := careTypeForm.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no row is open:\n%s", page)
	}
	open := editor{action: m[1]}
	if field := careTypeField.FindStringSubmatch(m[2]); field != nil {
		open.name = field[1]
	}
	if why := careTypeWhyRow.FindStringSubmatch(m[2]); why != nil {
		open.why = text(why[1])
	}
	if message := fieldError.FindStringSubmatch(m[2]); message != nil {
		open.message = text(message[1])
	}
	if drop := careTypeDrop.FindStringSubmatch(m[2]); drop != nil {
		open.dropTo, open.drop = drop[1], text(drop[2])
	}
	return open
}

func (f *moreFixture) careTypeRow(t *testing.T, slug string) (name string, archived bool) {
	t.Helper()

	var archivedAt *string
	err := f.tx.QueryRow(t.Context(), "SELECT name, archived_at::text FROM care_type WHERE garden_id = $1 AND slug = $2",
		moreGardenID, slug).Scan(&name, &archivedAt)
	if err != nil {
		t.Fatalf("reading the %s care type back: %v", slug, err)
	}
	return name, archivedAt != nil
}

func (f *moreFixture) careTypeCount(t *testing.T, slug string) int {
	t.Helper()

	var count int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM care_type WHERE garden_id = $1 AND slug = $2",
		moreGardenID, slug).Scan(&count); err != nil {
		t.Fatalf("counting the %s care types: %v", slug, err)
	}
	return count
}

func TestGarden_SavingWritesTheGardensName(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.saveGardenName, gardenPath, url.Values{"name": {"  The Roof  "}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != savedURL(gardenPath) {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, savedURL(gardenPath))
	}
	var name string
	if err := f.tx.QueryRow(t.Context(), "SELECT name FROM garden WHERE id = $1", moreGardenID).Scan(&name); err != nil {
		t.Fatalf("reading the garden back: %v", err)
	}
	if name != "The Roof" {
		t.Errorf("the garden is called %q, want %q", name, "The Roof")
	}
}

func TestGarden_AnEmptyGardenNameIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.saveGardenName, gardenPath, url.Values{"name": {"   "}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := fieldError.FindStringSubmatch(rec.Body.String()); got == nil || text(got[1]) != gardenNameMissing {
		t.Errorf("the form says %v, want %q", got, gardenNameMissing)
	}
	var name string
	if err := f.tx.QueryRow(t.Context(), "SELECT name FROM garden WHERE id = $1", moreGardenID).Scan(&name); err != nil {
		t.Fatalf("reading the garden back: %v", err)
	}
	if name != "Rosewood" {
		t.Errorf("the refused post left the garden called %q, want Rosewood", name)
	}
}

func TestGarden_TheNameFieldHoldsTheGardensName(t *testing.T) {
	f := careTypeGarden(t)

	page := f.page(t, f.handler.garden, gardenPath)

	if got := gardenNameRow.FindStringSubmatch(page); got == nil || got[1] != "Rosewood" {
		t.Errorf("the name field holds %v, want Rosewood", got)
	}
}

func TestGarden_ACareTypeThatIsOffKeepsItsPlaceInTheListAndItsRowReadsOff(t *testing.T) {
	f := careTypeGarden(t)

	rows := listedTypesOf(f.page(t, f.handler.garden, gardenPath))

	if got := listedNames(rows); !slices.Equal(got, []string{"Water", "Feed", "Mist"}) {
		t.Fatalf("the list holds %v, want Water, Feed, Mist", got)
	}
	if rows[2].note != "Off" {
		t.Errorf("the Mist row says %q, want %q", rows[2].note, "Off")
	}
	if rows[0].note != "" {
		t.Errorf("the Water row says %q, want nothing", rows[0].note)
	}
}

func TestGarden_AnotherGardensCareTypeIsNotListed(t *testing.T) {
	f := careTypeGarden(t)

	got := listedNames(listedTypesOf(f.page(t, f.handler.garden, gardenPath)))

	if slices.Contains(got, "Prune") {
		t.Errorf("the list holds %v, and Prune belongs to the other garden", got)
	}
}

// The care type routes require care_type.manage and not garden.edit, so this
// page can be reached by somebody who may not rename the garden.
func TestGarden_TheNameFormIsLeftOffForAReaderWhoCannotRenameTheGarden(t *testing.T) {
	f := careTypeGarden(t)
	delete(f.principal.Capabilities, auth.GardenEdit)

	page := f.page(t, f.handler.newCareType, careTypesPath)

	if gardenNameRow.MatchString(page) {
		t.Error("the page still has the name field")
	}
	if strings.Contains(page, "Save the name") {
		t.Error("the page still has the Save the name button")
	}
}

func TestGarden_TheCareTypesAreLeftOffForAReaderWhoCannotManageThem(t *testing.T) {
	f := careTypeGarden(t)
	delete(f.principal.Capabilities, auth.CareTypeManage)

	page := f.page(t, f.handler.garden, gardenPath)

	if got := listedTypesOf(page); len(got) > 0 {
		t.Errorf("the page lists %v, and this reader cannot change a care type", listedNames(got))
	}
	if strings.Contains(page, "Care types") {
		t.Error("the page still has the Care types heading")
	}
}

func TestGarden_ARowWithEventsBehindItOffersTurningItOffAndSaysHowManyTimesItWasUsed(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.editCareType, "water", careTypePath("water"), nil)

	open := editorOn(t, rec.Body.String())
	if open.name != "Water" {
		t.Errorf("the field holds %q, want Water", open.name)
	}
	if open.drop != "Turn off" || open.dropTo != offCareTypePath("water") {
		t.Errorf("the button says %q and posts to %q, want %q to %s", open.drop, open.dropTo, "Turn off", offCareTypePath("water"))
	}
	if !strings.HasPrefix(open.why, "Used 2 times, so it can be renamed or turned off but not deleted") {
		t.Errorf("the row says %q, want it to open on the two events logged", open.why)
	}
}

func TestGarden_ARowWithNothingLoggedAgainstItOffersDeleting(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.editCareType, "feed", careTypePath("feed"), nil)

	open := editorOn(t, rec.Body.String())
	if open.drop != "Delete" || open.dropTo != deleteCareTypePath("feed") {
		t.Errorf("the button says %q and posts to %q, want %q to %s", open.drop, open.dropTo, "Delete", deleteCareTypePath("feed"))
	}
	if open.why != "Not used yet, so it can be deleted." {
		t.Errorf("the row says %q, want it to say the care type is not used yet", open.why)
	}
}

func TestGarden_ARowThatIsOffOffersTurningItBackOn(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.editCareType, "mist", careTypePath("mist"), nil)

	open := editorOn(t, rec.Body.String())
	if open.drop != "Turn on" || open.dropTo != onCareTypePath("mist") {
		t.Errorf("the button says %q and posts to %q, want %q to %s", open.drop, open.dropTo, "Turn on", onCareTypePath("mist"))
	}
}

func TestGarden_ARowThatIsOffSaysItIsHiddenFromSchedulesAndLogCare(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.editCareType, "mist", careTypePath("mist"), nil)

	want := "Turned off. It’s hidden from schedules and Log care until turned on again."
	if got := editorOn(t, rec.Body.String()).why; got != want {
		t.Errorf("the row says %q, want %q", got, want)
	}
}

func TestGarden_ACareTypeInAnotherGardenHasNoRowToOpen(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.editCareType, "prune", careTypePath("prune"), nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGarden_ThePageASaveRedirectsToSaysSavedAndAPlainVisitDoesNot(t *testing.T) {
	f := moreGarden(t)

	if page := f.page(t, f.handler.garden, savedURL(gardenPath)); !strings.Contains(text(page), "Saved") {
		t.Errorf("the page after a save does not say Saved:\n%s", text(page))
	}
	if page := f.page(t, f.handler.garden, gardenPath); strings.Contains(text(page), "Saved") {
		t.Errorf("a plain visit says Saved:\n%s", text(page))
	}
}

func TestGarden_ARenameChangesTheNameAndLeavesTheSlug(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.renameCareType, "feed", careTypePath("feed"), url.Values{"name": {"Fertilise"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != gardenPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, gardenPath)
	}
	if name, _ := f.careTypeRow(t, "feed"); name != "Fertilise" {
		t.Errorf("the care type with the slug feed is called %q, want Fertilise", name)
	}
}

func TestGarden_ARenameToAnEmptyNameIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.renameCareType, "feed", careTypePath("feed"), url.Values{"name": {"  "}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := editorOn(t, rec.Body.String()).message; got != careTypeNameMissing {
		t.Errorf("the row says %q, want %q", got, careTypeNameMissing)
	}
	if name, _ := f.careTypeRow(t, "feed"); name != "Feed" {
		t.Errorf("the refused rename left the care type called %q, want Feed", name)
	}
}

func TestGarden_ARenameToANameAnotherCareTypeHasIsRefusedAndTheMessageNamesThatType(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.renameCareType, "feed", careTypePath("feed"), url.Values{"name": {"water"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := editorOn(t, rec.Body.String()).message; got != careTypeNameTaken("Water") {
		t.Errorf("the row says %q, want %q", got, careTypeNameTaken("Water"))
	}
	if name, _ := f.careTypeRow(t, "feed"); name != "Feed" {
		t.Errorf("the refused rename left the care type called %q, want Feed", name)
	}
}

func TestGarden_ARenameToTheSameNameInCapitalsIsSaved(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.renameCareType, "water", careTypePath("water"), url.Values{"name": {"WATER"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if name, _ := f.careTypeRow(t, "water"); name != "WATER" {
		t.Errorf("the care type is called %q, want WATER", name)
	}
}

func TestGarden_ANewCareTypeIsCreatedWithASlugFromItsName(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.createCareType, careTypesPath, url.Values{"name": {"Wipe leaves"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != gardenPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, gardenPath)
	}
	if name, off := f.careTypeRow(t, "wipe_leaves"); name != "Wipe leaves" || off {
		t.Errorf("the new care type is %q, off = %v, want %q and on", name, off, "Wipe leaves")
	}
}

func TestGarden_ANameACareTypeAlreadyHasIsRefusedRatherThanCreatingASecond(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.createCareType, careTypesPath, url.Values{"name": {"Water"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := editorOn(t, rec.Body.String()).message; got != careTypeNameTaken("Water") {
		t.Errorf("the form says %q, want %q", got, careTypeNameTaken("Water"))
	}
	if got := f.careTypeCount(t, "water"); got != 1 {
		t.Errorf("the garden has %d care types with the slug water, want 1", got)
	}
}

func TestGarden_ANameACareTypeThatIsOffAlreadyHasIsRefused(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.createCareType, careTypesPath, url.Values{"name": {"Mist"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := editorOn(t, rec.Body.String()).message; got != careTypeNameTaken("Mist") {
		t.Errorf("the form says %q, want %q", got, careTypeNameTaken("Mist"))
	}
}

func TestGarden_ANameWithNoLetterOrNumberInItIsRefused(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.createCareType, careTypesPath, url.Values{"name": {"!!!"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	open := editorOn(t, rec.Body.String())
	if open.message != careTypeNameUnusable {
		t.Errorf("the form says %q, want %q", open.message, careTypeNameUnusable)
	}
	if open.name != "!!!" {
		t.Errorf("the field holds %q, want what was typed", open.name)
	}
	var count int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM care_type WHERE garden_id = $1", moreGardenID).Scan(&count); err != nil {
		t.Fatalf("counting the care types: %v", err)
	}
	if count != 3 {
		t.Errorf("the garden has %d care types, want the 3 it started with", count)
	}
}

func TestGarden_AnEmptyNewCareTypeIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.do(t, f.handler.createCareType, careTypesPath, url.Values{"name": {""}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := editorOn(t, rec.Body.String()).message; got != careTypeNameMissing {
		t.Errorf("the form says %q, want %q", got, careTypeNameMissing)
	}
}

func TestGarden_TurningACareTypeOffKeepsTheEventsLoggedAgainstIt(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.turnOffCareType, "water", offCareTypePath("water"), url.Values{})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != gardenPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, gardenPath)
	}
	if _, off := f.careTypeRow(t, "water"); !off {
		t.Error("the care type is still on")
	}
	var events int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM care_event WHERE care_type_id = $1", moreWaterID).Scan(&events); err != nil {
		t.Fatalf("counting the events: %v", err)
	}
	if events != 2 {
		t.Errorf("%d events are recorded against it, want the 2 there were", events)
	}
}

func TestGarden_TurningOffACareTypeThatIsAlreadyOffIsRefused(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.turnOffCareType, "mist", offCareTypePath("mist"), url.Values{})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGarden_TurningACareTypeBackOnClearsOffFromItsRow(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.turnOnCareType, "mist", onCareTypePath("mist"), url.Values{})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if _, off := f.careTypeRow(t, "mist"); off {
		t.Error("the care type is still off")
	}
	for _, row := range listedTypesOf(f.page(t, f.handler.garden, gardenPath)) {
		if row.name == "Mist" && row.note != "" {
			t.Errorf("the Mist row says %q, want nothing", row.note)
		}
	}
}

func TestGarden_TurningOnACareTypeThatIsAlreadyOnIsRefused(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.turnOnCareType, "water", onCareTypePath("water"), url.Values{})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGarden_DeletingACareTypeWithEventsBehindItIsRefused(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.deleteCareType, "water", deleteCareTypePath("water"), url.Values{})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := f.careTypeCount(t, "water"); got != 1 {
		t.Errorf("the garden has %d care types with the slug water, want the 1 it had", got)
	}
}

func TestGarden_DeletingACareTypeWithNothingLoggedAgainstItRemovesIt(t *testing.T) {
	f := careTypeGarden(t)

	rec := f.careType(t, f.handler.deleteCareType, "feed", deleteCareTypePath("feed"), url.Values{})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != gardenPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, gardenPath)
	}
	if got := f.careTypeCount(t, "feed"); got != 0 {
		t.Errorf("the garden still has %d care types with the slug feed", got)
	}
}

func TestGarden_DeletingACareTypeTwiceIsRefusedTheSecondTime(t *testing.T) {
	f := careTypeGarden(t)

	f.careType(t, f.handler.deleteCareType, "feed", deleteCareTypePath("feed"), url.Values{})
	rec := f.careType(t, f.handler.deleteCareType, "feed", deleteCareTypePath("feed"), url.Values{})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGarden_AddingACareTypeOpensAnEmptyRowAtTheEndOfTheList(t *testing.T) {
	f := careTypeGarden(t)

	page := f.page(t, f.handler.newCareType, careTypesPath)

	open := editorOn(t, page)
	if open.action != careTypesPath {
		t.Errorf("the row posts to %q, want %s", open.action, careTypesPath)
	}
	if open.name != "" {
		t.Errorf("the field holds %q, want nothing", open.name)
	}
	if open.drop != "" {
		t.Errorf("the row offers %q, and a care type that does not exist yet has nothing to remove", open.drop)
	}
	if got := listedNames(listedTypesOf(page)); !slices.Equal(got, []string{"Water", "Feed", "Mist"}) {
		t.Errorf("the closed rows are %v, want the three the garden has", got)
	}
}

// storageLineOn captures the text of the sentence under Photos from the
// rendered page.
var storageLineOn = regexp.MustCompile(`>([^<]*of photo storage used\.[^<]*)<`)

// photoQuota gives the Garden page a photo store with the given quota.
func (f *moreFixture) photoQuota(t *testing.T, quota int64) {
	t.Helper()

	photos, err := photo.NewStore(t.TempDir(), quota)
	if err != nil {
		t.Fatal(err)
	}
	f.handler.photos = photos
}

// insertPhoto records a photo of the given sizes in the garden. The row has no
// file behind it, since the page only sums the rows.
func (f *moreFixture) insertPhoto(t *testing.T, garden, plant uuid.UUID, bytes int64, square *int64) {
	t.Helper()

	f.exec(t, `INSERT INTO photo (garden_id, plant_id, uploaded_by, kind, path, width, height, bytes, square_bytes)
		VALUES ($1, $2, $3, 'image/jpeg', 'x', 1, 1, $4, $5)`, garden, plant, moreUserID, bytes, square)
}

func TestGarden_ThePhotosLineSaysHowMuchOfTheGardensStorageIsUsed(t *testing.T) {
	f := careTypeGarden(t)
	square := int64(1 << 20)
	f.photoQuota(t, 4<<20)
	f.insertPhoto(t, moreGardenID, morePlantID, 1<<20, &square)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Ivy')", otherPlantID, otherGardenID)
	f.exec(t, `INSERT INTO photo (garden_id, plant_id, uploaded_by, kind, path, width, height, bytes)
		VALUES ($1, $2, $3, 'image/jpeg', 'y', 1, 1, $4)`, otherGardenID, otherPlantID, otherUserID, int64(1<<20))

	got := storageLineOn.FindStringSubmatch(f.page(t, f.handler.garden, gardenPath))

	if got == nil || got[1] != "2 MB of 4 MB of photo storage used." {
		t.Errorf("the Photos line reads %v, want the garden's own 2 MB of 4 MB", got)
	}
}

func TestGarden_NearlyFullThePhotosLineSaysToDeletePhotosToMakeRoom(t *testing.T) {
	f := careTypeGarden(t)
	f.photoQuota(t, 4<<20)
	f.insertPhoto(t, moreGardenID, morePlantID, 3700<<10, nil)

	got := storageLineOn.FindStringSubmatch(f.page(t, f.handler.garden, gardenPath))

	want := "4 MB of 4 MB of photo storage used. When it’s full, delete photos to make room."
	if got == nil || got[1] != want {
		t.Errorf("the Photos line reads %v, want %q", got, want)
	}
}

func TestStorageLine_AFigureUnderAGigabyteReadsAsWholeMegabytes(t *testing.T) {
	for _, c := range []struct {
		used, quota int64
		want        string
	}{
		{0, 1_000_000_000, "0 MB of 1 GB of photo storage used."},
		{200_000, 1_000_000_000, "0 MB of 1 GB of photo storage used."},
		{312_000_000, 1_000_000_000, "312 MB of 1 GB of photo storage used."},
		{999_499_999, 2_000_000_000, "999 MB of 2 GB of photo storage used."},
	} {
		if got := storageLine(photo.Usage{Used: c.used, Quota: c.quota}); got != c.want {
			t.Errorf("storageLine(%d of %d) = %q, want %q", c.used, c.quota, got, c.want)
		}
	}
}

func TestStorageLine_AFigureOfAGigabyteOrMoreReadsAsGigabytes(t *testing.T) {
	for _, c := range []struct {
		used, quota int64
		want        string
	}{
		{1_000_000_000, 5_000_000_000, "1 GB of 5 GB of photo storage used."},
		{1_001_000_000, 5_000_000_000, "1.0 GB of 5 GB of photo storage used."},
		{1_500_000_000, 5_000_000_000, "1.5 GB of 5 GB of photo storage used."},
		// A quota written as 4GiB is 4,294,967,296 bytes and reads in decimal
		// units, as a disk tool would show it.
		{1_000_000_000, 4 << 30, "1 GB of 4.3 GB of photo storage used."},
	} {
		if got := storageLine(photo.Usage{Used: c.used, Quota: c.quota}); got != c.want {
			t.Errorf("storageLine(%d of %d) = %q, want %q", c.used, c.quota, got, c.want)
		}
	}
}

func TestStorageLine_TheLineAddsWhatToDeleteFromNineTenthsOfTheQuota(t *testing.T) {
	for _, c := range []struct {
		used, quota int64
		want        string
	}{
		{899_999_999, 1_000_000_000, "900 MB of 1 GB of photo storage used."},
		{900_000_000, 1_000_000_000, "900 MB of 1 GB of photo storage used. When it’s full, delete photos to make room."},
		{2_000_000_000, 1_000_000_000, "2 GB of 1 GB of photo storage used. When it’s full, delete photos to make room."},
	} {
		if got := storageLine(photo.Usage{Used: c.used, Quota: c.quota}); got != c.want {
			t.Errorf("storageLine(%d of %d) = %q, want %q", c.used, c.quota, got, c.want)
		}
	}
}
