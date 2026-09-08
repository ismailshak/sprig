package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// photoPage requests a photo's page. asking requests the delete confirmation
// instead, and target is the HX-Target header, empty for a navigation.
func (f *formFixture) photoPage(t *testing.T, plantID, photoID uuid.UUID, asking bool, target string) *httptest.ResponseRecorder {
	t.Helper()

	path := photoPath(plantID, photoID)
	if asking {
		path = deletePhotoPath(plantID, photoID)
	}
	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	req.SetPathValue("plant", plantID.String())
	req.SetPathValue("photo", photoID.String())
	if target != "" {
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", target)
	}
	rec := httptest.NewRecorder()
	if asking {
		f.handler.confirmDelete(rec, req)
	} else {
		f.handler.photo(rec, req)
	}
	return rec
}

func (f *formFixture) deletePhoto(t *testing.T, plantID, photoID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, deletePhotoPath(plantID, photoID), nil)
	req.SetPathValue("plant", plantID.String())
	req.SetPathValue("photo", photoID.String())
	rec := httptest.NewRecorder()
	f.handler.deletePhoto(rec, req)
	return rec
}

// withRavi adds a second member, so a photo can be somebody else's.
func (f *formFixture) withRavi(t *testing.T) {
	t.Helper()

	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ravi', 'ravi', 'Europe/London')", raviID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'member', 8)", rosewoodID, raviID)
}

func (f *formFixture) photoExists(t *testing.T, photoID uuid.UUID) bool {
	t.Helper()

	_, err := store.New(f.tx).GetPhoto(t.Context(), rosewoodID, photoID)
	return err == nil
}

func TestPhoto_ThePageShowsThePhotoWhenItWasUploadedAndByWhom(t *testing.T) {
	f := plantFormOn(t)
	f.withRavi(t)
	photoID := f.storedPhoto(t, bigFellaID, raviID, thursday.AddDate(0, 0, -1))

	rec := f.photoPage(t, bigFellaID, photoID, false, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	if got, want := images(page), []string{photoFullPath(bigFellaID, photoID)}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("the page's images are %v, want %v", got, want)
	}
	if !strings.Contains(text(page), "Uploaded yesterday by Ravi") {
		t.Errorf("the page does not say when and by whom:\n%s", text(page))
	}
}

func TestPhoto_TheReadersOwnUploadSaysByYou(t *testing.T) {
	f := plantFormOn(t)
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)

	page := f.photoPage(t, bigFellaID, photoID, false, "").Body.String()

	if !strings.Contains(text(page), "Uploaded today by you") {
		t.Errorf("the page does not say Uploaded today by you:\n%s", text(page))
	}
}

func TestPhoto_APhotoWithATakenDateSaysTakenAndItsYearWhenNotThisYear(t *testing.T) {
	f := plantFormOn(t)
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)
	f.exec(t, "UPDATE photo SET taken_at = $1 WHERE id = $2", thursday.AddDate(-1, 0, 0), photoID)

	page := f.photoPage(t, bigFellaID, photoID, false, "").Body.String()

	if !strings.Contains(text(page), "Taken 3 Sep 2025 by you") {
		t.Errorf("the page does not say Taken 3 Sep 2025:\n%s", text(page))
	}
}

func TestPhoto_DeleteIsShownToTheUploaderWithDeleteOwn(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteOwn] = true
	f.withRavi(t)
	own := f.storedPhoto(t, bigFellaID, readerID, thursday)
	theirs := f.storedPhoto(t, bigFellaID, raviID, thursday)

	if page := f.photoPage(t, bigFellaID, own, false, "").Body.String(); !strings.Contains(page, deletePhotoPath(bigFellaID, own)) {
		t.Error("the reader's own photo has no Delete")
	}
	if page := f.photoPage(t, bigFellaID, theirs, false, "").Body.String(); strings.Contains(page, deletePhotoPath(bigFellaID, theirs)) {
		t.Error("another member's photo offers Delete to a reader with delete_own alone")
	}
}

