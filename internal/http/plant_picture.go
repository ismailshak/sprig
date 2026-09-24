package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

// photoField is the data the photo-field template renders. Its fields are
// where the plant form and the Add photo page differ.
type photoField struct {
	// Choose is the label on the button that opens the file chooser: "Add
	// photo" on the plant form and "Choose photo" on the Add photo page.
	Choose string
	// Picture is the URL of the plant's current profile picture, shown in the
	// preview when the plant form opens. It is empty on the Add photo page,
	// because that page does not set the picture.
	Picture string
	// Removable is true when the field renders the hidden photo-removed input.
	// It is false on the Add photo page, because that page does not clear the
	// picture. Removed is the input's value.
	Removable bool
	Removed   bool
	// Focus is the value of the hidden focus input, as "x,y" in percentages.
	// An empty Focus renders no input. It is empty on the Add photo page,
	// because a progress photo is never cropped.
	Focus string
	// NeedsChoosing renders "Choose the photo again."
	NeedsChoosing bool
	// Missing renders "Choose a photo."
	Missing bool
	// Refusal is the sentence under the field saying why the store refused
	// the photo. It is empty otherwise.
	Refusal string
	// Submit is the label on a submit button inside the field. It is empty on
	// the plant form, because that form has its own. The Add photo page sets it
	// so the button stays hidden with the field in a browser that cannot
	// resize a photo.
	Submit string
}

// formMaxBytes is the largest post accepted on the plant form and the Add
// photo page, the two photo files included.
const formMaxBytes = 10 << 20

// readMultipartForm parses a post into r.PostForm, whether it is encoded as
// multipart/form-data or as a query string. It writes the response itself and
// returns false when the body is over formMaxBytes or does not parse.
func readMultipartForm(templates *Templates, w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, formMaxBytes)
	// ParseForm runs first because ParseMultipartForm discards its error and
	// returns ErrNotMultipart when the post is a query string.
	err := r.ParseForm()
	if err == nil {
		if err = r.ParseMultipartForm(formMaxBytes); errors.Is(err, http.ErrNotMultipart) {
			err = nil
		}
	}
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		templates.tooLarge(w, r)
		return false
	case err != nil:
		templates.badRequest(w, r)
		return false
	}
	return true
}

// photoPosted reports whether the post had a photo file. Go parses a part
// with an empty filename as a form value rather than a file, so the part a
// browser sends for a file input with nothing chosen does not count.
func photoPosted(r *http.Request) bool {
	return r.MultipartForm != nil && len(r.MultipartForm.File["photo"]) > 0
}

// canSetPicture reports whether the member may use the plant form's photo
// field. The field stores a photo and makes it the plant's profile picture, so
// it takes both capabilities.
func canSetPicture(principal auth.Principal) bool {
	return principal.Can(auth.PhotoAdd) && principal.Can(auth.PhotoSetProfile)
}

// postedPhoto builds an upload for the plant from the posted photo and
// photo-square files. closeFiles closes whatever was opened and is never nil.
// It writes a 400 and returns false when a file will not open or the square is
// missing. The square is required because every list shows the picture as its
// square.
func postedPhoto(templates *Templates, w http.ResponseWriter, r *http.Request, principal auth.Principal, plantID uuid.UUID) (upload photo.Upload, closeFiles func(), ok bool) {
	var opened []io.Closer
	closeFiles = func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}
	upload = photo.Upload{GardenID: principal.Garden.ID, PlantID: plantID, UploadedBy: principal.User.ID}
	if len(r.MultipartForm.File["photo-square"]) == 0 {
		templates.badRequest(w, r)
		return upload, closeFiles, false
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		templates.badRequest(w, r)
		return upload, closeFiles, false
	}
	opened = append(opened, file)
	upload.File, upload.Size = file, header.Size
	square, header, err := r.FormFile("photo-square")
	if err != nil {
		templates.badRequest(w, r)
		return upload, closeFiles, false
	}
	opened = append(opened, square)
	upload.Square, upload.SquareSize = square, header.Size
	return upload, closeFiles, true
}

