package http

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

func TestPasskeys_EachDeviceIsARowWithWhenItWasLastUsed(t *testing.T) {
	f := moreGarden(t)

	rows := stackedRowsOf(f.page(t, f.handler.passkeys, passkeysPath))

	want := []stackedRow{
		{name: "iPhone", meta: "Last used today", drop: removePasskeyPath(phoneKeyID)},
		{name: "MacBook Air", meta: "Never used", drop: removePasskeyPath(laptopKeyID)},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("the passkeys are %v, want %v", rows, want)
	}
}

func TestPasskeys_TheLastOneOffersNoRemoveAndThePageSaysWhy(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "DELETE FROM passkey_credential WHERE id = $1", laptopKeyID)

	page := f.page(t, f.handler.passkeys, passkeysPath)

	rows := stackedRowsOf(page)
	if len(rows) != 1 {
		t.Fatalf("the page lists %d passkeys, want 1", len(rows))
	}
	if rows[0].drop != "" {
		t.Errorf("the only passkey offers Remove at %q", rows[0].drop)
	}
	if !strings.Contains(text(page), "Your only passkey can’t be removed") {
		t.Errorf("the page does not say why Remove is absent:\n%s", text(page))
	}
}

func TestPasskeys_RemovingOneDeletesThatCredential(t *testing.T) {
	f := moreGarden(t)

	rec := f.remove(t, f.handler.removePasskey, "key", phoneKeyID, removePasskeyPath(phoneKeyID))

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != passkeysPath {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, passkeysPath)
	}
	rows := stackedRowsOf(f.page(t, f.handler.passkeys, passkeysPath))
	if len(rows) != 1 || rows[0].name != "MacBook Air" {
		t.Errorf("the passkeys left are %v, want the MacBook Air alone", rows)
	}
}

func TestPasskeys_TheLastCredentialIsNotRemovedEvenWhenThePostIsMadeByHand(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "DELETE FROM passkey_credential WHERE id = $1", laptopKeyID)

	rec := f.remove(t, f.handler.removePasskey, "key", phoneKeyID, removePasskeyPath(phoneKeyID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var left int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM passkey_credential WHERE user_id = $1", moreUserID).Scan(&left); err != nil {
		t.Fatalf("counting the passkeys: %v", err)
	}
	if left != 1 {
		t.Errorf("%d passkeys are left, want the last one kept", left)
	}
}

func TestPasskeys_AnotherAccountsCredentialIsNotFound(t *testing.T) {
	f := moreGarden(t)

	rec := f.remove(t, f.handler.removePasskey, "key", strangerKeyID, removePasskeyPath(strangerKeyID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var left int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM passkey_credential WHERE user_id = $1", otherUserID).Scan(&left); err != nil {
		t.Fatalf("counting the passkeys: %v", err)
	}
	if left != 1 {
		t.Errorf("the other account has %d passkeys, want 1", left)
	}
}

// passkeyRowID returns the id of the passkey_credential row a device
// registered.
func (f *moreFixture) passkeyRowID(t *testing.T, credentialID string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT id FROM passkey_credential WHERE credential_id = $1", credentialID).Scan(&id); err != nil {
		t.Fatalf("finding the passkey: %v", err)
	}
	return id
}

func TestPasskeys_RemovingAPasskeyEndsTheSessionsItSignedInAndNoOther(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)
	now := f.handler.now()

	rec := f.signInWith(t, h, device)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("signing in: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	byPasskey := cookieNamed(t, rec, "__Host-sprig_session").Value
	byDevSignIn, _, err := h.sessions.Create(t.Context(), now, moreUserID, &moreGardenID, nil, "")
	if err != nil {
		t.Fatalf("starting a session with no passkey: %v", err)
	}

	var recorded *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT passkey_credential_id FROM session WHERE token_hash = $1", auth.HashToken(byPasskey)).Scan(&recorded); err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	passkeyID := f.passkeyRowID(t, device.CredentialID())
	if recorded == nil || *recorded != passkeyID {
		t.Fatalf("the session records passkey %v, want %s", recorded, passkeyID)
	}

	removal := f.remove(t, f.handler.removePasskey, "key", passkeyID, removePasskeyPath(passkeyID))

	if removal.Code != http.StatusSeeOther {
		t.Fatalf("removing: status = %d, want %d:\n%s", removal.Code, http.StatusSeeOther, removal.Body.String())
	}
	if _, err := h.sessions.Lookup(t.Context(), now, byPasskey); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("the session the removed passkey signed in still resolves: err = %v, want %v", err, auth.ErrNoSession)
	}
	if _, err := h.sessions.Lookup(t.Context(), now, byDevSignIn); err != nil {
		t.Errorf("the session with no passkey behind it stopped resolving: %v", err)
	}
}

