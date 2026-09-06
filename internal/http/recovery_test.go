package http

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

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

// No handler makes a batch yet, and plaintext codes only exist on the response
// that made them, so this renders the template directly.
func TestRecovery_ABatchIsListedInTheBoxThatSaysItIsTheOnlyTimeTheyAreShown(t *testing.T) {
	f := moreGarden(t)
	codes := []string{"k4rt-9wme-3xqd", "h72p-vc8n-md4s"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, recoveryPath, nil)
	f.handler.templates.render(rec, req, view{page: "recovery"}, recoveryPage{Codes: codes})

	box := secretSection.FindStringSubmatch(rec.Body.String())
	if box == nil {
		t.Fatalf("the page has no box for the codes:\n%s", rec.Body.String())
	}
	var listed []string
	for _, m := range secretCode.FindAllStringSubmatch(box[1], -1) {
		listed = append(listed, m[1])
	}
	if strings.Join(listed, " ") != strings.Join(codes, " ") {
		t.Errorf("the box lists %v, want %v", listed, codes)
	}
	if !strings.Contains(box[1], "This is the only time they are shown") {
		t.Errorf("the box does not say the codes are shown once:\n%s", box[1])
	}
}
