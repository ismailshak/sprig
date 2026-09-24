package http

import (
	"errors"
	"net/http"
	"net/url"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// plantFormPageName is the template for both the add and edit routes, since
// they are the same fields in the same order.
const plantFormPageName = "plant-form"

const newPlantPath = plantsPath + "/new"

func editPlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/edit"
}

func archivePlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/archive"
}

type plantFormPage struct {
	Title string
	// Action is the URL the form posts to and a schedule row re-renders from.
	Action string
	// Back is the URL the Cancel link points at.
	Back      string
	Submit    string
	Nickname  string
	Common    string
	Botanical string
	// NameError is shown under all three name fields, since any one of them
	// satisfies the requirement.
	NameError string
	Room      string
	// Rooms fills the <datalist> under the Room field.
	Rooms []string
	// Schedules is empty on the edit form. Schedules are edited on the plant's
	// page, beside the due date they change.
	Schedules []scheduleField
	Facts     []factField
	Notes     string
	Months    []option
	Years     []option
	// AcquiredError is shown under the month and year selects.
	AcquiredError string
	// Details is true when the Details disclosure starts open.
	Details bool
	// PhotoNeedsChoosing is true when a refused post had a photo. The form is
	// rendered again with an empty file input, because a server cannot fill
	// one.
	PhotoNeedsChoosing bool
	// PhotoRefusal is the sentence under the photo field saying why the store
	// refused the photo. It is empty otherwise.
	PhotoRefusal string
	// showPhoto is whether the form renders the photo field. It is true only
	// for a member who may set the plant's profile picture.
	showPhoto bool
	// Picture is the URL of the plant's current profile picture, shown in the
	// photo field with Replace and Remove. Empty on the add form and for a
	// plant with no picture.
	Picture string
	// PictureRemoved is true when the post being rendered again had Remove
	// pressed. The form renders the hidden photo-removed input set to 1, so
	// saving again still removes the picture.
	PictureRemoved bool
	// PictureFocus is which part of the picture the plant's page shows, as
	// "x,y" in percentages. It is the centre on the add form, the stored pair
	// on the edit form, and the posted pair on a form rendered again after a
	// refused save.
	PictureFocus string
}

// PhotoField is the photo field's data for the template. It is nil for a form
// that does not render the field.
func (p plantFormPage) PhotoField() *photoField {
	if !p.showPhoto {
		return nil
	}
	return &photoField{
		Choose:        "Add photo",
		Picture:       p.Picture,
		Removable:     true,
		Removed:       p.PictureRemoved,
		Focus:         p.PictureFocus,
		NeedsChoosing: p.PhotoNeedsChoosing,
		Refusal:       p.PhotoRefusal,
	}
}

type factField struct {
	Name  string
	Label string
	Value string
}

func newPlantFormPage(f plantFields, now time.Time) plantFormPage {
	page := plantFormPage{
		Nickname:  f.nickname,
		Common:    f.common,
		Botanical: f.botanical,
		Room:      f.room,
		Notes:     f.notes,
		Months:    append([]option{{Value: "0", Label: "Month", On: f.month == 0}}, monthOptions(f.month)...),
		Years:     append([]option{{Value: "0", Label: "Year", On: f.year == 0}}, numberOptions(acquiredYears(now), f.year)...),
		Details:   f.notes != "" || f.month != 0 || f.year != 0,
	}
	for i, held := range f.facts() {
		page.Facts = append(page.Facts, factField{Name: detailFields[i].name, Label: detailFields[i].label, Value: *held})
		page.Details = page.Details || *held != ""
	}
	return page
}

func addPlantPage(principal auth.Principal, f plantFields, rows []scheduleDraft, now time.Time) plantFormPage {
	page := newPlantFormPage(f, now)
	page.Title = "Add plant"
	page.Action = newPlantPath
	page.Back = plantsPath
	page.Submit = "Add plant"
	page.showPhoto = canSetPicture(principal)
	page.PictureFocus = centre.String()
	for _, row := range rows {
		page.Schedules = append(page.Schedules, newScheduleField(row, newPlantPath, now))
	}
	return page
}

