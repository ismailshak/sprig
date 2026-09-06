package http

import (
	"net/http"
	"slices"
	"strings"
	"testing"
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
	if !strings.Contains(text(page), "This is the only way you can sign in, so it cannot be removed") {
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
