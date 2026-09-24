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

// photosPath is the URL of a plant's Photos page, the grid of every photo.
func photosPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/photos"
}

// olderPhotosPath is the URL of the Photos page holding the photos older than
// the one c names.
func olderPhotosPath(plantID uuid.UUID, c cursor) string {
	return photosPath(plantID) + "?" + url.Values{beforeParam: {c.String()}}.Encode()
}

// photoPath is the URL of a photo's own page.
func photoPath(plantID, photoID uuid.UUID) string {
	return photosPath(plantID) + "/" + photoID.String()
}

// newPhotoPath is the URL of the Add a photo page.
func newPhotoPath(plantID uuid.UUID) string {
	return photosPath(plantID) + "/new"
}

// deletePhotoPath is the URL for deleting a photo. A GET renders the
// confirmation on the photo's page and a POST deletes.
func deletePhotoPath(plantID, photoID uuid.UUID) string {
	return photoPath(plantID, photoID) + "/delete"
}

// gridPageSize is how many photos one page of the grid holds. Twenty-four
// divides into the three columns a phone shows and the four a desktop shows.
const gridPageSize = 24

// photoTilesFragment is the template that renders one page of the grid's
// tiles.
const photoTilesFragment = "photo-tiles"

type photosPage struct {
	// Name is the plant's name, shown in the back link and the title.
	Name string
	Back string
	// Add is the URL of the Add a photo page. Empty for a reader who may not
	// add photos and for an archived plant.
	Add   string
	Tiles []photoTile
	// Older is the URL of the next page of the grid, empty on the last page.
	Older string
}

// photoTile is one photo in the grid, a link to the photo's page around its
// image. When is the day the photo was taken. The grid does not show it and
// the strip on the plant's page shows it under each image.
type photoTile struct {
	Href string
	Src  string
	Alt  string
	When string
}

// photoGrid handles GET /plants/{plant}/photos. An htmx request from the Older
// photos tile gets the next page's tiles alone. They replace that tile.
func (h *plants) photoGrid(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plant, ok := h.resolvePlant(w, r, principal)
	if !ok {
		return
	}
	var before *cursor
	if s := r.URL.Query().Get(beforeParam); s != "" {
		c, ok := parseCursor(s)
		if !ok {
			h.templates.notFound(w, r)
			return
		}
		before = &c
	}
	params := store.ListPlantPhotosParams{
		GardenID: principal.Garden.ID,
		PlantID:  plant.ID,
		// One past the page tells whether there is an older page, without a
		// count.
		Count: gridPageSize + 1,
	}
	if before != nil {
		params.BeforeAt = &before.at
		params.BeforeID = &before.id
	}
	rows, err := h.queries.ListPlantPhotos(r.Context(), params)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the plant's photos", err)
		return
	}
	now := h.now().In(locationFor(principal.User))
	page := photosPage{Name: plant.DisplayName(), Back: plantPath(plant.ID)}
	if principal.Can(auth.PhotoAdd) && plant.ArchivedAt == nil {
		page.Add = newPhotoPath(plant.ID)
	}
	if len(rows) > gridPageSize {
		rows = rows[:gridPageSize]
		last := rows[len(rows)-1].Photo
		page.Older = olderPhotosPath(plant.ID, cursor{at: last.UploadedAt, id: last.ID})
	}
	for _, row := range rows {
		page.Tiles = append(page.Tiles, newPhotoTile(plant, row.Photo, now))
	}
	v := view{page: "photos", fragment: photoTilesFragment, announce: photosWord(len(page.Tiles)) + " added."}
	h.templates.render(w, r, v, page)
}

func newPhotoTile(plant store.Plant, p store.Photo, now time.Time) photoTile {
	day := photoDateWord(p, now)
	// A tile is a small square, so it shows the square variant. A photo
	// uploaded without one is shown from its full-size file instead.
	src := photoFullPath(plant.ID, p.ID)
	if p.SquareBytes != nil {
		src = photoSquarePath(plant.ID, p.ID)
	}
	return photoTile{
		Href: photoPath(plant.ID, p.ID),
		Src:  src,
		Alt:  "Photo of " + plant.DisplayName() + ", " + day,
		When: day,
	}
}

// photoDateWord is the day a photo was taken, or the day it was uploaded when
// the uploader did not say. A date in an earlier year includes the year.
func photoDateWord(p store.Photo, now time.Time) string {
	at := p.UploadedAt
	if p.TakenAt != nil {
		at = *p.TakenAt
	}
	at = at.In(now.Location())
	if at.Year() != now.Year() {
		return at.Format("2 Jan 2006")
	}
	return agoWord(at, now)
}

// resolvePlant reads the plant in the URL. A plant the garden does not have
// is a 404, the same as one that does not exist.
func (h *plants) resolvePlant(w http.ResponseWriter, r *http.Request, principal auth.Principal) (store.Plant, bool) {
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return store.Plant{}, false
	}
	plant, err := h.queries.GetPlant(r.Context(), principal.Garden.ID, plantID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		h.templates.notFound(w, r)
		return store.Plant{}, false
	case err != nil:
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return store.Plant{}, false
	}
	return plant, true
}
