package http

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// photoFootID is the HTML id of the section holding the Delete button on a
// photo's page. The template that renders that section has the same name, so
// the swap target and the fragment are one string.
const photoFootID = "photo-foot"

type photoPage struct {
	// Name is the plant's name, used in the title and the image's alt text.
	Name string
	// Back is the URL of the plant's Photos page.
	Back string
	Src  string
	// Width and Height are the photo's pixel size, written on the img so the
	// page lays the box out at its final shape before the photo downloads.
	Width  int32
	Height int32
	// When is the day the photo was taken, or uploaded when the uploader did
	// not say, as "Taken 3 Sep" or "Uploaded today".
	When string
	// Who is the uploader's name, or "you" when the reader uploaded it.
	Who string
	// Foot is nil for a reader who may not delete this photo.
	Foot *photoFoot
}

// photoFoot is the Delete button at the bottom of a photo's page, or the
// confirmation that replaces it.
type photoFoot struct {
	Name string
	// Delete is the URL for deleting. A GET renders the confirmation and a
	// POST deletes.
	Delete string
	// Asking is true while the confirmation replaces the button. Keep is the
	// URL the confirmation's Cancel button goes to, the photo's page with the
	// Delete button back.
	Asking bool
	Keep   string
}

// mayDeletePhoto reports whether the page shows Delete. It repeats the delete
// query's own test, so the button and the 404 agree. Both delete routes
// require photo.delete_own at the mux, so a role granted photo.delete_any is
// expected to hold that one too.
func mayDeletePhoto(principal auth.Principal, p store.Photo) bool {
	return principal.Can(auth.PhotoDeleteAny) ||
		(p.UploadedBy == principal.User.ID && principal.Can(auth.PhotoDeleteOwn))
}

// photo handles GET /plants/{plant}/photos/{photo}.
func (h *plants) photo(w http.ResponseWriter, r *http.Request) {
	h.renderPhoto(w, r, false)
}

// confirmDelete handles GET /plants/{plant}/photos/{photo}/delete. It renders
// the photo's page with the delete confirmation at the bottom. An htmx
// request gets the bottom section alone, since that is the only element that
// changes.
func (h *plants) confirmDelete(w http.ResponseWriter, r *http.Request) {
	h.renderPhoto(w, r, true)
}

func (h *plants) renderPhoto(w http.ResponseWriter, r *http.Request, asking bool) {
	principal := PrincipalFrom(r)
	plant, row, ok := h.resolvePhoto(w, r, principal)
	if !ok {
		return
	}
	now := h.now().In(locationFor(principal.User))
	page := photoPage{
		Name:   plant.DisplayName(),
		Back:   photosPath(plant.ID),
		Src:    photoFullPath(plant.ID, row.Photo.ID),
		Width:  row.Photo.Width,
		Height: row.Photo.Height,
		When:   photoWhen(row.Photo, now),
		Who:    row.UploadedByName,
	}
	if row.Photo.UploadedBy == principal.User.ID {
		page.Who = "you"
	}
	if mayDeletePhoto(principal, row.Photo) {
		page.Foot = &photoFoot{
			Name:   plant.DisplayName(),
			Delete: deletePhotoPath(plant.ID, row.Photo.ID),
			Asking: asking,
			Keep:   photoPath(plant.ID, row.Photo.ID),
		}
	}
	// A reader who may not delete the photo has no confirmation to render.
	if asking && page.Foot == nil {
		h.templates.notFound(w, r)
		return
	}
	v := view{page: "photo"}
	if r.Header.Get("HX-Target") == photoFootID {
		v.fragment = photoFootID
	}
	if asking {
		v.announce = "Delete this photo? This can’t be undone. Cancel or Delete."
	}
	h.templates.render(w, r, v, page)
}

// photoWhen is the first half of the line under a photo, "Taken 3 Sep" or
// "Uploaded today".
func photoWhen(p store.Photo, now time.Time) string {
	if p.TakenAt != nil {
		return "Taken " + photoDateWord(p, now)
	}
	return "Uploaded " + photoDateWord(p, now)
}

// deletePhoto handles POST /plants/{plant}/photos/{photo}/delete and
// redirects to the plant's Photos page. A photo the reader may not delete is
// a 404, the same as one that does not exist.
func (h *plants) deletePhoto(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plant, row, ok := h.resolvePhoto(w, r, principal)
	if !ok {
		return
	}
	err := h.photos.Delete(r.Context(), h.queries, store.DeletePhotoParams{
		GardenID:     principal.Garden.ID,
		PlantID:      plant.ID,
		PhotoID:      row.Photo.ID,
		MayDeleteAny: principal.Can(auth.PhotoDeleteAny),
		UploadedBy:   principal.User.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "delete the photo", err)
		return
	}
	http.Redirect(w, r, photosPath(plant.ID), http.StatusSeeOther)
}

// resolvePhoto reads the plant and the photo in the URL. A photo in another
// garden, or under another plant's URL, is a 404.
func (h *plants) resolvePhoto(w http.ResponseWriter, r *http.Request, principal auth.Principal) (store.Plant, store.GetPlantPhotoRow, bool) {
	plant, ok := h.resolvePlant(w, r, principal)
	if !ok {
		return store.Plant{}, store.GetPlantPhotoRow{}, false
	}
	photoID, err := uuid.Parse(r.PathValue("photo"))
	if err != nil {
		h.templates.notFound(w, r)
		return store.Plant{}, store.GetPlantPhotoRow{}, false
	}
	row, err := h.queries.GetPlantPhoto(r.Context(), store.GetPlantPhotoParams{GardenID: principal.Garden.ID, PlantID: plant.ID, PhotoID: photoID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return store.Plant{}, store.GetPlantPhotoRow{}, false
	case err != nil:
		h.templates.serverError(h.logger, w, r, "load the photo", err)
		return store.Plant{}, store.GetPlantPhotoRow{}, false
	}
	return plant, row, true
}
