package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
)

var (
	deletePlantID      = uuid.MustParse("00000000-0000-7000-8000-000000000330")
	deleteOtherPlantID = uuid.MustParse("00000000-0000-7000-8000-000000000331")
	deletePhotoID      = uuid.MustParse("00000000-0000-7000-8000-000000000332")
	deleteOtherPhotoID = uuid.MustParse("00000000-0000-7000-8000-000000000333")
)

// deletableGarden gives Rosewood and Fairview each a plant with a care type, a
// schedule, a care event, a token and a photo whose file is on disk, so the
// delete has one of everything to remove and one of everything to leave. It
// returns the directory the photo files are under.
func deletableGarden(t *testing.T) (*moreFixture, string) {
	t.Helper()

	f := moreGarden(t)
	f.principal.Capabilities[auth.GardenDelete] = true
	dir := t.TempDir()
	photos, err := photo.NewStore(dir, testPhotoQuota)
	if err != nil {
		t.Fatal(err)
	}
	f.handler.photos = photos

	for _, g := range []struct {
		garden, user, plant, photoID uuid.UUID
	}{
		{moreGardenID, moreUserID, deletePlantID, deletePhotoID},
		{otherGardenID, otherUserID, deleteOtherPlantID, deleteOtherPhotoID},
	} {
		f.exec(t, "INSERT INTO care_type (garden_id, name, slug) VALUES ($1, 'Water', 'water')", g.garden)
		f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern')", g.plant, g.garden)
		f.exec(t, `INSERT INTO care_schedule (garden_id, plant_id, care_type_id, interval_count, interval_unit)
			SELECT $1, $2, id, 7, 'day' FROM care_type WHERE garden_id = $1`, g.garden, g.plant)
		f.exec(t, `INSERT INTO care_event (garden_id, plant_id, care_type_id, performed_by, performed_at, done)
			SELECT $1, $2, id, $3, now(), true FROM care_type WHERE garden_id = $1`, g.garden, g.plant, g.user)
		f.exec(t, `INSERT INTO api_token (garden_id, name, token_hash, prefix, created_by, expires_at)
			VALUES ($1, 'Display', $2, 'sprg_0000', $3, now() + interval '30 days')`, g.garden, g.garden.String(), g.user)
		p := photo.Path(g.garden, g.plant, g.photoID, photo.JPEG)
		f.exec(t, `INSERT INTO photo (id, garden_id, plant_id, uploaded_by, kind, path, width, height, bytes)
			VALUES ($1, $2, $3, $4, 'image/jpeg', $5, 3, 2, 100)`, g.photoID, g.garden, g.plant, g.user, p)
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("jpeg"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A session on the garden, so the delete has one to set the garden to null on.
	f.exec(t, "INSERT INTO session (token_hash, user_id, garden_id, created_at, last_seen_at) VALUES ('owner-session', $1, $2, now(), now())", moreUserID, moreGardenID)
	return f, dir
}

func (f *moreFixture) deleteGarden(t *testing.T, typed string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, f.handler.deleteGarden, deleteGardenPath, url.Values{"name": {typed}})
}

func TestDeleteGarden_ThePageNamesTheGardenAndAsksForItsName(t *testing.T) {
	f, _ := deletableGarden(t)

	page := f.page(t, f.handler.confirmDeleteGarden, deleteGardenPath)

	if !strings.Contains(page, "Type Rosewood to confirm") {
		t.Errorf("the page does not ask for the garden's name:\n%s", text(page))
	}
	if !strings.Contains(page, "Deleting Rosewood removes its plants") {
		t.Errorf("the page does not say what goes:\n%s", text(page))
	}
}

func TestDeleteGarden_ANameThatIsNotTheGardensIsRefusedAndNothingIsDeleted(t *testing.T) {
	f, dir := deletableGarden(t)
	rec := f.do(t, f.handler.deleteGarden, deleteGardenPath, url.Values{"name": {"Rosewod"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if got := errorUnder(page, "name"); got != deleteGardenMismatch {
		t.Errorf("the message under the field is %q, want %q", got, deleteGardenMismatch)
	}
	if got := valueOf(t, page, "name"); got != "Rosewod" {
		t.Errorf("the field holds %q after the refusal, want what was typed", got)
	}
	if n := countRows(t, f.tx, "garden"); n != 2 {
		t.Errorf("%d gardens remain, want both", n)
	}
	if _, err := os.Stat(filepath.Join(dir, moreGardenID.String())); err != nil {
		t.Errorf("the garden's photo directory is gone after a refused post: %v", err)
	}
}

// Surrounding spaces are trimmed before the comparison.
func TestDeleteGarden_TheNameIsComparedAsTyped(t *testing.T) {
	f, _ := deletableGarden(t)

	if rec := f.do(t, f.handler.deleteGarden, deleteGardenPath, url.Values{"name": {"rosewood"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a lower-case name returned %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if rec := f.do(t, f.handler.deleteGarden, deleteGardenPath, url.Values{"name": {"  Rosewood "}}); rec.Code != http.StatusSeeOther {
		t.Errorf("the name with spaces around it returned %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestDeleteGarden_TheGardensRowsAndPhotoFilesGoAndAnotherGardensStay(t *testing.T) {
	f, dir := deletableGarden(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	rec := f.deleteGarden(t, "Rosewood")

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("deleting returned %d to %q, want %d to Today", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	for table, want := range map[string]int{
		"garden": 1, "membership": 1, "plant": 1, "care_type": 1, "care_schedule": 1,
		"care_event": 1, "photo": 1, "api_token": 1, "invite": 1, "notification_preference": 0,
	} {
		if n := countRows(t, f.tx, table); n != want {
			t.Errorf("%s holds %d rows after the delete, want %d, Fairview's alone", table, n, want)
		}
	}
	var fairview int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM garden WHERE id = $1", otherGardenID).Scan(&fairview); err != nil || fairview != 1 {
		t.Errorf("Fairview is gone: %v", err)
	}
	// The accounts and sessions stay, with the session no longer on the garden.
	if n := countRows(t, f.tx, "app_user"); n != 2 {
		t.Errorf("%d accounts remain, want both", n)
	}
	var onGarden *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT garden_id FROM session WHERE token_hash = 'owner-session'").Scan(&onGarden); err != nil {
		t.Fatalf("the owner's session is gone: %v", err)
	}
	if onGarden != nil {
		t.Error("the owner's session still names the deleted garden")
	}
	if _, err := os.Stat(filepath.Join(dir, moreGardenID.String())); !os.IsNotExist(err) {
		t.Errorf("the garden's photo directory is still on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(photo.Path(otherGardenID, deleteOtherPlantID, deleteOtherPhotoID, photo.JPEG)))); err != nil {
		t.Errorf("Fairview's photo file went with Rosewood's: %v", err)
	}
	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, since the garden's memberships were due digests", woken)
	}
}

func TestDeleteGarden_AGardenWithNoPhotosHasNoDirectoryAndDeletesAnyway(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities[auth.GardenDelete] = true

	if rec := f.deleteGarden(t, "Rosewood"); rec.Code != http.StatusSeeOther {
		t.Errorf("deleting returned %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if n := countRows(t, f.tx, "garden"); n != 1 {
		t.Errorf("%d gardens remain, want Fairview alone", n)
	}
}

func TestGarden_DeleteGardenIsLinkedOnlyForAReaderWithGardenDelete(t *testing.T) {
	f := moreGarden(t)

	if page := f.page(t, f.handler.garden, gardenPath); strings.Contains(page, deleteGardenPath) {
		t.Error("a reader without garden.delete is offered Delete garden")
	}
	f.principal.Capabilities[auth.GardenDelete] = true
	if page := f.page(t, f.handler.garden, gardenPath); !strings.Contains(page, `href="`+deleteGardenPath+`">Delete garden`) {
		t.Errorf("the owner's Garden page has no Delete garden link:\n%s", text(page))
	}
}
