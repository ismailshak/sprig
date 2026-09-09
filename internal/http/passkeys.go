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
	Bar  topbar
	Keys []passkeyRow
	// OnlyOne is true when the account has one credential left. The page then
	// says why no row offers Remove.
	OnlyOne bool
	// Error is the message shown above Add a passkey when a device was refused.
	// Empty otherwise.
	Error string
	// Add is the URL the Add a passkey form posts the browser's credential to.
	Add string
	// Challenge is the URL the page's script posts to for a challenge, before it
	// calls the browser's credential API.
	Challenge string
	// Field is the name of the hidden input the browser's credential goes in. The
	// script reads it off the form, so the name is written once.
	Field string
}

type passkeyRow struct {
	// Name is the device and browser the passkey was registered from, as
	// "Mac · Chrome".
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
		h.templates.serverError(h.logger, w, r, "list the passkeys", err)
		return
	}
	page := newPasskeysPage(keys, h.now().In(locationFor(principal.User)))
	h.templates.render(w, r, view{page: "passkeys"}, page)
}

// removePasskey deletes one credential. The sessions that passkey signed in
// are deleted with it, so a browser holding one of them is sent to the sign-in
// page on its next request. Removing the last credential is refused by the
// query, because an account with none has no way back in. The delete runs
// under a lock on the account's row, so two removals sent at the same moment
// cannot both pass that check and empty the list.
func (h *more) removePasskey(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	passkeyID, err := uuid.Parse(r.PathValue("key"))
	if err != nil {
		h.templates.notFound(w, r)
		return
	}
	var removed int64
	err = h.queries.InTx(r.Context(), func(q *store.Queries) error {
		if err := q.LockUser(r.Context(), principal.User.ID); err != nil {
			return err
		}
		n, err := q.DeletePasskey(r.Context(), principal.User.ID, passkeyID)
		removed = n
		return err
	})
	if err != nil {
		h.templates.serverError(h.logger, w, r, "remove the passkey", err)
		return
	}
	// Another account's credential, one already removed and the last one left
	// are all 404, since none of the three was a button this page offered.
	if removed == 0 {
		h.templates.notFound(w, r)
		return
	}
	http.Redirect(w, r, passkeysPath, http.StatusSeeOther)
}

func newPasskeysPage(keys []store.PasskeyCredential, now time.Time) passkeysPage {
	page := passkeysPage{
		Bar:       moreBar("Passkeys"),
		OnlyOne:   len(keys) == 1,
		Add:       passkeysPath,
		Challenge: registerPath,
		Field:     credentialField,
	}
	for _, key := range keys {
		row := passkeyRow{Name: key.Name, Used: usedNote(key.LastUsedAt, now)}
		if !page.OnlyOne {
			row.Remove = removePasskeyPath(key.ID)
		}
		page.Keys = append(page.Keys, row)
	}
	return page
}
