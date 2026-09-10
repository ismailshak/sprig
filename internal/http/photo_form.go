package http

import (
	"net/http"

	"github.com/ismailshak/sprig/internal/store"
)

// photoFormOverhead is the allowance for what a post to Add a photo holds
// besides the two files: the multipart boundaries, each part's headers and the
// filenames. Those run to a few hundred bytes, so a body longer than the room
// left plus 4 KB cannot hold files that fit.
const photoFormOverhead = 4 << 10

// photoFormPage is the Add a photo page.
type photoFormPage struct {
	// Name is the plant's name, shown in the title.
	Name string
	// Back is the URL the Cancel link points at. It goes back to the plant's
	// page.
	Back   string
	Action string
	// PhotoRefusal is the sentence under the photo field saying why the store
	// refused the photo. It is empty otherwise.
	PhotoRefusal string
	// PhotoMissing is true when the post had no photo.
	PhotoMissing bool
}

// PhotoField is the photo field's data for the template.
func (p photoFormPage) PhotoField() photoField {
	return photoField{
		Choose:  "Choose photo",
		Missing: p.PhotoMissing,
		Refusal: p.PhotoRefusal,
		Submit:  "Add photo",
	}
}

func (h *plants) newPhotoPage(plant store.Plant) photoFormPage {
	return photoFormPage{Name: plant.DisplayName(), Back: plantPath(plant.ID), Action: newPhotoPath(plant.ID)}
}

// newPhoto handles GET /plants/{plant}/photos/new.
func (h *plants) newPhoto(w http.ResponseWriter, r *http.Request) {
	plant, ok := h.editable(w, r, PrincipalFrom(r))
	if !ok {
		return
	}
	h.templates.render(w, r, view{page: "photo-new"}, h.newPhotoPage(plant))
}

// addPhoto handles POST /plants/{plant}/photos/new and redirects to the
// plant's Photos page, where the new photo is first.
func (h *plants) addPhoto(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plant, ok := h.editable(w, r, principal)
	if !ok {
		return
	}
	page := h.newPhotoPage(plant)
	// The body is the photo, so a declared length over the room left is
	// refused before any of it is read. A chunked body declares no length and
	// is checked as it is written instead.
	usage, err := h.photos.Usage(r.Context(), h.queries, principal.Garden.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "sum the garden's photos", err)
		return
	}
	if r.ContentLength > usage.Remaining()+photoFormOverhead {
		page.PhotoRefusal = photoQuotaFull
		h.templates.render(w, r, view{page: "photo-new", status: http.StatusRequestEntityTooLarge}, page)
		return
	}
	if !readMultipartForm(h.templates, w, r) {
		return
	}
	if !photoPosted(r) {
		page.PhotoMissing = true
		h.templates.render(w, r, view{page: "photo-new", status: http.StatusUnprocessableEntity}, page)
		return
	}
	upload, closeFiles, ok := postedPhoto(h.templates, w, r, principal, plant.ID)
	defer closeFiles()
	if !ok {
		return
	}
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		_, err := h.photos.Save(r.Context(), q, upload)
		return err
	})
	if status, message := photoRefusal(err); message != "" {
		page.PhotoRefusal = message
		h.templates.render(w, r, view{page: "photo-new", status: status}, page)
		return
	}
	if err != nil {
		h.templates.serverError(h.logger, w, r, "add the photo", err)
		return
	}
	h.notifyStorage(r.Context(), principal, plant)
	http.Redirect(w, r, photosPath(plant.ID), http.StatusSeeOther)
}