func editPlantPage(principal auth.Principal, f plantFields, plant store.Plant, now time.Time) plantFormPage {
	page := newPlantFormPage(f, now)
	page.Title = "Edit plant"
	page.Action = editPlantPath(plant.ID)
	page.Back = plantPath(plant.ID)
	page.Submit = "Save changes"
	page.showPhoto = canSetPicture(principal)
	page.Picture = picturePath(plant)
	return page
}

// swappedRow returns the row the re-render was triggered from: the one a
// button just closed, or the one whose fields were sent with the request.
func swappedRow(rows []scheduleField, values url.Values) (scheduleField, bool) {
	slug := values.Get("close")
	if slug == "" {
		slug = values.Get("open")
	}
	for _, row := range rows {
		if row.Slug == slug {
			return row, true
		}
	}
	return scheduleField{}, false
}

// gardenRooms returns the rooms for the Room field. It writes the response
// itself and returns false when the query fails.
func (h *plants) gardenRooms(w http.ResponseWriter, r *http.Request, gardenID uuid.UUID) ([]string, bool) {
	rooms, err := h.queries.ListRooms(r.Context(), gardenID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the rooms", err)
		return nil, false
	}
	return rooms, true
}

// newPlant handles GET /plants/new. The form is empty when first opened, and
// keeps its values when a schedule row re-renders it.
func (h *plants) newPlant(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	cares, err := h.queries.ListCareTypes(r.Context(), principal.Garden.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the care types", err)
		return
	}
	now := h.now().In(locationFor(principal.User))

	// A request with no query string is the form being opened. A re-render
	// sends every field the form rendered.
	query := r.URL.Query()
	fields, rows := plantFields{}, openingRows(cares, now)
	if len(query) > 0 {
		var fieldsOK, rowsOK bool
		fields, fieldsOK = readPlantFields(query, now)
		rows, rowsOK = readScheduleRows(query, cares, now)
		if !fieldsOK || !rowsOK {
			h.templates.badRequest(w, r)
			return
		}
	}
	page := addPlantPage(principal, fields, rows, now)

	// An htmx request from one row gets that row back, since it is the only
	// element that changes.
	if isHTMX(r) {
		row, ok := swappedRow(page.Schedules, query)
		if !ok {
			h.templates.badRequest(w, r)
			return
		}
		page.Schedules = []scheduleField{row}
		v := view{page: plantFormPageName, fragment: "schedule-fields", announce: row.nowShows()}
		h.templates.render(w, r, v, page)
		return
	}

	rooms, ok := h.gardenRooms(w, r, principal.Garden.ID)
	if !ok {
		return
	}
	page.Rooms = rooms
	page.Room = canonicalRoom(rooms, page.Room)
	h.templates.render(w, r, view{page: plantFormPageName}, page)
}

