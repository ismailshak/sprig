package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
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

// formMaxBytes is the largest post accepted on the plant form and on Add a
// photo, the two photo files included.
const formMaxBytes = 10 << 20

// waterSlug is the schedule row the add form opens with, since every plant is
// watered and no other care is that common.
const waterSlug = "water"

// acquiredSpan is how many years the acquired-year select offers, this year
// included.
const acquiredSpan = 21

// detailFields are the fields under Details, in the order the plant's page
// shows them, with the name each input posts under. Water and Feed repeat two
// care type names because a schedule says how often and a Details field says
// what this plant needs.
var detailFields = [...]struct{ name, label string }{
	{"sun", "Sun"},
	{"water", "Water"},
	{"feed", "Feed"},
	{"soil", "Soil"},
	{"climate", "Climate"},
	{"pot", "Pot"},
}

// plantFields is the plant half of the form's values.
type plantFields struct {
	nickname  string
	common    string
	botanical string
	room      string
	sun       string
	water     string
	feed      string
	soil      string
	climate   string
	pot       string
	notes     string
	// month and year are zero when the select is on its placeholder option.
	month int
	year  int
}

// facts returns a pointer to the field for each of detailFields, in the same
// order, so reading and rendering the form iterate one list.
func (f *plantFields) facts() [len(detailFields)]*string {
	return [...]*string{&f.sun, &f.water, &f.feed, &f.soil, &f.climate, &f.pot}
}

// readPlantFields reads the plant half from a query string or a form body. It
// returns false for an acquired month or year that is not one of the options.
func readPlantFields(values url.Values, now time.Time) (plantFields, bool) {
	f := plantFields{
		nickname:  strings.TrimSpace(values.Get("nickname")),
		common:    strings.TrimSpace(values.Get("common")),
		botanical: strings.TrimSpace(values.Get("botanical")),
		room:      strings.TrimSpace(values.Get("room")),
		notes:     strings.TrimSpace(values.Get("notes")),
	}
	for i, held := range f.facts() {
		*held = strings.TrimSpace(values.Get(detailFields[i].name))
	}

	// A missing select stays at zero, because a schedule row's own request
	// sends that row and none of these fields.
	var ok bool
	if values.Has("acquired-month") {
		if f.month, ok = numberIn(values.Get("acquired-month"), append([]int{0}, months()...)); !ok {
			return f, false
		}
	}
	if values.Has("acquired-year") {
		if f.year, ok = numberIn(values.Get("acquired-year"), append([]int{0}, acquiredYears(now)...)); !ok {
			return f, false
		}
	}
	return f, true
}

func plantFieldsOf(plant store.Plant) plantFields {
	f := plantFields{
		nickname:  value(plant.Nickname),
		common:    value(plant.CommonName),
		botanical: value(plant.BotanicalName),
		room:      value(plant.Location),
		notes:     value(plant.Notes),
	}
	stored := [...]*string{plant.Sun, plant.WaterNeeds, plant.FeedNeeds, plant.Soil, plant.Climate, plant.Pot}
	for i, held := range f.facts() {
		*held = value(stored[i])
	}
	if plant.AcquiredMonth != nil {
		f.month = int(*plant.AcquiredMonth)
	}
	if plant.AcquiredYear != nil {
		f.year = int(*plant.AcquiredYear)
	}
	return f
}

// refuse returns the form's error messages: one under the three names and one
// under Acquired. Both are empty when the post is valid.
func (f plantFields) refuse() (name, acquired string) {
	if f.nickname == "" && f.common == "" && f.botanical == "" {
		name = "Enter at least one name."
	}
	// A month with no year is an error rather than dropped silently, since the
	// plant's page shows nothing for it.
	if f.month != 0 && f.year == 0 {
		acquired = "Choose a year as well as a month."
	}
	return name, acquired
}

func (f plantFields) create(gardenID uuid.UUID) store.CreatePlantParams {
	return store.CreatePlantParams{
		GardenID:      gardenID,
		Nickname:      set(f.nickname),
		CommonName:    set(f.common),
		BotanicalName: set(f.botanical),
		Location:      set(f.room),
		Sun:           set(f.sun),
		WaterNeeds:    set(f.water),
		FeedNeeds:     set(f.feed),
		Soil:          set(f.soil),
		Climate:       set(f.climate),
		Pot:           set(f.pot),
		Notes:         set(f.notes),
		AcquiredYear:  acquiredPart(f.year),
		AcquiredMonth: acquiredPart(f.month),
	}
}

// update writes the same columns as create, so a field emptied on the form is
// emptied on the row.
func (f plantFields) update(gardenID, plantID uuid.UUID) store.UpdatePlantParams {
	c := f.create(gardenID)
	return store.UpdatePlantParams{
		GardenID:      gardenID,
		PlantID:       plantID,
		Nickname:      c.Nickname,
		CommonName:    c.CommonName,
		BotanicalName: c.BotanicalName,
		Location:      c.Location,
		Sun:           c.Sun,
		WaterNeeds:    c.WaterNeeds,
		FeedNeeds:     c.FeedNeeds,
		Soil:          c.Soil,
		Climate:       c.Climate,
		Pot:           c.Pot,
		Notes:         c.Notes,
		AcquiredYear:  c.AcquiredYear,
		AcquiredMonth: c.AcquiredMonth,
	}
}

