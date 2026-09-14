package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// archivedPlantsLink returns the link to Archived plants, or nil.
func archivedPlantsLink(page string) *element {
	return readHTML(page).first(isTag("a"), attrIs("href", archivedPlantsPath))
}

// plantRows returns the href and the text of every link to a plant's page, in
// page order.
func plantRows(page string) (hrefs, texts []string) {
	for _, link := range readHTML(page).all(isTag("a")) {
		href := link.attr("href")
		if _, err := uuid.Parse(strings.TrimPrefix(href, plantsPath+"/")); err != nil {
			continue
		}
		hrefs = append(hrefs, href)
		texts = append(texts, link.text())
	}
	return hrefs, texts
}

// restoreButton returns the Restore button of the form that posts to the
// plant's restore URL, or nil.
func restoreButton(doc *element, plantID uuid.UUID) *element {
	return doc.first(isTag("form"), attrIs("action", restorePlantPath(plantID))).first(isTag("button"), textIs("Restore"))
}

// archiveTwo archives Doris and Nigel on the Plants fixture's garden, Nigel
// more recently, and adds an archived plant to another garden.
func archiveTwo(t *testing.T, f *plantsFixture) {
	t.Helper()
	f.exec(t, "UPDATE plant SET archived_at = $2 WHERE id = $1", dorisID, thursday.AddDate(0, 0, -30))
	f.exec(t, "UPDATE plant SET archived_at = $2 WHERE id = $1", nigelID, thursday.AddDate(0, 0, -2))
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	f.exec(t, "INSERT INTO plant (garden_id, nickname, archived_at) VALUES ($1, 'Kauri', now())", fairviewID)
}

func (f *plantsFixture) archived(t *testing.T) string {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, archivedPlantsPath, nil)
	rec := httptest.NewRecorder()
	f.handler.archived(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *plantsFixture) restore(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, restorePlantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.restore(rec, req)
	return rec
}

func TestPlants_TheArchivedPlantsLinkIsAbsentWithNothingArchived(t *testing.T) {
	f := rosewoodPlants(t)

	if link := archivedPlantsLink(f.show(t)); link != nil {
		t.Errorf("Plants links to Archived plants as %q with nothing archived", link.text())
	}
}

func TestPlants_TheArchivedPlantsLinkReadsTheNumberArchived(t *testing.T) {
	f := rosewoodPlants(t)
	archiveTwo(t, f)

	link := archivedPlantsLink(f.show(t))

	if link == nil {
		t.Fatal("Plants has no link to Archived plants with two plants archived")
	}
	if got := link.text(); got != "2 archived" {
		t.Errorf("the link to %s reads %q, want \"2 archived\"", archivedPlantsPath, got)
	}
}

func TestArchived_ListsTheGardensArchivedPlantsMostRecentFirstWithTheDay(t *testing.T) {
	f := rosewoodPlants(t)
	archiveTwo(t, f)

	hrefs, rows := plantRows(f.archived(t))

	if want := []string{"Nigel Boston fern · Archived 1 Sep", "Doris Snake plant · Archived 4 Aug"}; !slices.Equal(rows, want) {
		t.Errorf("Archived plants lists %q, want %q: this garden's, the most recently archived first, each with the day", rows, want)
	}
	if want := []string{plantPath(nigelID), plantPath(dorisID)}; !slices.Equal(hrefs, want) {
		t.Errorf("the rows link to %v, want %v", hrefs, want)
	}
}

func TestArchived_AnArchiveDateInAnotherYearNamesTheYear(t *testing.T) {
	f := rosewoodPlants(t)
	f.exec(t, "UPDATE plant SET archived_at = $2 WHERE id = $1", dorisID, time.Date(2025, time.December, 31, 23, 30, 0, 0, time.UTC))

	page := f.archived(t)

	if !strings.Contains(text(page), "Archived 31 Dec 2025") {
		t.Errorf("the row does not name the year:\n%s", text(page))
	}
}

func TestArchived_AnEmptyArchiveReadsNothingArchived(t *testing.T) {
	f := rosewoodPlants(t)

	page := f.archived(t)

	if _, rows := plantRows(page); len(rows) != 0 || !strings.Contains(text(page), "No archived plants") {
		t.Errorf("the empty archive reads:\n%s", text(page))
	}
}

