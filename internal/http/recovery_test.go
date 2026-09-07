package http

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

var (
	recoveryLine  = regexp.MustCompile(`(?s)<li class="row row--setting row--stack">(.*?)</li>`)
	settingName   = regexp.MustCompile(`(?s)<span class="row__name">(.*?)</span>`)
	settingMeta   = regexp.MustCompile(`(?s)<span class="row__meta">(.*?)</span></span>`)
	wideAction    = regexp.MustCompile(`(?s)<button class="wide-action"[^>]*>(.*?)</button>`)
	secretCode    = regexp.MustCompile(`<li>([^<]+)</li>`)
	secretSection = regexp.MustCompile(`(?s)<div class="secret">(.*?)</div>`)
)

func TestRecovery_ABatchReadsAsHowManyAreLeftAndWhenItWasMade(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, `INSERT INTO recovery_code (user_id, code_hash, generated_at, used_at)
		VALUES ($1, 'used', $2, $2), ($1, 'live-one', $2, NULL), ($1, 'live-two', $2, NULL)`,
		moreUserID, thursday.AddDate(0, 0, -31))

	page := f.page(t, f.handler.recovery, recoveryPath)

	line := recoveryLine.FindStringSubmatch(page)
	if line == nil {
		t.Fatalf("the page has no row for the batch:\n%s", page)
	}
	if got, want := text(settingName.FindStringSubmatch(line[1])[1]), "2 of 3 left"; got != want {
		t.Errorf("the batch reads %q, want %q", got, want)
	}
	if got, want := text(settingMeta.FindStringSubmatch(line[1])[1]), "Made 3 Aug"; got != want {
		t.Errorf("the batch was %q, want %q", got, want)
	}
	if got, want := text(wideAction.FindStringSubmatch(page)[1]), "Create new codes"; got != want {
		t.Errorf("the button reads %q, want %q", got, want)
	}
}

func TestRecovery_AnAccountHoldingNoneIsToldSoAndOfferedASet(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.recovery, recoveryPath)

	if recoveryLine.MatchString(page) {
		t.Errorf("the page shows a batch for an account holding none:\n%s", page)
	}
	if !strings.Contains(page, "You are holding none") {
		t.Errorf("the page does not say the account is holding none:\n%s", page)
	}
	if got, want := text(wideAction.FindStringSubmatch(page)[1]), "Create codes"; got != want {
		t.Errorf("the button reads %q, want %q", got, want)
	}
}

func TestRecovery_AMemberIsNotToldThatOnlyAnOwnerIsPromptedForCodes(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities = auth.Capabilities{auth.TokenManage: true}

	page := f.page(t, f.handler.recovery, recoveryPath)

	if strings.Contains(page, "only an owner is prompted") {
		t.Errorf("the page tells a member only an owner is prompted:\n%s", page)
	}
	if got, want := text(wideAction.FindStringSubmatch(page)[1]), "Create codes"; got != want {
		t.Errorf("the button reads %q, want %q", got, want)
	}
}

func TestRecovery_AnotherAccountsCodesAreNotShownAsThisAccountsBatch(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'someone-elses')", otherUserID)

	page := f.page(t, f.handler.recovery, recoveryPath)

	if recoveryLine.MatchString(page) {
		t.Errorf("the page shows a batch that belongs to another account:\n%s", page)
	}
}

func TestRecovery_CreatingCodesShowsTenOnceAndStoresOnlyTheirHashes(t *testing.T) {
	f := moreGarden(t)

	rec := f.post(t, f.handler.createCodes, recoveryPath, url.Values{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, text(rec.Body.String()))
	}
	page := rec.Body.String()
	box := secretSection.FindStringSubmatch(page)
	if box == nil {
		t.Fatalf("the page has no box for the codes:\n%s", text(page))
	}
	var codes []string
	for _, m := range secretCode.FindAllStringSubmatch(box[1], -1) {
		codes = append(codes, m[1])
	}
	if len(codes) != 10 {
		t.Fatalf("the box lists %d codes, want 10:\n%s", len(codes), box[1])
	}
	if !strings.Contains(box[1], "This is the only time they are shown") {
		t.Errorf("the box does not say the codes are shown once:\n%s", box[1])
	}
	if !strings.Contains(page, `<a class="wide-action" href="`+accountPath+`">Done</a>`) {
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
	if secretSection.MatchString(again) {
		t.Errorf("the next request shows the codes again:\n%s", text(again))
	}
	line := recoveryLine.FindStringSubmatch(again)
	if line == nil {
		t.Fatalf("the page has no row for the batch:\n%s", text(again))
	}
	if got, want := text(settingName.FindStringSubmatch(line[1])[1]), "10 of 10 left"; got != want {
		t.Errorf("the batch reads %q, want %q", got, want)
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
