package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/ismailshak/sprig/internal/store"
)

// handlePath is the URL the script on Set up your garden and an invite's join
// form GETs a free handle from, with the display name in the name parameter.
const handlePath = "/handle"

// handleSuggestions serves the handle suggestion. The route has no session,
// because the account does not exist yet.
type handleSuggestions struct {
	queries   *store.Queries
	templates *Templates
	logger    *slog.Logger
}

// tooManyHandleSuggestions is the response to a GET /handle past the budget.
// The page's script ignores anything but a 200 and leaves the handle it
// mirrored from the display name, so there is no body to write.
func tooManyHandleSuggestions(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusTooManyRequests)
}

// suggest handles GET /handle. It returns {"handle": "..."}: the display
// name's slug, or the slug and a random suffix when another account holds it.
// A blank name is a 422. Without JavaScript nothing calls it. The post then
// makes the handle from the display name.
func (h *handleSuggestions) suggest(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	handle, err := h.queries.FreeHandle(r.Context(), name)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "suggest a handle", err)
		return
	}
	writeJSON(h.templates, h.logger, w, r, struct {
		Handle string `json:"handle"`
	}{handle})
}
