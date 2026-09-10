package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

const archivedPlantsPath = plantsPath + "/archived"

func restorePlantPath(plantID uuid.UUID) string {
	return plantPath(plantID) + "/restore"
}

type archivedPage struct {
	Bar    topbar
	Plants []plantRow
}

// archived handles GET /plants/archived.
func (h *plants) archived(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	list, err := h.queries.ListArchivedPlants(r.Context(), principal.Garden.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the archived plants", err)
		return
	}
	now := h.now().In(locationFor(principal.User))
	h.templates.render(w, r, view{page: "archived-plants"}, newArchivedPage(list, now))
}

func newArchivedPage(list []store.Plant, now time.Time) archivedPage {
	page := archivedPage{Bar: topbar{Href: plantsPath, Back: "Plants", Title: "Archived plants"}}
	for _, plant := range list {
		row := plantRow{
			Href:       plantPath(plant.ID),
			Name:       plant.DisplayName(),
			Botanical:  plant.BotanicalOnly(),
			Picture:    squarePicturePath(plant),
			ArchivedOn: archivedWord(*plant.ArchivedAt, now),
		}
		row.Sub, row.SubBotanical = plant.OtherName()
		page.Plants = append(page.Plants, row)
	}
	return page
}

// archivedLink is the link at the bottom of the Plants page, reading "3
// archived". It is nil when nothing is archived.
func archivedLink(count int64) *link {
	if count == 0 {
		return nil
	}
	return &link{Label: strconv.FormatInt(count, 10) + " archived", Href: archivedPlantsPath}
}

// restore handles POST /plants/{plant}/restore. It puts an archived plant back
// on the Plants list. There is no confirmation step, because archiving the
// plant again undoes it. With htmx it renders the page under the top bar,
// because restoring changes the back link, the care buttons, the schedule
// rows, Add photo and the buttons at the bottom. A plain post redirects to
// the plant's page.
func (h *plants) restore(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	plantID, err := uuid.Parse(r.PathValue("plant"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	if _, err := h.queries.RestorePlant(r.Context(), principal.Garden.ID, plantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.templates.notFound(w, r)
			return
		}
		h.templates.serverError(h.logger, w, r, "restore the plant", err)
		return
	}
	if !isHTMX(r) {
		http.Redirect(w, r, plantPath(plantID), http.StatusSeeOther)
		return
	}
	detail, err := loadPlant(r.Context(), h.queries, principal, plantID, h.now())
	if err != nil {
		h.templates.serverError(h.logger, w, r, "load the plant", err)
		return
	}
	page := newPlantPage(principal, detail)
	fragment, ok := plantSwap(r, &page)
	if !ok {
		h.templates.notFound(w, r)
		return
	}
	announce := detail.plant.DisplayName() + " restored. It’s back on Plants."
	h.templates.render(w, r, view{page: "plant", fragment: fragment, announce: announce}, page)
}
