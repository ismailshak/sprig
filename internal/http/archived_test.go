package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	archivedLinkEl = regexp.MustCompile(`<a class="foot-link" href="([^"]+)">(.*?)<svg`)
	archivedRowEl  = regexp.MustCompile(`(?s)<a class="row row--link" href="([^"]+)">(.*?)</a>`)
	rowMeta        = regexp.MustCompile(`(?s)<span class="row__meta">(.*?)</span>\s*</span>`)
	backLink       = regexp.MustCompile(`<a class="backlink" href="([^"]+)">(?:<svg.*?</svg>)?([^<]+)</a>`)
	restoreForm    = regexp.MustCompile(`<form method="post" action="([^"]+)"><button class="care care--outline">Restore</button></form>`)
)

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

	if m := archivedLinkEl.FindStringSubmatch(f.show(t)); m != nil {
		t.Errorf("Plants links to Archived plants as %q with nothing archived", m[2])
	}
}

func TestPlants_TheArchivedPlantsLinkReadsTheNumberArchived(t *testing.T) {
	f := rosewoodPlants(t)
	archiveTwo(t, f)

	m := archivedLinkEl.FindStringSubmatch(f.show(t))

	if m == nil {
		t.Fatal("Plants has no link to Archived plants with two plants archived")
	}
	if m[1] != archivedPlantsPath || m[2] != "2 archived" {
		t.Errorf("the link reads %q to %s, want \"2 archived\" to %s", m[2], m[1], archivedPlantsPath)
	}
}

func TestArchived_ListsTheGardensArchivedPlantsMostRecentFirstWithTheDay(t *testing.T) {
	f := rosewoodPlants(t)
	archiveTwo(t, f)

	page := f.archived(t)

	var hrefs, names, metas []string
	for _, m := range archivedRowEl.FindAllStringSubmatch(page, -1) {
		hrefs = append(hrefs, m[1])
		names = append(names, text(rowLeadName.FindStringSubmatch(m[2])[2]))
		metas = append(metas, text(rowMeta.FindStringSubmatch(m[2])[1]))
	}
	if want := []string{"Nigel", "Doris"}; !slices.Equal(names, want) {
		t.Errorf("Archived plants lists %v, want %v: this garden's, the most recently archived first", names, want)
	}
	if want := []string{plantPath(nigelID), plantPath(dorisID)}; !slices.Equal(hrefs, want) {
		t.Errorf("the rows link to %v, want %v", hrefs, want)
	}
	if want := []string{"Boston fern · Archived 1 Sep", "Snake plant · Archived 4 Aug"}; !slices.Equal(metas, want) {
		t.Errorf("the second lines read %v, want %v", metas, want)
	}
}

func TestArchived_AnArchiveDateInAnotherYearNamesTheYear(t *testing.T) {
	f := rosewoodPlants(t)
	f.exec(t, "UPDATE plant SET archived_at = $2 WHERE id = $1", dorisID, time.Date(2025, time.December, 31, 23, 30, 0, 0, time.UTC))

	page := f.archived(t)

	if !strings.Contains(page, "Archived 31 Dec 2025") {
		t.Errorf("the row does not name the year:\n%s", text(page))
	}
}

func TestArchived_AnEmptyArchiveReadsNothingArchived(t *testing.T) {
	f := rosewoodPlants(t)

	page := f.archived(t)

	if archivedRowEl.MatchString(page) || !strings.Contains(page, "No archived plants") {
		t.Errorf("the empty archive reads:\n%s", text(page))
	}
}

func TestPlant_AnArchivedPlantSaysWhenItWasArchived(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = $2 WHERE id = $1", bigFellaID, thursday.AddDate(0, 0, -2))

	page := f.page(t, bigFellaID)

	if !strings.Contains(page, `<p class="hero__where">Archived 1 Sep</p>`) {
		t.Errorf("the page does not say when the plant was archived:\n%s", text(page))
	}
}

func TestPlant_AnArchivedPlantOffersRestoreAndGoesBackToArchivedPlants(t *testing.T) {
	f := rosewoodPlant(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	page := f.page(t, bigFellaID)

	if m := restoreForm.FindStringSubmatch(page); m == nil || m[1] != restorePlantPath(bigFellaID) {
		t.Errorf("the page has no Restore posting to %s:\n%v", restorePlantPath(bigFellaID), m)
	}
	if m := backLink.FindStringSubmatch(page); m == nil || m[1] != archivedPlantsPath || m[2] != "Archived plants" {
		t.Errorf("the back link is %v, want Archived plants at %s", m, archivedPlantsPath)
	}
	for _, absent := range []string{editPlantPath(bigFellaID), archivePlantPath(bigFellaID)} {
		if strings.Contains(page, absent) {
			t.Errorf("an archived plant's page offers %s", absent)
		}
	}
}

func TestPlant_AReaderWhoMayNotArchiveGetsNoRestore(t *testing.T) {
	f := rosewoodPlant(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if page := f.page(t, bigFellaID); restoreForm.MatchString(page) {
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
	if m := archivedLinkEl.FindStringSubmatch(f.show(t)); m == nil || m[2] != "1 archived" {
		t.Errorf("after restoring one of two, the Archived plants link reads %v, want 1 archived", m)
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
