package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

// storedPhoto inserts a photo of the plant uploaded by the user at the given
// time, writes its file and its square under the fixture's directory, and
// returns its id.
func (f *formFixture) storedPhoto(t *testing.T, plantID, uploadedBy uuid.UUID, uploadedAt time.Time) uuid.UUID {
	t.Helper()

	photoID := givePhoto(t, f.tx, plantID, uploadedBy, uploadedAt)
	row, err := store.New(f.tx).GetPhoto(t.Context(), rosewoodID, photoID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{row.Path, photo.SquarePath(row.Path)} {
		full := filepath.Join(f.photoDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return photoID
}

// grid requests the plant's Photos page. query is appended to the URL, and
// htmx sets the HX-Request header the Older photos tile sends.
func (f *formFixture) grid(t *testing.T, plantID uuid.UUID, query string, htmx bool) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, photosPath(plantID)+query, nil)
	req.SetPathValue("plant", plantID.String())
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	f.handler.photoGrid(rec, req)
	return rec
}

// tiles returns the href of every link that holds a photo, in order.
func tiles(markup string) []string {
	var hrefs []string
	for _, link := range readHTML(markup).all(isTag("a")) {
		if link.first(isTag("img")) != nil {
			hrefs = append(hrefs, link.attr("href"))
		}
	}
	return hrefs
}

// olderPhotos returns the URL the Older photos tile fetches, or "" when the
// page has no such tile.
func olderPhotos(markup string) string {
	return readHTML(markup).byID("photos-more").attr("hx-get")
}

func TestPhotos_TheGridListsThePlantsPhotosNewestFirst(t *testing.T) {
	f := plantFormOn(t)
	older := f.storedPhoto(t, bigFellaID, readerID, thursday.AddDate(0, 0, -30))
	newer := f.storedPhoto(t, bigFellaID, readerID, thursday)
	f.storedPhoto(t, dorisID, readerID, thursday)

	rec := f.grid(t, bigFellaID, "", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	want := []string{photoPath(bigFellaID, newer), photoPath(bigFellaID, older)}
	if got := tiles(rec.Body.String()); !slices.Equal(got, want) {
		t.Errorf("the grid links to %v, want %v", got, want)
	}
	if olderPhotos(rec.Body.String()) != "" {
		t.Error("the grid offers older photos when it holds every photo")
	}
}

func TestPhotos_ATileShowsThePhotosSquare(t *testing.T) {
	f := plantFormOn(t)
	photoID := givePhoto(t, f.tx, bigFellaID, readerID, thursday)

	rec := f.grid(t, bigFellaID, "", false)

	if got, want := images(rec.Body.String()), []string{photoSquarePath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the grid's images are %v, want %v", got, want)
	}
}

func TestPhotos_APhotoWithNoSquareIsShownAtFullSize(t *testing.T) {
	f := plantFormOn(t)
	photoID := givePhoto(t, f.tx, bigFellaID, readerID, thursday)
	if _, err := f.tx.Exec(t.Context(), "UPDATE photo SET square_bytes = NULL WHERE id = $1", photoID); err != nil {
		t.Fatal(err)
	}

	rec := f.grid(t, bigFellaID, "", false)

	if got, want := images(rec.Body.String()), []string{photoFullPath(bigFellaID, photoID)}; !slices.Equal(got, want) {
		t.Errorf("the grid's images are %v, want %v", got, want)
	}
}

func TestPhotos_TheGridShowsTwentyFourPhotosAndTheOlderTileFetchesTheRest(t *testing.T) {
	f := plantFormOn(t)
	var ids []uuid.UUID
	for i := range gridPageSize + 1 {
		ids = append(ids, f.storedPhoto(t, bigFellaID, readerID, thursday.Add(-time.Duration(i)*time.Minute)))
	}

	first := f.grid(t, bigFellaID, "", false).Body.String()

	if got := tiles(first); len(got) != gridPageSize || got[0] != photoPath(bigFellaID, ids[0]) {
		t.Fatalf("the first page has %d tiles starting at %v, want %d starting at the newest", len(got), got[:1], gridPageSize)
	}
	older := olderPhotos(first)
	if older == "" {
		t.Fatal("the first page has no Older photos tile")
	}
	rec := f.grid(t, bigFellaID, strings.TrimPrefix(older, photosPath(bigFellaID)), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("the older page answered %d:\n%s", rec.Code, rec.Body.String())
	}
	second := rec.Body.String()
	if got, want := tiles(second), []string{photoPath(bigFellaID, ids[gridPageSize])}; !slices.Equal(got, want) {
		t.Errorf("the second page links to %v, want %v", got, want)
	}
	if olderPhotos(second) != "" {
		t.Error("the last page offers older photos")
	}
	if readHTML(second).first(isTag("html")) != nil {
		t.Error("the Older photos tile got a whole page, want the tiles alone")
	}
}

func TestPhotos_ALastPageOfOnePhotoIsAnnouncedAsOnePhoto(t *testing.T) {
	f := plantFormOn(t)
	for i := range gridPageSize + 1 {
		f.storedPhoto(t, bigFellaID, readerID, thursday.Add(-time.Duration(i)*time.Minute))
	}
	older := olderPhotos(f.grid(t, bigFellaID, "", false).Body.String())
	if older == "" {
		t.Fatal("the first page has no Older photos tile")
	}

	second := f.grid(t, bigFellaID, strings.TrimPrefix(older, photosPath(bigFellaID)), true).Body.String()

	if got, want := announcedIn(second), "1 older photo added."; got != want {
		t.Errorf("the last page announces %q, want %q", got, want)
	}
}

func TestPhotos_TheOlderPhotosLinkGetsTheWholePageWithTheRemainingTiles(t *testing.T) {
	f := plantFormOn(t)
	var ids []uuid.UUID
	for i := range gridPageSize + 1 {
		ids = append(ids, f.storedPhoto(t, bigFellaID, readerID, thursday.Add(-time.Duration(i)*time.Minute)))
	}
	older := olderPhotos(f.grid(t, bigFellaID, "", false).Body.String())
	if older == "" {
		t.Fatal("the first page has no Older photos tile")
	}

	rec := f.grid(t, bigFellaID, strings.TrimPrefix(older, photosPath(bigFellaID)), false)

	if rec.Code != http.StatusOK {
		t.Fatalf("the older page answered %d:\n%s", rec.Code, rec.Body.String())
	}
	page := rec.Body.String()
	if readHTML(page).first(isTag("html")) == nil {
		t.Error("following the link got the tiles alone, want the whole page")
	}
	if got, want := tiles(page), []string{photoPath(bigFellaID, ids[gridPageSize])}; !slices.Equal(got, want) {
		t.Errorf("the page links to %v, want %v", got, want)
	}
}

func TestPhotos_TwoPhotosUploadedInTheSameSecondAreEachListedOnceAcrossThePages(t *testing.T) {
	f := plantFormOn(t)
	at := thursday
	var ids []uuid.UUID
	for range gridPageSize + 1 {
		ids = append(ids, f.storedPhoto(t, bigFellaID, readerID, at))
	}

	first := f.grid(t, bigFellaID, "", false).Body.String()
	older := olderPhotos(first)
	if older == "" {
		t.Fatal("the first page has no Older photos tile")
	}
	second := f.grid(t, bigFellaID, strings.TrimPrefix(older, photosPath(bigFellaID)), true).Body.String()

	seen := append(tiles(first), tiles(second)...)
	slices.Sort(seen)
	var want []string
	for _, id := range ids {
		want = append(want, photoPath(bigFellaID, id))
	}
	slices.Sort(want)
	if !slices.Equal(seen, want) {
		t.Errorf("the two pages together link to %d photos, want each of the %d once", len(seen), len(want))
	}
}

func TestPhotos_ACursorThatDoesNotParseIs404(t *testing.T) {
	f := plantFormOn(t)

	rec := f.grid(t, bigFellaID, "?before=yesterday", false)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPhotos_APlantWithNoPhotosOffersAMemberAddPhoto(t *testing.T) {
	f := plantFormOn(t)

	page := f.grid(t, bigFellaID, "", false).Body.String()

	if !strings.Contains(text(page), "No photos yet") {
		t.Errorf("the page does not say there are no photos:\n%s", text(page))
	}
	if got := readHTML(page).first(isTag("a"), attrIs("href", newPhotoPath(bigFellaID))).text(); got != "Add photo" {
		t.Errorf("the link to %s reads %q, want Add photo", newPhotoPath(bigFellaID), got)
	}
}

func TestPhotos_ASitterIsNotOfferedAddPhoto(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}
	f.storedPhoto(t, bigFellaID, readerID, thursday)

	page := f.grid(t, bigFellaID, "", false).Body.String()

	if pointsAt(readHTML(page), newPhotoPath(bigFellaID)) {
		t.Error("a sitter's grid links to Add a photo")
	}
}

func TestPhotos_AMembersGridLeadsWithTheAddTile(t *testing.T) {
	f := plantFormOn(t)
	f.storedPhoto(t, bigFellaID, readerID, thursday)

	page := f.grid(t, bigFellaID, "", false).Body.String()

	if got := readHTML(page).byID("photo-grid").first(isTag("a")).attr("href"); got != newPhotoPath(bigFellaID) {
		t.Errorf("the grid's first link points at %q, want the add tile at %s", got, newPhotoPath(bigFellaID))
	}
}

func TestPhotos_AnArchivedPlantsGridHasNoAddTile(t *testing.T) {
	f := plantFormOn(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)
	f.storedPhoto(t, bigFellaID, readerID, thursday)

	rec := f.grid(t, bigFellaID, "", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if pointsAt(readHTML(rec.Body.String()), newPhotoPath(bigFellaID)) {
		t.Error("an archived plant's grid links to Add a photo")
	}
}

func TestPhotos_APlantTheGardenDoesNotHaveIs404(t *testing.T) {
	f := plantFormOn(t)

	rec := f.grid(t, uuid.MustParse("00000000-0000-7000-8000-000000000999"), "", false)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