func TestPlant_AnArchivedPlantSaysWhenItWasArchived(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = $2 WHERE id = $1", bigFellaID, thursday.AddDate(0, 0, -2))

	page := f.page(t, bigFellaID)

	if got, want := hero(page), "Big Fella Swiss cheese plant · Monstera deliciosa Living room Archived 1 Sep"; got != want {
		t.Errorf("the top of the page reads %q, want %q", got, want)
	}
}

func TestPlant_AnArchivedPlantOffersRestoreAndGoesBackToArchivedPlants(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	doc := readHTML(f.page(t, bigFellaID))

	if restoreButton(doc, bigFellaID) == nil {
		t.Errorf("the page has no Restore posting to %s", restorePlantPath(bigFellaID))
	}
	if back := doc.byID("plant-bar").first(isTag("a")); back.attr("href") != archivedPlantsPath || back.text() != "Archived plants" {
		t.Errorf("the back link is %s, want Archived plants at %s", back, archivedPlantsPath)
	}
	for _, absent := range []string{editPlantPath(bigFellaID), archivePlantPath(bigFellaID)} {
		if pointsAt(doc, absent) {
			t.Errorf("an archived plant's page offers %s", absent)
		}
	}
}

func TestPlant_AReaderWhoMayNotArchiveGetsNoRestore(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if restoreButton(readHTML(f.page(t, bigFellaID)), bigFellaID) != nil {
		t.Error("a reader without plant.archive was offered Restore")
	}
}

func TestPlant_RestoringPutsAnArchivedPlantBackOnThePlantsList(t *testing.T) {
	f := rosewoodPlants(t)
	archiveTwo(t, f)

	rec := f.restore(t, dorisID)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != plantPath(dorisID) {
		t.Fatalf("restoring returned %d to %q, want %d to the plant's page", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	list, err := store.New(f.tx).ListPlants(t.Context(), rosewoodID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list, func(p store.Plant) bool { return p.ID == dorisID }) {
		t.Error("the restored plant is not on the plant list")
	}
	if got := archivedPlantsLink(f.show(t)).text(); got != "1 archived" {
		t.Errorf("after restoring one of two, the Archived plants link reads %q, want 1 archived", got)
	}
}

func TestPlant_RestoringAPlantThatIsNotArchivedIs404(t *testing.T) {
	f := rosewoodPlants(t)

	if rec := f.restore(t, dorisID); rec.Code != http.StatusNotFound {
		t.Errorf("restoring a plant on the list returned %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPlant_RestoringAnotherGardensArchivedPlantIs404AndLeavesItArchived(t *testing.T) {
	f := rosewoodPlants(t)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", fairviewID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname, archived_at) VALUES ($1, $2, 'Kauri', now())", fairviewPlantID, fairviewID)

	if rec := f.restore(t, fairviewPlantID); rec.Code != http.StatusNotFound {
		t.Errorf("restoring another garden's plant returned %d, want %d", rec.Code, http.StatusNotFound)
	}
	var archived bool
	if err := f.tx.QueryRow(t.Context(), "SELECT archived_at IS NOT NULL FROM plant WHERE id = $1", fairviewPlantID).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if !archived {
		t.Error("another garden's plant was restored")
	}
}

// restoreSwap posts Restore for plantID the way htmx sends it.
func (f *plantFixture) restoreSwap(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, restorePlantPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", plantBodyID)
	rec := httptest.NewRecorder()
	f.handler.restore(rec, req)
	return rec
}

func TestPlant_RestoreSwapsTheBackLinkToPlants(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	body := fragment(t, f.restoreSwap(t, bigFellaID), plantBodyID)

	back := readHTML(body).first(attrIs("hx-swap-oob", "innerHTML:#plant-bar")).first(isTag("a"))
	if back.attr("href") != plantsPath || back.text() != "Plants" {
		t.Errorf("the swap's back link is %s, want Plants at %s", back, plantsPath)
	}
}

func TestPlant_RestoreSwapsThePageUnderTheTopBarAndOffersArchiveInstead(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	body := fragment(t, f.restoreSwap(t, bigFellaID), plantBodyID)

	doc := readHTML(body)
	if restoreButton(doc, bigFellaID) != nil {
		t.Errorf("the swap still offers Restore:\n%s", text(body))
	}
	if !pointsAt(doc, archivePlantPath(bigFellaID)) {
		t.Errorf("the swap has no Archive button, so the plant does not read as restored:\n%s", text(body))
	}
	if got, want := announcedIn(body), "Big Fella restored. It’s back on Plants."; got != want {
		t.Errorf("the swap announces %q, want %q", got, want)
	}
}