// create handles POST /plants/new. It writes the plant and its schedules in one
// transaction.
func (h *plants) create(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if !readMultipartForm(h.templates, w, r) {
		return
	}
	focus, focusPosted, focusOK := postedFocus(r)
	// The route admits anyone who may add a plant. A posted photo becomes the
	// plant's profile picture and the focus field positions it, so either one
	// is refused unless the member may also add a photo and set the profile
	// picture.
	if (photoPosted(r) || focusPosted) && !canSetPicture(principal) {
		h.templates.notFound(w, r)
		return
	}
	cares, err := h.queries.ListCareTypes(r.Context(), principal.Garden.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the care types", err)
		return
	}
	now := h.now().In(locationFor(principal.User))

	fields, fieldsOK := readPlantFields(r.PostForm, now)
	rows, rowsOK := readScheduleRows(r.PostForm, cares, now)
	if !fieldsOK || !rowsOK || !focusOK {
		h.templates.badRequest(w, r)
		return
	}
	rooms, ok := h.gardenRooms(w, r, principal.Garden.ID)
	if !ok {
		return
	}
	fields.room = canonicalRoom(rooms, fields.room)

	schedules, messages, refused := checkSchedules(rows, principal.Garden.ID)
	page := addPlantPage(principal, fields, rows, now)
	page.Rooms = rooms
	page.PictureFocus = focus.String()
	page.NameError, page.AcquiredError = fields.refuse()
	for i := range page.Schedules {
		page.Schedules[i].Error = messages[page.Schedules[i].Slug]
	}
	if refused || page.NameError != "" || page.AcquiredError != "" {
		page.PhotoNeedsChoosing = photoPosted(r)
		h.templates.render(w, r, view{page: plantFormPageName, status: http.StatusUnprocessableEntity}, page)
		return
	}

	// The photo row is inserted in the same transaction as the plant, so a
	// photo the store refuses rolls the plant back and a retry does not add
	// the plant twice. The file is written before the row and stays on disk
	// when the transaction fails.
	var plant store.Plant
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		var err error
		if plant, err = q.CreatePlant(r.Context(), fields.create(principal.Garden.ID)); err != nil {
			return err
		}
		for _, params := range schedules {
			params.PlantID = plant.ID
			if _, err := q.CreateCareSchedule(r.Context(), params); err != nil {
				return err
			}
		}
		if photoPosted(r) {
			upload, closeFiles, ok := postedPhoto(h.templates, w, r, principal, plant.ID)
			defer closeFiles()
			if !ok {
				return errResponded
			}
			return h.savePicture(r.Context(), q, upload, focus)
		}
		return nil
	})
	if errors.Is(err, errResponded) {
		return
	}
	if h.photoRefused(w, r, err, page) {
		return
	}
	if err != nil {
		h.templates.serverError(h.logger, w, r, "add the plant", err)
		return
	}
	if photoPosted(r) {
		h.notifyStorage(r.Context(), principal, plant)
	}
	http.Redirect(w, r, plantPath(plant.ID), http.StatusSeeOther)
}

// errResponded is returned from inside a transaction when the response has
// been written already, so the caller rolls back and writes nothing more.
var errResponded = errors.New("the response has been written")

// edit handles GET /plants/{plant}/edit with the same form, filled in.
func (h *plants) edit(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plant, ok := h.editable(w, r, principal)
	if !ok {
		return
	}
	rooms, ok := h.gardenRooms(w, r, principal.Garden.ID)
	if !ok {
		return
	}
	now := h.now().In(locationFor(principal.User))
	page := editPlantPage(principal, plantFieldsOf(plant), plant, now)
	page.Rooms = rooms
	focus, err := h.pictureFocus(r.Context(), principal, plant)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the plant's picture", err)
		return
	}
	page.PictureFocus = focus.String()
	h.templates.render(w, r, view{page: plantFormPageName}, page)
}

