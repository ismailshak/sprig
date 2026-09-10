package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ismailshak/sprig/internal/auth"
)

var (
	zoneOptionTag   = regexp.MustCompile(`<option value="([^"]+)"(?: data-also="[^"]+")?( selected)?>([^<]+)</option>`)
	fieldBlock      = regexp.MustCompile(`(?s)<div class="field">(.*?)</div>`)
	fieldInput      = regexp.MustCompile(`id="([^"]+)" name=`)
	fieldInputValue = regexp.MustCompile(`value="([^"]*)"`)
	fieldError      = regexp.MustCompile(`(?s)<p class="field__error">(.*?)</p>`)
)

// valueOf returns the value rendered on the input with this id. It fails the
// test when the page has no such input.
func valueOf(t *testing.T, page, id string) string {
	t.Helper()

	block := fieldWithInput(page, id)
	if block == "" {
		t.Fatalf("the page has no field with an input called %q:\n%s", id, page)
	}
	value := fieldInputValue.FindStringSubmatch(block)
	if value == nil {
		t.Fatalf("the %q input has no value:\n%s", id, block)
	}
	return value[1]
}

// errorUnder returns the error message rendered under the field whose input
// has this id, or "" when it has none.
func errorUnder(page, id string) string {
	block := fieldWithInput(page, id)
	if message := fieldError.FindStringSubmatch(block); message != nil {
		return text(message[1])
	}
	return ""
}

// fieldWithInput returns the markup of the field whose input has this id, or ""
// when the page has no such field.
func fieldWithInput(page, id string) string {
	for _, block := range fieldBlock.FindAllStringSubmatch(page, -1) {
		if input := fieldInput.FindStringSubmatch(block[1]); input != nil && input[1] == id {
			return block[1]
		}
	}
	return ""
}

// saveAccount posts all three fields, because the page saves them together.
func (f *moreFixture) saveAccount(t *testing.T, name, handle, zone string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, f.handler.saveAccount, accountPath, url.Values{"name": {name}, "handle": {handle}, "timezone": {zone}})
}

type zoneChoice struct {
	value string
	label string
	on    bool
}

func zoneOptionsOf(page string) []zoneChoice {
	var out []zoneChoice
	for _, m := range zoneOptionTag.FindAllStringSubmatch(page, -1) {
		out = append(out, zoneChoice{value: m[1], label: m[3], on: m[2] != ""})
	}
	return out
}

func selectedZone(t *testing.T, page string) zoneChoice {
	t.Helper()

	for _, zone := range zoneOptionsOf(page) {
		if zone.on {
			return zone
		}
	}
	t.Fatalf("no timezone is selected:\n%s", page)
	return zoneChoice{}
}

func TestAccount_TheFormOpensOnTheNameHandleAndZoneTheAccountHolds(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.account, accountPath)

	if got := valueOf(t, page, "name"); got != "Ellie" {
		t.Errorf("the display name field holds %q, want Ellie", got)
	}
	if got := valueOf(t, page, "handle"); got != "ellie" {
		t.Errorf("the handle field holds %q, want ellie", got)
	}
	if got := selectedZone(t, page); got.value != "Europe/London" {
		t.Errorf("the zone selected is %q, want Europe/London", got.value)
	}
}

// Europe/Belfast is an old name the tz database keeps for Europe/London.
// zone.tab does not list it, so it is a zone an account can hold that the
// select does not offer.
func TestAccount_AZoneTheSelectDoesNotListIsStillOfferedAndSelected(t *testing.T) {
	f := moreGarden(t)
	f.principal.User.Timezone = "Europe/Belfast"

	page := f.page(t, f.handler.account, accountPath)

	if got := selectedZone(t, page); got.value != "Europe/Belfast" {
		t.Errorf("the zone selected is %q, want Europe/Belfast", got.value)
	}
}

func TestAccount_TheZoneSelectOffersAZoneFromEveryRegion(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.account, accountPath)

	offered := map[string]bool{}
	for _, zone := range zoneOptionsOf(page) {
		offered[zone.value] = true
	}
	for _, zone := range []string{"Africa/Lagos", "America/Argentina/Buenos_Aires", "Asia/Kolkata", "Europe/Madrid", "Pacific/Auckland"} {
		if !offered[zone] {
			t.Errorf("%s is not offered", zone)
		}
	}
}

func TestAccount_TheZoneSelectIsNotMarkedForTheBrowsersZone(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.account, accountPath)

	if strings.Contains(page, "data-propose") {
		t.Error("the select is marked data-propose, and Account opens on the zone the account holds")
	}
}