func TestPhoto_DeleteIsShownOnAnyPhotoWithDeleteAny(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteAny] = true
	f.withRavi(t)
	theirs := f.storedPhoto(t, bigFellaID, raviID, thursday)

	page := f.photoPage(t, bigFellaID, theirs, false, "").Body.String()

	if !strings.Contains(page, deletePhotoPath(bigFellaID, theirs)) {
		t.Error("another member's photo has no Delete for a reader with delete_any")
	}
}

func TestPhoto_ASitterSeesNoDelete(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)

	page := f.photoPage(t, bigFellaID, photoID, false, "").Body.String()

	if strings.Contains(page, "Delete") {
		t.Error("a sitter's photo page offers Delete")
	}
}

func TestPhoto_DeleteAsksFirstAndAnHTMXRequestGetsTheFootAlone(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteOwn] = true
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)

	whole := f.photoPage(t, bigFellaID, photoID, true, "")
	foot := f.photoPage(t, bigFellaID, photoID, true, photoFootID)

	if !strings.Contains(text(whole.Body.String()), "Delete this photo? This can’t be undone. Cancel Delete") {
		t.Errorf("the page does not ask:\n%s", text(whole.Body.String()))
	}
	if !strings.Contains(whole.Body.String(), "<html") {
		t.Error("a navigation got the foot alone, want the whole page")
	}
	if strings.Contains(foot.Body.String(), "<html") || !strings.Contains(foot.Body.String(), `id="photo-foot"`) {
		t.Error("the htmx request did not get the foot alone")
	}
	if f.photoExists(t, photoID) == false {
		t.Error("asking deleted the photo")
	}
}

func TestPhoto_TheConfirmationIs404ForAReaderWhoMayNotDelete(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteOwn] = true
	f.withRavi(t)
	theirs := f.storedPhoto(t, bigFellaID, raviID, thursday)

	rec := f.photoPage(t, bigFellaID, theirs, true, "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPhoto_DeletingRemovesTheRowAndBothFilesAndGoesToTheGrid(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteOwn] = true
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)
	if got := f.storedFiles(t); len(got) != 2 {
		t.Fatalf("the directory holds %v before the delete, want the photo and its square", got)
	}

	rec := f.deletePhoto(t, bigFellaID, photoID)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != photosPath(bigFellaID) {
		t.Errorf("the delete answered %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, photosPath(bigFellaID))
	}
	if f.photoExists(t, photoID) {
		t.Error("the photo row is still there")
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPhoto_DeletingAnotherMembersPhotoWithDeleteOwnIs404AndRemovesNothing(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteOwn] = true
	f.withRavi(t)
	theirs := f.storedPhoto(t, bigFellaID, raviID, thursday)

	rec := f.deletePhoto(t, bigFellaID, theirs)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !f.photoExists(t, theirs) {
		t.Error("the photo row is gone")
	}
	if got := f.storedFiles(t); len(got) != 2 {
		t.Errorf("the directory holds %v, want the photo and its square", got)
	}
}

func TestPhoto_DeletingThePlantsPictureLeavesThePlantWithNoPicture(t *testing.T) {
	f := plantFormOn(t)
	f.principal.Capabilities[auth.PhotoDeleteOwn] = true
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)
	f.exec(t, "UPDATE plant SET profile_photo_id = $1 WHERE id = $2", photoID, bigFellaID)

	f.deletePhoto(t, bigFellaID, photoID)

	if got := f.pictureOf(t, bigFellaID); got != nil {
		t.Errorf("the plant's picture is %s, want none", got)
	}
}

func TestPhoto_APhotoPageUnderAnotherPlantsURLIs404(t *testing.T) {
	f := plantFormOn(t)
	photoID := f.storedPhoto(t, bigFellaID, readerID, thursday)

	rec := f.photoPage(t, dorisID, photoID, false, "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
