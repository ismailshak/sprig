package http

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
)

func (f *formFixture) addPhotoPage(t *testing.T, plantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, newPhotoPath(plantID), nil)
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.newPhoto(rec, req)
	return rec
}

// addPhoto posts image and its square to Add a photo. A nil image leaves the
// photo part out, the post a browser makes with nothing chosen.
func (f *formFixture) addPhoto(t *testing.T, plantID uuid.UUID, image, square []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
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
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, newPhotoPath(plantID), &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.SetPathValue("plant", plantID.String())
	rec := httptest.NewRecorder()
	f.handler.addPhoto(rec, req)
	return rec
}

func TestPhotoForm_ThePageHasTheFieldAndTheLineForABrowserThatCannotResize(t *testing.T) {
	f := plantFormOn(t)

	rec := f.addPhotoPage(t, bigFellaID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	if !strings.Contains(page, `id="photo-field" hidden`) || !strings.Contains(page, `id="photo-unsupported"`) {
		t.Error("the page lacks the hidden photo field or the line for a browser that cannot resize")
	}
	if !strings.Contains(page, `href="`+plantPath(bigFellaID)+`">Cancel</a>`) {
		t.Error("Cancel does not go back to the plant's page")
	}
}

func TestPhotoForm_APostedPhotoIsStoredAndThePostGoesToTheGrid(t *testing.T) {
	f := plantFormOn(t)
	image, square := testJPEG(t, 30, 20), testJPEG(t, 8, 8)

	rec := f.addPhoto(t, bigFellaID, image, square)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != photosPath(bigFellaID) {
		t.Fatalf("the post answered %d to %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, photosPath(bigFellaID), rec.Body.String())
	}
	row := f.photoOf(t, bigFellaID)
	if row.UploadedBy != readerID || row.Bytes != int64(len(image)) || row.SquareBytes == nil {
		t.Errorf("the row is by %s with %d bytes and square %v, want the reader's %d bytes with a square", row.UploadedBy, row.Bytes, row.SquareBytes, len(image))
	}
	if got := f.storedFiles(t); len(got) != 2 {
		t.Errorf("the directory holds %v, want the photo and its square", got)
	}
	if got := f.pictureOf(t, bigFellaID); got != nil {
		t.Error("adding a photo made it the plant's picture")
	}
}

func TestPhotoForm_APostWithNoPhotoSaysChooseAPhotoFirst(t *testing.T) {
	f := plantFormOn(t)

	rec := f.addPhoto(t, bigFellaID, nil, nil)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if got := errorsOn(rec.Body.String()); len(got) != 1 || got[0] != "Choose a photo." {
		t.Errorf("the form's errors are %v, want Choose a photo.", got)
	}
}

func TestPhotoForm_ABodyLongerThanTheRoomLeftIs413WithTheReasonBeforeItIsRead(t *testing.T) {
	f := plantFormOn(t)
	f.photosWithRoomFor(t, 100)
	image := testJPEG(t, 3000, 2000)
	if len(image) <= photoFormOverhead {
		t.Fatalf("the image is %d bytes, want more than the %d the handler allows for the form's overhead", len(image), photoFormOverhead)
	}

	rec := f.addPhoto(t, bigFellaID, image, testJPEG(t, 8, 8))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := errorsOn(rec.Body.String()); len(got) != 1 || got[0] != photoQuotaFull {
		t.Errorf("the form's errors are %v, want %q", got, photoQuotaFull)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPhotoForm_APhotoTheGardenHasNoRoomForIsRefusedWithTheReasonAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)
	image, square := testJPEG(t, 30, 20), testJPEG(t, 8, 8)
	// Within the overhead the declared length is allowed, so the refusal
	// comes from the store after the body is read.
	f.photosWithRoomFor(t, len(image)+len(square)-1)

	rec := f.addPhoto(t, bigFellaID, image, square)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if got := errorsOn(rec.Body.String()); len(got) != 1 || got[0] != photoQuotaFull {
		t.Errorf("the form's errors are %v, want %q", got, photoQuotaFull)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPhotoForm_AnArchivedPlantIs404(t *testing.T) {
	f := plantFormOn(t)
	f.exec(t, "UPDATE plant SET archived_at = now() WHERE id = $1", bigFellaID)

	if rec := f.addPhotoPage(t, bigFellaID); rec.Code != http.StatusNotFound {
		t.Errorf("the page answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	if rec := f.addPhoto(t, bigFellaID, testJPEG(t, 30, 20), testJPEG(t, 8, 8)); rec.Code != http.StatusNotFound {
		t.Errorf("the post answered %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPhotoForm_APhotoWithoutItsSquareIs400AndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)

	rec := f.addPhoto(t, bigFellaID, testJPEG(t, 30, 20), nil)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPhotoForm_ThePostIsBoundToTheSessionsGarden(t *testing.T) {
	f := plantFormOn(t)
	f.principal = memberWith(auth.Capabilities{auth.PhotoAdd: true})

	rec := f.addPhoto(t, bigFellaID, testJPEG(t, 30, 20), testJPEG(t, 8, 8))

	if rec.Code != http.StatusNotFound {
		t.Errorf("a member of another garden got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPhotoForm_AddAPhotoHasNoFocusField(t *testing.T) {
	f := plantFormOn(t)

	page := f.addPhotoPage(t, bigFellaID).Body.String()

	if strings.Contains(page, `name="focus"`) {
		t.Error("Add a photo has a focus field, want none, because a progress photo is not cropped")
	}
}

func TestPhotoForm_AFileThatIsNotAPhotoIsRefusedWithTheReasonUnderTheFieldAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)

	rec := f.addPhoto(t, bigFellaID, []byte("not a photo"), testJPEG(t, 8, 8))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if got := errorsOn(rec.Body.String()); len(got) != 1 || got[0] != photoNotImage {
		t.Errorf("the form's errors are %v, want %q", got, photoNotImage)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

func TestPhotoForm_APhotoOverTheFileLimitIsRefusedWithTheLimitUnderTheField(t *testing.T) {
	f := plantFormOn(t)
	image := append(testJPEG(t, 30, 20), make([]byte, photo.MaxBytes)...)

	rec := f.addPhoto(t, bigFellaID, image, testJPEG(t, 8, 8))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if got := errorsOn(rec.Body.String()); len(got) != 1 || got[0] != plainText(tooLargeTitle, tooLargeLine) {
		t.Errorf("the form's errors are %v, want the size limit", got)
	}
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}