// update handles POST /plants/{plant}/edit and redirects to the plant's page.
func (h *plants) update(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	// The plant is resolved before the form is read, because a plant the
	// garden does not have is a 404 whatever was posted.
	plant, ok := h.editable(w, r, principal)
	if !ok {
		return
	}
	if !readMultipartForm(h.templates, w, r) {
		return
	}
	focus, focusPosted, focusOK := postedFocus(r)
	if (photoPosted(r) || pictureRemoved(r) || focusPosted) && !canSetPicture(principal) {
		h.templates.notFound(w, r)
		return
	}
	now := h.now().In(locationFor(principal.User))
	fields, fieldsOK := readPlantFields(r.PostForm, now)
	if !fieldsOK || !focusOK {
		h.templates.badRequest(w, r)
		return
	}
	rooms, ok := h.gardenRooms(w, r, principal.Garden.ID)
	if !ok {
		return
	}
	fields.room = canonicalRoom(rooms, fields.room)

	page := editPlantPage(principal, fields, plant, now)
	page.Rooms = rooms
	page.PictureFocus = focus.String()
	page.NameError, page.AcquiredError = fields.refuse()
	if pictureRemoved(r) {
		page.Picture = ""
		page.PictureRemoved = true
	}
	if page.NameError != "" || page.AcquiredError != "" {
		page.PhotoNeedsChoosing = photoPosted(r)
		h.templates.render(w, r, view{page: plantFormPageName, status: http.StatusUnprocessableEntity}, page)
		return
	}

	// A posted photo becomes the picture. Remove clears profile_photo_id and
	// keeps the photo row as one of the plant's photos. With neither, the
	// focus field moves the picture the plant already has.
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		if _, err := q.UpdatePlant(r.Context(), fields.update(principal.Garden.ID, plant.ID)); err != nil {
			return err
		}
		switch {
		case photoPosted(r):
			upload, closeFiles, ok := postedPhoto(h.templates, w, r, principal, plant.ID)
			defer closeFiles()
			if !ok {
				return errResponded
			}
			return h.savePicture(r.Context(), q, upload, focus)
		case pictureRemoved(r):
			_, err := q.SetProfilePhoto(r.Context(), store.SetProfilePhotoParams{GardenID: principal.Garden.ID, PlantID: plant.ID})
			return err
		case focusPosted && plant.ProfilePhotoID != nil:
			return q.SetPhotoFocus(r.Context(), store.SetPhotoFocusParams{FocusX: focus.x, FocusY: focus.y, GardenID: principal.Garden.ID, PlantID: plant.ID, PhotoID: *plant.ProfilePhotoID})
		}
		return nil
	})
	if errors.Is(err, errResponded) {
		return
	}
	if h.photoRefused(w, r, err, page) {
		return
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "save the plant", err)
		return
	}
	if photoPosted(r) {
		h.notifyStorage(r.Context(), principal, plant)
	}
	http.Redirect(w, r, plantPath(plant.ID), http.StatusSeeOther)
}

// confirmArchive handles GET /plants/{plant}/archive. It renders the plant's
// page with the archive confirmation at the bottom. An htmx request gets the
// bottom section alone, since that is the only element that changes.
func (h *plants) confirmArchive(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return
	case err != nil:
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return
	}
	// An archived plant has no Archive button. There is nothing to confirm.
	if detail.plant.ArchivedAt != nil {
		h.templates.notFound(w, r)
		return
	}

	page := newPlantPage(principal, detail)
	page.Foot = page.Foot.asking()
	fragment, ok := plantSwap(r, &page)
	if !ok {
		h.templates.notFound(w, r)
		return
	}
	announce := "Archive " + detail.plant.DisplayName() + "? It’s removed from Plants but keeps its activity and photos. Cancel or Archive."
	h.templates.render(w, r, view{page: "plant", fragment: fragment, announce: announce}, page)
}

// archive handles POST /plants/{plant}/archive. A plant is archived rather
// than deleted so its history is kept.
func (h *plants) archive(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	if _, err := h.queries.ArchivePlant(r.Context(), principal.Garden.ID, plantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.templates.notFound(w, r)
			return
		}
		h.templates.serverError(h.logger, w, r, "archive the plant", err)
		return
	}
	http.Redirect(w, r, plantsPath, http.StatusSeeOther)
}

// editable resolves the plant for the routes that change it: the plant form
// and Add a photo. It writes the response itself and returns false when there
// is nothing to edit. An archived plant is a 404 like a plant the garden does
// not have, since the form would otherwise save a plant that is no longer on
// the Plants list.
func (h *plants) editable(w http.ResponseWriter, r *http.Request, principal auth.Principal) (store.Plant, bool) {
	plant, ok := h.resolvePlant(w, r, principal)
	if !ok {
		return store.Plant{}, false
	}
	if plant.ArchivedAt != nil {
		h.templates.notFound(w, r)
		return store.Plant{}, false
	}
	return plant, true
}