// readMultipartForm parses a post into r.PostForm, whether it is encoded as
// multipart/form-data or as a query string. The plant form and Add a photo
// both post this way. It writes the response itself and returns false when the
// body was over the size cap or did not parse.
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

// photoField is the data the photo-field template renders. Its fields are
// where the plant form and Add a photo differ.
type photoField struct {
	// Choose is the label on the button that opens the file chooser: "Add
	// photo" on the plant form and "Choose photo" on Add a photo.
	Choose string
	// Picture is the URL of the plant's current profile picture, shown in the
	// preview when the plant form opens. It is empty on Add a photo, because
	// that page does not set the picture.
	Picture string
	// Removable renders the hidden photo-removed input. Add a photo leaves it
	// out, because it has no picture to clear. Removed is that input's value.
	Removable bool
	Removed   bool
	// Focus is the value of the hidden focus input, which part of the picture
	// the plant's page shows, as "x,y" in percentages. It is empty on Add a
	// photo. That page renders no focus input, because a progress photo is
	// never cropped.
	Focus string
	// NeedsChoosing renders "Choose the photo again."
	NeedsChoosing bool
	// Missing renders "Choose a photo."
	Missing bool
	// Refusal is the sentence under the field saying why the store refused
	// the photo. It is empty otherwise.
	Refusal string
	// Submit is the label on the submit button rendered inside the field. It
	// is empty on the plant form, which has its own submit button below. Add a
	// photo puts its button here so that a browser which cannot resize a photo
	// has nothing to post.
	Submit string
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

// postedPhoto builds an upload for the plant from the posted photo file and
// its square variant. closeFiles closes whatever was opened and is never nil.
// A part that will not open gets a 400 written here, and false returned. A
// post with no square gets the same 400, because every list shows the picture
// as its square and the form's script always posts both files.
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

// The sentences under the photo field for a photo the garden has no room for
// and for a file that is not a photo.
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

// set converts a text field to its column value. An empty field is null, since
// an empty string in the column would count as a value.
func set(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// canonicalRoom returns the room in rooms that matches typed ignoring case, or
// typed unchanged when none does. Without it, "bathroom" would be a second
// room next to "Bathroom" on the Plants list. Text matching no room is
// kept as it was typed, because typing a name is how a new room is made.
func canonicalRoom(rooms []string, typed string) string {
	for _, room := range rooms {
		if strings.EqualFold(room, typed) {
			return room
		}
	}
	return typed
}

// acquiredPart converts an acquired month or year to its column value, null
// for the placeholder option.
func acquiredPart(n int) *int16 {
	if n == 0 {
		return nil
	}
	return smallint(n)
}

// smallint converts n for a smallint column, null if it is out of range.
func smallint(n int) *int16 {
	if n < math.MinInt16 || n > math.MaxInt16 {
		return nil
	}
	return ptr(int16(n))
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

// acquiredYears is this year and the 20 before it, newest first.
func acquiredYears(now time.Time) []int {
	out := make([]int, 0, acquiredSpan)
	for i := range acquiredSpan {
		out = append(out, now.Year()-i)
	}
	return out
}

// readScheduleRows reads a row for every care type, since a re-render shows
// the closed rows as well as the open ones.
func readScheduleRows(values url.Values, cares []store.CareType, now time.Time) ([]scheduleDraft, bool) {
	rows := make([]scheduleDraft, 0, len(cares))
	for _, care := range cares {
		row, ok := readScheduleDraft(values, care, now)
		if !ok {
			return nil, false
		}
		rows = append(rows, row)
	}
	return rows, true
}

// openingRows returns the schedule rows for a freshly opened add form.
func openingRows(cares []store.CareType, now time.Time) []scheduleDraft {
	rows := make([]scheduleDraft, 0, len(cares))
	for _, care := range cares {
		row := newScheduleDraft(care, now)
		row.open = care.Slug == waterSlug
		rows = append(rows, row)
	}
	return rows
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

// checkSchedules converts the open rows to care_schedule params. Invalid rows
// get an error message instead, keyed by care type slug.
func checkSchedules(rows []scheduleDraft, gardenID uuid.UUID) (schedules []store.CreateCareScheduleParams, messages map[string]string, refused bool) {
	messages = map[string]string{}
	for _, row := range rows {
		if !row.open {
			continue
		}
		params, message := row.params(gardenID)
		if message != "" {
			messages[row.care.Slug] = message
			refused = true
			continue
		}
		schedules = append(schedules, params)
	}
	return schedules, messages, refused
}

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
