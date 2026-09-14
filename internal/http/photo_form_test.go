package http

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
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
	doc := readHTML(rec.Body.String())
	if !doc.byID("photo-field").has("hidden") || doc.byID("photo-unsupported") == nil {
		t.Error("the page lacks the hidden photo field or the line for a browser that cannot resize")
	}
	if got := doc.first(isTag("a"), textIs("Cancel")).attr("href"); got != plantPath(bigFellaID) {
		t.Errorf("Cancel points at %q, want the plant's page", got)
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

// photoRefusals holds every message the photo field shows when a post is
// refused.
var photoRefusals = []string{"Choose a photo.", photoQuotaFull, photoNotImage, plainText(tooLargeTitle, tooLargeLine)}

// refusedWith fails the test unless the page shows want and no other message
// in photoRefusals.
func refusedWith(t *testing.T, page, want string) {
	t.Helper()

	shown := paragraphsShown(page)
	if !slices.Contains(shown, want) {
		t.Errorf("the page does not say %q. It says %q", want, shown)
	}
	for _, other := range photoRefusals {
		if other != want && slices.Contains(shown, other) {
			t.Errorf("the page says %q as well as %q", other, want)
		}
	}
}

func TestPhotoForm_APostWithNoPhotoSaysChooseAPhotoFirst(t *testing.T) {
	f := plantFormOn(t)

	rec := f.addPhoto(t, bigFellaID, nil, nil)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	refusedWith(t, rec.Body.String(), "Choose a photo.")
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
	refusedWith(t, rec.Body.String(), photoQuotaFull)
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
	refusedWith(t, rec.Body.String(), photoQuotaFull)
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

	if readHTML(page).first(attrIs("name", "focus")) != nil {
		t.Error("Add a photo has a focus field, want none, because a progress photo is not cropped")
	}
}

func TestPhotoForm_AFileThatIsNotAPhotoIsRefusedWithTheReasonUnderTheFieldAndNothingIsWritten(t *testing.T) {
	f := plantFormOn(t)

	rec := f.addPhoto(t, bigFellaID, []byte("not a photo"), testJPEG(t, 8, 8))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	refusedWith(t, rec.Body.String(), photoNotImage)
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
	refusedWith(t, rec.Body.String(), plainText(tooLargeTitle, tooLargeLine))
	if got := f.storedFiles(t); len(got) != 0 {
		t.Errorf("the directory holds %v, want nothing", got)
	}
}

// storedPhotoIDs returns the ids of every photo in the garden, oldest first.
func (f *formFixture) storedPhotoIDs(t *testing.T) []uuid.UUID {
	t.Helper()

	rows, err := f.tx.Query(t.Context(), "SELECT id FROM photo WHERE garden_id = $1 ORDER BY uploaded_at, id", rosewoodID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestPhotoForm_AnUploadThatFillsNinetyPercentOfTheQuotaTellsTheOwnerOnceUntilRoomIsMadeAgain(t *testing.T) {
	f := plantFormOn(t)
	f.withRavi(t)
	got := captureUserNotifications(&f.handler.notify)
	big, small := testJPEG(t, 3000, 2000), testJPEG(t, 8, 8)
	// The big photo alone is over nine tenths of the quota, and the small one
	// still fits after it.
	f.photosWithRoomFor(t, len(big)+len(small)+len(small)+len(small))
	if float64(len(big)+len(small)) < nearlyFull*float64(len(big)+3*len(small)) {
		t.Fatal("the fixture's big photo does not reach nearly full on its own")
	}

	if rec := f.addPhoto(t, bigFellaID, big, small); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first upload: status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if rec := f.addPhoto(t, bigFellaID, small, small); rec.Code != http.StatusSeeOther {
		t.Fatalf("the second upload: status = %d:\n%s", rec.Code, rec.Body.String())
	}

	// Ravi is a member and cannot delete other people's photos, so only Ellie
	// is told. Only the upload that crossed the line sends anything.
	if len(*got) != 1 || (*got)[0].user.ID != readerID || (*got)[0].n.URL != photosPath(bigFellaID) {
		t.Fatalf("notified %+v, want Ellie once, with the notification opening Big Fella's photos", *got)
	}

	// Deleting the big photo takes the garden back under the line. The next
	// upload past it is sent for again.
	if rec := f.deletePhoto(t, bigFellaID, f.storedPhotoIDs(t)[0]); rec.Code != http.StatusSeeOther {
		t.Fatalf("the delete: status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if rec := f.addPhoto(t, bigFellaID, big, small); rec.Code != http.StatusSeeOther {
		t.Fatalf("the third upload: status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if len(*got) != 2 {
		t.Errorf("notified %+v, want Ellie a second time after room was made and used up again", *got)
	}
}

func TestPhotoForm_AnUploadThatLeavesRoomTellsNobody(t *testing.T) {
	f := plantFormOn(t)
	got := captureUserNotifications(&f.handler.notify)
	image, square := testJPEG(t, 30, 20), testJPEG(t, 8, 8)
	f.photosWithRoomFor(t, 4*(len(image)+len(square)))

	if rec := f.addPhoto(t, bigFellaID, image, square); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	if len(*got) != 0 {
		t.Errorf("notified %+v, want nobody: the garden is a quarter full", *got)
	}
}

func TestPlantForm_APhotoOnTheEditFormThatFillsTheGardenTellsTheOwner(t *testing.T) {
	f := plantFormOn(t)
	got := captureUserNotifications(&f.handler.notify)
	values := addValues()
	values.Set("nickname", "Big Fella")
	image, square := testJPEG(t, 3000, 2000), testJPEG(t, 8, 8)
	f.photosWithRoomFor(t, len(image)+len(square))
	id := bigFellaID

	if rec := f.postPhoto(t, &id, values, image, square); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	if len(*got) != 1 || (*got)[0].user.ID != readerID {
		t.Errorf("notified %+v, want Ellie once", *got)
	}
}

func TestPlantForm_APhotoOnTheAddFormThatFillsTheGardenTellsTheOwner(t *testing.T) {
	f := plantFormOn(t)
	got := captureUserNotifications(&f.handler.notify)
	values := addValues()
	values.Set("nickname", "Ada")
	image, square := testJPEG(t, 3000, 2000), testJPEG(t, 8, 8)
	f.photosWithRoomFor(t, len(image)+len(square))

	if rec := f.postPhoto(t, nil, values, image, square); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d:\n%s", rec.Code, rec.Body.String())
	}

	if len(*got) != 1 || (*got)[0].user.ID != readerID {
		t.Errorf("notified %+v, want Ellie once", *got)
	}
}
