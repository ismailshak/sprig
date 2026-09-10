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

// passkeysListID is both the HTML id of the list and the name of the template
// that renders it. Every Remove swaps it.
const passkeysListID = "passkeys-list"

// passkeyProviders names the passkey provider for each AAGUID an
// authenticator sends at registration. The AAGUIDs are the published ones from
// the passkey-authenticator-aaguids list.
//
// The label is worked out when the page renders rather than stored at
// registration, so adding a provider here relabels the rows already enrolled.
var passkeyProviders = map[uuid.UUID]string{
	uuid.MustParse("fbfc3007-154e-4ecc-8c0b-6e020557d7bd"): "iCloud Keychain",
	uuid.MustParse("bada5566-a7aa-401f-bd96-45619a55120d"): "1Password",
	uuid.MustParse("ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4"): "Google Password Manager",
	uuid.MustParse("adce0002-35bc-c60a-648b-0b25f1f05503"): "Chrome on Mac",
	uuid.MustParse("d548826e-79b4-db40-a3d8-11116f7e8349"): "Bitwarden",
	uuid.MustParse("531126d6-e717-415c-9320-3d9aa6981239"): "Dashlane",
	uuid.MustParse("08987058-cadc-4b81-b6e1-30de50dcbe96"): "Windows Hello",
	uuid.MustParse("9ddd1817-af5a-4672-a2b9-3e3dd95000a9"): "Windows Hello",
	uuid.MustParse("6028b017-b1d4-4c02-b4b3-afcdafc96bb2"): "Windows Hello",
	uuid.MustParse("50726f74-6f6e-5061-7373-50726f746f6e"): "Proton Pass",
	uuid.MustParse("53414d53-554e-4700-0000-000000000000"): "Samsung Pass",
	uuid.MustParse("fdb141b2-5d84-443e-8a35-4698c205a502"): "KeePassXC",
	uuid.MustParse("0ea242b4-43c4-4a1b-8b17-dd6d0b6baec6"): "Keeper",
	uuid.MustParse("b84e4048-15dc-4dd0-8640-f4f60813c8af"): "NordPass",
	uuid.MustParse("f3809540-7f14-49c1-a8b3-8f813b225541"): "Enpass",
}

// passkeyLabel is the name shown on a passkey's row: the provider's, when the
// registration named one in passkeyProviders, and otherwise the browser label
// stored with the row.
func passkeyLabel(key store.PasskeyCredential) string {
	if key.Aaguid != nil {
		if provider, ok := passkeyProviders[*key.Aaguid]; ok {
			return provider
		}
	}
	return key.Name
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
	// Name is the provider holding the passkey, "1Password" or "iCloud
	// Keychain", when its AAGUID is one passkeyProviders knows. Otherwise it is
	// the device and browser the passkey was registered from, as "Mac ·
	// Chrome".
	Name string
	Used string
	// Remove is the URL the row's Remove button posts to. It is empty on the
	// only credential an account has left.
	Remove string
}

func (h *more) passkeys(w http.ResponseWriter, r *http.Request) {
	h.renderPasskeys(w, r)
}

func (h *more) renderPasskeys(w http.ResponseWriter, r *http.Request) {
	principal := PrincipalFrom(r)
	keys, err := h.queries.ListPasskeys(r.Context(), principal.User.ID)
	if err != nil {
		h.templates.serverError(h.logger, w, r, "list the passkeys", err)
		return
	}
	page := newPasskeysPage(keys, h.now().In(locationFor(principal.User)))
	v := view{page: "passkeys"}
	if r.Header.Get("HX-Target") == passkeysListID {
		v.fragment = passkeysListID
	}
	h.templates.render(w, r, v, page)
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
	if isHTMX(r) {
		h.renderPasskeys(w, r)
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
		row := passkeyRow{Name: passkeyLabel(key), Used: usedNote(key.LastUsedAt, now)}
		if !page.OnlyOne {
			row.Remove = removePasskeyPath(key.ID)
		}
		page.Keys = append(page.Keys, row)
	}
	return page
}
