package http

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

// batchRow returns the text of the row saying how many of the account's codes
// are left. It is empty when the page has no such row.
func batchRow(page string) string {
	for _, row := range readHTML(page).all(isTag("li")) {
		if strings.Contains(row.text(), " left") {
			return row.text()
		}
	}
	return ""
}

// createButton returns the text of the button that posts a new set of codes.
func createButton(page string) string {
	return formTo(page, recoveryPath).first(isTag("button")).text()
}

func TestRecovery_ABatchReadsAsHowManyAreLeftAndWhenItWasMade(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, `INSERT INTO recovery_code (user_id, code_hash, generated_at, used_at)
		VALUES ($1, 'used', $2, $2), ($1, 'live-one', $2, NULL), ($1, 'live-two', $2, NULL)`,
		moreUserID, thursday.AddDate(0, 0, -31))

	page := f.page(t, f.handler.recovery, recoveryPath)

	if got, want := batchRow(page), "2 of 3 left Made 3 Aug"; got != want {
		t.Errorf("the batch reads %q, want %q:\n%s", got, want, text(page))
	}
	if got, want := createButton(page), "Create new codes"; got != want {
		t.Errorf("the button reads %q, want %q", got, want)
	}
}

func TestRecovery_AnAccountHoldingNoneIsToldSoAndOfferedASet(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.recovery, recoveryPath)

	if row := batchRow(page); row != "" {
		t.Errorf("the page shows the batch %q for an account holding none", row)
	}
	if !strings.Contains(text(page), "As the owner, nobody can send you a new invite link") {
		t.Errorf("the page does not say the account is holding none:\n%s", text(page))
	}
	if got, want := createButton(page), "Create codes"; got != want {
		t.Errorf("the button reads %q, want %q", got, want)
	}
}

func TestRecovery_AMemberIsNotToldThatOnlyAnOwnerIsPromptedForCodes(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities = auth.Capabilities{auth.TokenManage: true}

	page := f.page(t, f.handler.recovery, recoveryPath)

	if strings.Contains(text(page), "only an owner is prompted") {
		t.Errorf("the page tells a member only an owner is prompted:\n%s", page)
	}
	if got, want := createButton(page), "Create codes"; got != want {
		t.Errorf("the button reads %q, want %q", got, want)
	}
}

func TestRecovery_AnotherAccountsCodesAreNotShownAsThisAccountsBatch(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'someone-elses')", otherUserID)

	page := f.page(t, f.handler.recovery, recoveryPath)

	if row := batchRow(page); row != "" {
		t.Errorf("the page shows the batch %q, and it belongs to another account", row)
	}
}

func TestRecovery_CreatingCodesShowsTenOnceAndStoresOnlyTheirHashes(t *testing.T) {
	f := moreGarden(t)

	rec := f.post(t, f.handler.createCodes, recoveryPath, url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, text(rec.Body.String()))
	}
	page := rec.Body.String()
	var codes []string
	for _, item := range readHTML(page).all(isTag("li")) {
		codes = append(codes, item.text())
	}
	if len(codes) != 10 {
		t.Fatalf("the page lists %d codes, want 10:\n%s", len(codes), text(page))
	}
	if !strings.Contains(text(page), "These codes are shown only once") {
		t.Errorf("the page does not say the codes are shown once:\n%s", text(page))
	}
	// Copy all is rendered hidden with the codes one per line on it, for the
	// page's script to put on the clipboard.
	if copyAll := buttonLabelled(t, page, "Copy all"); copyAll.attr("data-copy") != strings.Join(codes, "\n") || !copyAll.has("hidden") {
		t.Errorf("Copy all is not hidden with the ten codes on it:\n%s", copyAll)
	}
	if !linkTo(page, accountPath, "Done") {
		t.Errorf("the page has no Done link back to Account:\n%s", text(page))
	}
	for _, code := range codes {
		if canonical, ok := auth.CanonicalRecoveryCode(code); !ok || canonical != code {
			t.Errorf("the code %q is not shown in the form it is looked up in", code)
		}
		var owner uuid.UUID
		if err := f.tx.QueryRow(t.Context(), "SELECT user_id FROM recovery_code WHERE code_hash = $1 AND used_at IS NULL AND generated_at = $2", auth.HashToken(code), thursday).Scan(&owner); err != nil {
			t.Errorf("the code %q has no live row made now: %v", code, err)
		} else if owner != moreUserID {
			t.Errorf("the code %q belongs to %s, want the reader", code, owner)
		}
		var stored int
		if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM recovery_code WHERE code_hash = $1", code).Scan(&stored); err != nil || stored != 0 {
			t.Errorf("the code %q is stored in the clear", code)
		}
	}

	again := f.page(t, f.handler.recovery, recoveryPath)
	if strings.Contains(text(again), "These codes are shown only once") {
		t.Errorf("the next request shows the codes again:\n%s", text(again))
	}
	if got, want := batchRow(again), "10 of 10 left"; !strings.HasPrefix(got, want) {
		t.Errorf("the batch reads %q, want it to start %q", got, want)
	}
}

func TestRecovery_CreatingANewSetDeletesTheOldOneAndLeavesAnotherAccountsAlone(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, `INSERT INTO recovery_code (user_id, code_hash, generated_at, used_at)
		VALUES ($1, 'old-used', $2, $2), ($1, 'old-live', $2, NULL), ($3, 'someone-elses', $2, NULL)`,
		moreUserID, thursday.AddDate(0, 0, -31), otherUserID)

	rec := f.post(t, f.handler.createCodes, recoveryPath, url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, text(rec.Body.String()))
	}
	var old, mine, theirs int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM recovery_code WHERE code_hash IN ('old-used', 'old-live')").Scan(&old); err != nil {
		t.Fatalf("counting the old codes: %v", err)
	}
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM recovery_code WHERE user_id = $1 AND generated_at = $2", moreUserID, thursday).Scan(&mine); err != nil {
		t.Fatalf("counting the new codes: %v", err)
	}
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM recovery_code WHERE user_id = $1 AND code_hash = 'someone-elses'", otherUserID).Scan(&theirs); err != nil {
		t.Fatalf("counting the other account's codes: %v", err)
	}
	if old != 0 {
		t.Errorf("%d codes of the old batch are still there, want none", old)
	}
	if mine != 10 {
		t.Errorf("the reader holds %d codes made now, want 10", mine)
	}
	if theirs != 1 {
		t.Error("the other account's code was deleted")
	}
}
