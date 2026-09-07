package http

import (
	"errors"
	"net/http"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// photoFullPath is the URL of a photo's file at its uploaded size.
func photoFullPath(plantID, photoID uuid.UUID) string {
	return plantPath(plantID) + "/photos/" + photoID.String() + "/full"
}

// photoSquarePath is the URL of a photo's square variant.
func photoSquarePath(plantID, photoID uuid.UUID) string {
	return plantPath(plantID) + "/photos/" + photoID.String() + "/square"
}

// picturePath is the URL of the plant's profile picture at its uploaded size.
// Empty for a plant with no picture.
func picturePath(plant store.Plant) string {
	if plant.ProfilePhotoID == nil {
		return ""
	}
	return photoFullPath(plant.ID, *plant.ProfilePhotoID)
}

// squarePicturePath is the URL of the square variant of the plant's profile
// picture, shown beside its name in a row. Empty for a plant with no picture.
// A picture always has a square, because a photo without one cannot be set as
// the picture.
func squarePicturePath(plant store.Plant) string {
	if plant.ProfilePhotoID == nil {
		return ""
	}
	return photoSquarePath(plant.ID, *plant.ProfilePhotoID)
}

// photoFull handles GET /plants/{plant}/photos/{photo}/full.
func (h *plants) photoFull(w http.ResponseWriter, r *http.Request) {
	h.servePhoto(w, r, false)
}

// photoSquare handles GET /plants/{plant}/photos/{photo}/square. A photo
// uploaded without a square variant is a 404.
func (h *plants) photoSquare(w http.ResponseWriter, r *http.Request) {
	h.servePhoto(w, r, true)
}

// servePhoto loads the photo row in the session's garden, then opens its file.
// A photo in another garden, or under another plant's URL, is a 404. The path
// opened comes from the row and never from the URL.
func (h *plants) servePhoto(w http.ResponseWriter, r *http.Request, square bool) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	photoID, err := uuid.Parse(r.PathValue("photo"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	row, err := h.queries.GetPhoto(r.Context(), principal.Garden.ID, photoID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		serverError(h.logger, w, r, "load the photo", err)
		return
	}
	if row.PlantID != plantID || (square && row.SquareBytes == nil) {
		http.NotFound(w, r)
		return
	}
	if err := h.photos.Serve(w, r, row, square); err != nil {
		serverError(h.logger, w, r, "serve the photo", err)
	}
}
