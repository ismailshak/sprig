package http

import (
	"net/http"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/store"
)

func removePasskeyPath(passkeyID uuid.UUID) string {
	return passkeysPath + "/" + passkeyID.String() + "/remove"
}

type passkeysPage struct {
	Keys []passkeyRow
	// OnlyOne is true when the account has one credential left. The page then
	// says why no row offers Remove.
	OnlyOne bool
}

type passkeyRow struct {
	// Name is what the browser called the device when it registered.
	Name string
	Used string
	// Remove is the URL the row's Remove button posts to. It is empty on the
	// only credential an account has left.
	Remove string
}

func (h *more) passkeys(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	keys, err := h.queries.ListPasskeys(r.Context(), principal.User.ID)
	if err != nil {
		serverError(h.logger, w, r, "list the passkeys", err)
		return
	}
	page := newPasskeysPage(keys, h.now().In(locationFor(principal.User)))
	h.templates.render(w, r, view{page: "passkeys"}, page)
}

// removePasskey deletes one credential. Removing the last one is refused by
// the query, because an account with none has no way back in.
func (h *more) removePasskey(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	passkeyID, err := uuid.Parse(r.PathValue("key"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	removed, err := h.queries.DeletePasskey(r.Context(), principal.User.ID, passkeyID)
	if err != nil {
		serverError(h.logger, w, r, "remove the passkey", err)
		return
	}
	// Another account's credential, one already removed and the last one left
	// are all 404, since none of the three was a button this page offered.
	if removed == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, passkeysPath, http.StatusSeeOther)
}

func newPasskeysPage(keys []store.PasskeyCredential, now time.Time) passkeysPage {
	page := passkeysPage{OnlyOne: len(keys) == 1}
	for _, key := range keys {
		row := passkeyRow{Name: key.Name, Used: usedNote(key.LastUsedAt, now)}
		if !page.OnlyOne {
			row.Remove = removePasskeyPath(key.ID)
		}
		page.Keys = append(page.Keys, row)
	}
	return page
}