func TestAccount_AZoneReadsWithoutTheUnderscore(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.account, accountPath)

	for _, zone := range zoneOptionsOf(page) {
		if zone.value == "America/New_York" && zone.label != "America/New York" {
			t.Errorf("America/New_York reads %q, want %q", zone.label, "America/New York")
		}
	}
}

func TestAccount_SavingWritesTheNameTheHandleAndTheZone(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "  Eleanor  ", "  eleanor  ", "Asia/Tokyo")

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != savedURL(accountPath) {
		t.Fatalf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, savedURL(accountPath))
	}
	var name, handle, zone string
	if err := f.tx.QueryRow(t.Context(), "SELECT display_name, handle, timezone FROM app_user WHERE id = $1", moreUserID).Scan(&name, &handle, &zone); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if name != "Eleanor" || handle != "eleanor" || zone != "Asia/Tokyo" {
		t.Errorf("the account holds %q, %q in %q, want %q, %q in %q",
			name, handle, zone, "Eleanor", "eleanor", "Asia/Tokyo")
	}
}

func TestAccount_SavingTheTimezoneWakesTheDigestJob(t *testing.T) {
	f := moreGarden(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.saveAccount(t, "Ellie", "ellie", "Asia/Tokyo")

	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, so the digest hour is read in the old zone until the job's timer fires", woken)
	}
}

func TestAccount_AnEmptyDisplayNameIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "   ", "ellie", "Asia/Tokyo")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorUnder(rec.Body.String(), "name"); got != nameMissing {
		t.Errorf("the form says %q under Display name, want %q", got, nameMissing)
	}
	var name string
	if err := f.tx.QueryRow(t.Context(), "SELECT display_name FROM app_user WHERE id = $1", moreUserID).Scan(&name); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if name != "Ellie" {
		t.Errorf("the refused post left the name as %q, want Ellie", name)
	}
}

func TestAccount_TheRefusedFormComesBackWithWhatWasTyped(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "", "eleanor", "Asia/Tokyo")

	page := rec.Body.String()
	if got := selectedZone(t, page); got.value != "Asia/Tokyo" {
		t.Errorf("the zone selected is %q, want the posted Asia/Tokyo", got.value)
	}
	if got := valueOf(t, page, "handle"); got != "eleanor" {
		t.Errorf("the handle field holds %q, want the posted eleanor", got)
	}
}

func TestAccount_AZoneTheSelectDidNotOfferIsRefused(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "Eleanor", "eleanor", "Mars/Olympus_Mons")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var name, zone string
	if err := f.tx.QueryRow(t.Context(), "SELECT display_name, timezone FROM app_user WHERE id = $1", moreUserID).Scan(&name, &zone); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if name != "Ellie" || zone != "Europe/London" {
		t.Errorf("the refused post left the account as %q in %q, want Ellie in Europe/London", name, zone)
	}
}

// Another seeded account has the handle "sam". "Sam" normalises to it, so the
// update is rejected by the unique index.
func TestAccount_AHandleAnotherAccountHoldsIsRefusedAndTheMessageNamesIt(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "Ellie", "Sam", "Europe/London")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got, want := errorUnder(rec.Body.String(), "handle"), handleTakenMessage("sam"); got != want {
		t.Errorf("the form says %q under Handle, want %q", got, want)
	}
}

func TestAccount_SavingWithTheHandleUnchangedRedirectsBackToAccount(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "Eleanor", "ellie", "Europe/London")

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != savedURL(accountPath) {
		t.Fatalf("status = %d to %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, savedURL(accountPath), rec.Body.String())
	}
}

func TestAccount_ThePageASaveRedirectsToSaysSavedAndAPlainVisitDoesNot(t *testing.T) {
	f := moreGarden(t)

	if page := f.page(t, f.handler.account, savedURL(accountPath)); !strings.Contains(text(page), "Saved") {
		t.Errorf("the page after a save does not say Saved:\n%s", text(page))
	}
	if page := f.page(t, f.handler.account, accountPath); strings.Contains(text(page), "Saved") {
		t.Errorf("a plain visit says Saved:\n%s", text(page))
	}
}

func TestAccount_AnEmptyHandleIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "Ellie", "   ", "Europe/London")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorUnder(rec.Body.String(), "handle"); got != handleMissing {
		t.Errorf("the form says %q under Handle, want %q", got, handleMissing)
	}
}