// savePicture stores the photo, makes it the plant's profile picture and
// records which part of it the plant's page shows. Every write goes through
// q, so they join the caller's transaction.
func (h *plants) savePicture(ctx context.Context, q *store.Queries, upload photo.Upload, focus focusPoint) error {
	row, err := h.photos.Save(ctx, q, upload)
	if err != nil {
		return err
	}
	if _, err := q.SetProfilePhoto(ctx, store.SetProfilePhotoParams{PhotoID: &row.ID, GardenID: upload.GardenID, PlantID: upload.PlantID}); err != nil {
		return err
	}
	return q.SetPhotoFocus(ctx, store.SetPhotoFocusParams{FocusX: focus.x, FocusY: focus.y, GardenID: upload.GardenID, PlantID: upload.PlantID, PhotoID: row.ID})
}

// focusPoint is which part of the picture the plant's page shows, as
// percentages across and down. 50 and 50 is the centre.
type focusPoint struct {
	x, y int16
}

var centre = focusPoint{50, 50}

// String returns the point as "x,y" in percentages, the value the form's focus
// field holds.
func (f focusPoint) String() string {
	return fmt.Sprintf("%d,%d", f.x, f.y)
}

// postedFocus reads the form's focus field. posted is false when the post had
// no such field. A member who may not set the picture posts none, because
// their form is rendered without the photo field. ok is false unless the value
// is two whole numbers from 0 to 100.
func postedFocus(r *http.Request) (focus focusPoint, posted, ok bool) {
	if !r.PostForm.Has("focus") {
		return centre, false, true
	}
	xs, ys, found := strings.Cut(r.PostForm.Get("focus"), ",")
	if !found {
		return focus, true, false
	}
	x, errX := strconv.ParseInt(xs, 10, 16)
	y, errY := strconv.ParseInt(ys, 10, 16)
	if errX != nil || errY != nil || x < 0 || x > 100 || y < 0 || y > 100 {
		return focus, true, false
	}
	return focusPoint{x: int16(x), y: int16(y)}, true, true
}

// pictureFocus returns the focal point stored for the plant's profile picture,
// for the edit form's hidden field. It returns the centre for a plant with no
// picture and for a picture whose row has since been deleted.
func (h *plants) pictureFocus(ctx context.Context, principal auth.Principal, plant store.Plant) (focusPoint, error) {
	if plant.ProfilePhotoID == nil {
		return centre, nil
	}
	picture, err := h.queries.GetPhoto(ctx, principal.Garden.ID, *plant.ProfilePhotoID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return centre, nil
	case err != nil:
		return focusPoint{}, err
	}
	return focusPoint{x: picture.FocusX, y: picture.FocusY}, nil
}

// pictureRemoved reports whether the post asks for the plant's profile picture
// to be cleared. Only the page's script sets the flag, because the photo field
// stays hidden without JavaScript.
func pictureRemoved(r *http.Request) bool {
	return r.PostForm.Get("photo-removed") == "1"
}

const (
	photoQuotaFull = "Photo storage is full. Delete some photos to make room."
	photoNotImage  = "The photo must be a JPEG or WebP."
)

// photoRefused reports whether err is a refusal from the store. When it is,
// the form is rendered again with the reason under the photo field. The
// caller reports any other error itself.
func (h *plants) photoRefused(w http.ResponseWriter, r *http.Request, err error, page plantFormPage) bool {
	status, message := photoRefusal(err)
	if message == "" {
		return false
	}
	page.PhotoRefusal = message
	h.templates.render(w, r, view{page: plantFormPageName, status: status}, page)
	return true
}

// photoRefusal returns the status and the sentence under the photo field for
// a photo the store refused, and an empty sentence for any other error. Only
// a post the app's pages did not make reaches the size or file type case,
// because the form's script re-encodes every photo it sends and the field
// stays hidden when no script is running.
func photoRefusal(err error) (int, string) {
	switch {
	case errors.Is(err, photo.ErrQuotaFull):
		return http.StatusUnprocessableEntity, photoQuotaFull
	case errors.Is(err, photo.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, plainText(tooLargeTitle, tooLargeLine)
	case errors.Is(err, photo.ErrNotImage):
		return http.StatusUnprocessableEntity, photoNotImage
	}
	return 0, ""
}