func TestPasskeys_APasskeyFromAKnownProviderIsNamedAfterTheProvider(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "UPDATE passkey_credential SET aaguid = $1 WHERE id = $2", uuid.MustParse("bada5566-a7aa-401f-bd96-45619a55120d"), phoneKeyID)
	// An AAGUID the table does not know keeps the browser's label.
	f.exec(t, "UPDATE passkey_credential SET aaguid = $1 WHERE id = $2", uuid.MustParse("11111111-2222-4333-8444-555555555555"), laptopKeyID)

	rows := stackedRowsOf(f.page(t, f.handler.passkeys, passkeysPath))

	want := []stackedRow{
		{name: "1Password", meta: "Last used today", drop: removePasskeyPath(phoneKeyID)},
		{name: "MacBook Air", meta: "Never used", drop: removePasskeyPath(laptopKeyID)},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("the passkeys are %v, want %v", rows, want)
	}
}

func TestPasskeys_ARemoveSentAsASwapGetsTheListWithoutThatRow(t *testing.T) {
	f := moreGarden(t)

	rec := f.swap(t, f.handler.removePasskey, removePasskeyPath(phoneKeyID), passkeysListID, "key", phoneKeyID.String(), url.Values{})

	rows := stackedRowsOf(fragment(t, rec, passkeysListID))
	if len(rows) != 1 || rows[0].name != "MacBook Air" {
		t.Errorf("the passkeys left are %v, want the MacBook Air alone", rows)
	}
}

func TestPasskeys_RemovingThePasskeyThisSessionSignedInWithRedirectsToSignIn(t *testing.T) {
	f := moreGarden(t)
	f.principal.Session.PasskeyCredentialID = &phoneKeyID

	rec := f.remove(t, f.handler.removePasskey, "key", phoneKeyID, removePasskeyPath(phoneKeyID))

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signInPath {
		t.Errorf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, signInPath)
	}
}

func TestPasskeys_RemovingAPasskeyThisSessionDidNotSignInWithGetsTheList(t *testing.T) {
	f := moreGarden(t)
	f.principal.Session.PasskeyCredentialID = &laptopKeyID

	rec := f.swap(t, f.handler.removePasskey, removePasskeyPath(phoneKeyID), passkeysListID, "key", phoneKeyID.String(), url.Values{})

	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("HX-Redirect = %q, want the list and no redirect", got)
	}
	rows := stackedRowsOf(fragment(t, rec, passkeysListID))
	if len(rows) != 1 || rows[0].name != "MacBook Air" {
		t.Errorf("the passkeys left are %v, want the MacBook Air alone", rows)
	}
}

func TestPasskeys_ARemoveOfThisSessionsPasskeySentAsASwapRedirectsToSignIn(t *testing.T) {
	f := moreGarden(t)
	f.principal.Session.PasskeyCredentialID = &phoneKeyID

	rec := f.swap(t, f.handler.removePasskey, removePasskeyPath(phoneKeyID), passkeysListID, "key", phoneKeyID.String(), url.Values{})

	if got := rec.Header().Get("HX-Redirect"); got != signInPath {
		t.Errorf("HX-Redirect = %q, want %s", got, signInPath)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("the response has a body htmx would swap in:\n%s", rec.Body.String())
	}
}