func TestAccount_AHandleTypedWithACapitalAndASpaceIsSavedAsLowerCaseWithAnUnderscore(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "Ellie", "Emma Fletcher", "Europe/London")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	var held string
	if err := f.tx.QueryRow(t.Context(), "SELECT handle FROM app_user WHERE id = $1", moreUserID).Scan(&held); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if held != "emma_fletcher" {
		t.Errorf("the account holds %q, want emma_fletcher", held)
	}
}

func TestAccount_AHandleOfPunctuationAloneIsRefusedWithTheReasonUnderTheField(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "Ellie", "!!!", "Europe/London")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if got := errorUnder(rec.Body.String(), "handle"); got != handleMissing {
		t.Errorf("the form says %q under Handle, want %q", got, handleMissing)
	}
	var held string
	if err := f.tx.QueryRow(t.Context(), "SELECT handle FROM app_user WHERE id = $1", moreUserID).Scan(&held); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if held != "ellie" {
		t.Errorf("the refused post left the handle as %q, want ellie", held)
	}
}

func TestAccount_BothMessagesAreShownWhenTheNameAndTheHandleAreBothEmpty(t *testing.T) {
	f := moreGarden(t)

	rec := f.saveAccount(t, "  ", "  ", "Europe/London")

	page := rec.Body.String()
	if got := errorUnder(page, "name"); got != nameMissing {
		t.Errorf("the form says %q under Display name, want %q", got, nameMissing)
	}
	if got := errorUnder(page, "handle"); got != handleMissing {
		t.Errorf("the form says %q under Handle, want %q", got, handleMissing)
	}
}

func TestAccount_TheRecoveryCodesRowSaysNoneYetUntilABatchExists(t *testing.T) {
	f := moreGarden(t)

	if got := noteOn(t, f.page(t, f.handler.account, accountPath), "Recovery codes"); got != accountCodesNote {
		t.Errorf("holding none the row says %q, want %q", got, accountCodesNote)
	}

	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'one')", moreUserID)
	if got := noteOn(t, f.page(t, f.handler.account, accountPath), "Recovery codes"); got != "" {
		t.Errorf("holding a batch the row says %q, want nothing", got)
	}
}

func TestAccount_TheRecoveryCodesRowSaysNoneLeftWhenEveryCodeHasBeenUsed(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash, used_at) VALUES ($1, 'spent-one', $2), ($1, 'spent-two', $2)", moreUserID, thursday)

	if got := noteOn(t, f.page(t, f.handler.account, accountPath), "Recovery codes"); got != accountCodesNoneLeftNote {
		t.Errorf("with every code used the row says %q, want %q", got, accountCodesNoneLeftNote)
	}
}

func TestAccount_ASaveShowsSavedUnderTheButtonWithoutReloadingThePage(t *testing.T) {
	f := moreGarden(t)
	form := url.Values{"name": {"Eleanor"}, "handle": {"eleanor"}, "timezone": {"Asia/Tokyo"}}

	rec := f.swap(t, f.handler.saveAccount, accountPath, accountID, "", "", form)

	body := fragment(t, rec, accountID)
	page := withoutAnnouncement(body)
	if !strings.Contains(page, `value="Eleanor"`) || !strings.Contains(text(page), "Saved") {
		t.Errorf("the swap does not show the saved name with Saved under the button:\n%s", text(page))
	}
	if !strings.Contains(body, announced(savedAnnouncement)) {
		t.Errorf("the swap does not announce the save:\n%s", body)
	}
}

func TestAccount_ASaveWithNoDisplayNameKeepsThePageAndReadsOutTheMessage(t *testing.T) {
	f := moreGarden(t)
	form := url.Values{"name": {""}, "handle": {"ellie"}, "timezone": {"Europe/London"}}

	rec := f.swap(t, f.handler.saveAccount, accountPath, accountID, "", "", form)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.HasPrefix(body, "<!doctype html>") || !strings.Contains(body, `id="`+accountID+`"`) {
		t.Fatalf("the refusal is not a swap of the page under the top bar:\n%.200s", body)
	}
	if !strings.Contains(body, announced(nameMissing)) {
		t.Errorf("the refusal does not announce the message:\n%s", body)
	}
}

func TestAccount_AMemberIsNotToldTheyHaveNoRecoveryCodes(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities = auth.Capabilities{auth.TokenManage: true}

	if got := noteOn(t, f.page(t, f.handler.account, accountPath), "Recovery codes"); got != "" {
		t.Errorf("the row says %q to a member, want nothing", got)
	}
}
