package http

import (
	"net/http"
	"strings"

	"github.com/ismailshak/sprig/internal/store"
)

// deleteGardenPath is the URL of the Delete garden page. A GET renders the
// confirmation and a POST deletes the garden.
const deleteGardenPath = gardenPath + "/delete"

// deleteGardenMismatch is shown under the field when what was typed is not
// the garden's name.
const deleteGardenMismatch = "That isn’t the garden’s name. Type it exactly as shown."

// deleteGardenPage is the Delete garden page. The garden's name has to be
// typed into a field rather than a button pressed, because this is the one
// action that deletes plants, activity and photos with no way to undo it.
type deleteGardenPage struct {
	Bar    topbar
	Action string
	// Name is the garden's name. The typed value has to equal it.
	Name string
	// Typed is the value put back in the field after a post that did not match.
	Typed string
	// Error is shown under the field, empty until a post is refused.
	Error string
}

func (h *more) newDeleteGardenPage(name string) deleteGardenPage {
	return deleteGardenPage{
		Bar:    topbar{Href: gardenPath, Back: "Garden", Title: "Delete garden"},
		Action: deleteGardenPath,
		Name:   name,
	}
}

// confirmDeleteGarden handles GET /more/garden/delete.
func (h *more) confirmDeleteGarden(w http.ResponseWriter, r *http.Request) {
	h.templates.render(w, r, view{page: "delete-garden"}, h.newDeleteGardenPage(PrincipalFrom(r).Garden.Name))
}

// deleteGarden handles POST /more/garden/delete. The rows are deleted in one
// transaction and the photo files afterwards, so a crash between the two
// leaves files with no row rather than rows with no file. Deleting the
// memberships sets garden_id to null on the garden's sessions. The redirect to
// Today then opens the owner's next garden or the no-garden page.
func (h *more) deleteGarden(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	typed := strings.TrimSpace(r.PostForm.Get("name"))
	if typed != principal.Garden.Name {
		page := h.newDeleteGardenPage(principal.Garden.Name)
		page.Typed = typed
		page.Error = deleteGardenMismatch
		h.templates.render(w, r, view{page: "delete-garden", status: http.StatusUnprocessableEntity}, page)
		return
	}

	gardenID := principal.Garden.ID
	err := h.queries.InTx(r.Context(), func(q *store.Queries) error {
		if err := q.DeleteGardenCareEvents(r.Context(), gardenID); err != nil {
			return err
		}
		_, err := q.DeleteGarden(r.Context(), gardenID)
		return err
	})
	if err != nil {
		serverError(h.logger, w, r, "delete the garden", err)
		return
	}
	// A failure here is logged and not shown, because the rows are already
	// deleted. The photo sweep deletes any file left behind.
	if err := h.photos.DeleteGardenPhotos(gardenID); err != nil {
		h.logger.WarnContext(r.Context(), "remove the deleted garden's photos", "error", err)
	}
	// The digest job works out its next send again, because the memberships it
	// was going to send to are gone.
	h.wake.call()
	http.Redirect(w, r, todayPath, http.StatusSeeOther)
}
