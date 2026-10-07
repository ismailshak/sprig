package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

type accountFixture struct {
	*moreFixture
	handler *account
}

func openAccount(t *testing.T) *accountFixture {
	t.Helper()

	f := moreGarden(t)
	return &accountFixture{
		moreFixture: f,
		handler:     &account{logger: testLogger, queries: store.New(f.tx), templates: testTemplates()},
	}
}

// valueOf returns the value rendered on the input with this id. It fails the
// test when the page has no such input.
func valueOf(t *testing.T, page, id string) string {
	t.Helper()

	input := readHTML(page).byID(id)
	if input == nil {
		t.Fatalf("the page has no input called %q:\n%s", id, page)
	}
	if !input.has("value") {
		t.Fatalf("the %q input has no value:\n%s", id, input)
	}
	return input.attr("value")
}

// saveAccount posts all three fields, because the page saves them together.
func (f *accountFixture) saveAccount(t *testing.T, name, handle, zone string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, f.handler.saveAccount, accountPath, url.Values{"name": {name}, "handle": {handle}, "timezone": {zone}})
}

type zoneChoice struct {
	value string
	label string
	on    bool
}

// zoneOptionsOf reads the options of the Timezone select.
func zoneOptionsOf(page string) []zoneChoice {
	var out []zoneChoice
	for _, option := range readHTML(page).byID("timezone").all(isTag("option")) {
		out = append(out, zoneChoice{value: option.attr("value"), label: option.text(), on: option.has("selected")})
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

func TestAccount_EditOpensTheFormOnTheNameHandleAndZoneTheAccountHolds(t *testing.T) {
	f := openAccount(t)

	page := f.page(t, f.handler.edit, accountEditPath)

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
	f := openAccount(t)
	f.principal.User.Timezone = "Europe/Belfast"

	page := f.page(t, f.handler.edit, accountEditPath)

	if got := selectedZone(t, page); got.value != "Europe/Belfast" {
		t.Errorf("the zone selected is %q, want Europe/Belfast", got.value)
	}
}

func TestAccount_TheZoneSelectOffersAZoneFromEveryRegion(t *testing.T) {
	f := openAccount(t)

	page := f.page(t, f.handler.edit, accountEditPath)

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
	f := openAccount(t)

	page := f.page(t, f.handler.edit, accountEditPath)

	zones := readHTML(page).byID("timezone")
	if zones == nil {
		t.Fatalf("the page has no Timezone select:\n%s", page)
	}
	if zones.has("data-propose") {
		t.Error("the select is marked data-propose, and Account opens on the zone the account holds")
	}
}

func TestAccount_AZoneReadsWithoutTheUnderscore(t *testing.T) {
	f := openAccount(t)

	page := f.page(t, f.handler.edit, accountEditPath)

	for _, zone := range zoneOptionsOf(page) {
		if zone.value == "America/New_York" && zone.label != "America/New York" {
			t.Errorf("America/New_York reads %q, want %q", zone.label, "America/New York")
		}
	}
}

func TestAccount_SavingWritesTheNameTheHandleAndTheZone(t *testing.T) {
	f := openAccount(t)

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
	f := openAccount(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.saveAccount(t, "Ellie", "ellie", "Asia/Tokyo")

	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, so the digest hour is read in the old zone until the job's timer fires", woken)
	}
}

func TestAccount_AnEmptyDisplayNameIsRefusedWithTheReason(t *testing.T) {
	f := openAccount(t)

	rec := f.saveAccount(t, "   ", "ellie", "Asia/Tokyo")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if page := text(rec.Body.String()); !strings.Contains(page, nameMissing) {
		t.Errorf("the refused form does not say %q:\n%s", nameMissing, page)
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
	f := openAccount(t)

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
	f := openAccount(t)

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
	f := openAccount(t)

	rec := f.saveAccount(t, "Ellie", "Sam", "Europe/London")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if want := handleTakenMessage("sam"); !strings.Contains(text(page), want) {
		t.Errorf("the refused form does not say %q:\n%s", want, text(page))
	}
	if got := valueOf(t, page, "handle"); got != "sam" {
		t.Errorf("the handle field holds %q after the refusal, want the handle the message names, sam", got)
	}
}

func TestAccount_SavingWithTheHandleUnchangedRedirectsBackToAccount(t *testing.T) {
	f := openAccount(t)

	rec := f.saveAccount(t, "Eleanor", "ellie", "Europe/London")

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != savedURL(accountPath) {
		t.Fatalf("status = %d to %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, savedURL(accountPath), rec.Body.String())
	}
}

func TestAccount_ThePageASaveRedirectsToSaysSavedAndAPlainVisitDoesNot(t *testing.T) {
	f := openAccount(t)

	if page := f.page(t, f.handler.show, savedURL(accountPath)); !strings.Contains(text(page), "Saved") {
		t.Errorf("the page after a save does not say Saved:\n%s", text(page))
	}
	if page := f.page(t, f.handler.show, accountPath); strings.Contains(text(page), "Saved") {
		t.Errorf("a plain visit says Saved:\n%s", text(page))
	}
}

func TestAccount_AnEmptyHandleIsRefusedWithTheReason(t *testing.T) {
	f := openAccount(t)

	rec := f.saveAccount(t, "Ellie", "   ", "Europe/London")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if page := text(rec.Body.String()); !strings.Contains(page, handleMissing) {
		t.Errorf("the refused form does not say %q:\n%s", handleMissing, page)
	}
}

func TestAccount_AHandleTypedWithACapitalAndASpaceIsSavedAsLowerCaseWithAnUnderscore(t *testing.T) {
	f := openAccount(t)

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

func TestAccount_AHandleOfPunctuationAloneIsRefusedWithTheReason(t *testing.T) {
	f := openAccount(t)

	rec := f.saveAccount(t, "Ellie", "!!!", "Europe/London")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if page := text(rec.Body.String()); !strings.Contains(page, handleMissing) {
		t.Errorf("the refused form does not say %q:\n%s", handleMissing, page)
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
	f := openAccount(t)

	rec := f.saveAccount(t, "  ", "  ", "Europe/London")

	page := text(rec.Body.String())
	for _, want := range []string{nameMissing, handleMissing} {
		if !strings.Contains(page, want) {
			t.Errorf("the refused form does not say %q:\n%s", want, page)
		}
	}
}

func TestAccount_TheRecoveryCodesRowSaysNoneYetUntilABatchExists(t *testing.T) {
	f := openAccount(t)

	if got := noteOn(t, f.page(t, f.handler.show, accountPath), "Recovery codes"); got != accountCodesNote {
		t.Errorf("holding none the row says %q, want %q", got, accountCodesNote)
	}

	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'one')", moreUserID)
	if got := noteOn(t, f.page(t, f.handler.show, accountPath), "Recovery codes"); got != "" {
		t.Errorf("holding a batch the row says %q, want nothing", got)
	}
}

func TestAccount_TheRecoveryCodesRowSaysNoneLeftWhenEveryCodeHasBeenUsed(t *testing.T) {
	f := openAccount(t)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash, used_at) VALUES ($1, 'spent-one', $2), ($1, 'spent-two', $2)", moreUserID, thursday)

	if got := noteOn(t, f.page(t, f.handler.show, accountPath), "Recovery codes"); got != accountCodesNoneLeftNote {
		t.Errorf("with every code used the row says %q, want %q", got, accountCodesNoneLeftNote)
	}
}

func TestAccount_ASaveSwapsInTheSavedValuesAsTextWithSavedUnderThem(t *testing.T) {
	f := openAccount(t)
	form := url.Values{"name": {"Eleanor"}, "handle": {"eleanor"}, "timezone": {"Asia/Tokyo"}}

	rec := f.swap(t, f.handler.saveAccount, accountPath, accountID, "", "", form)

	body := fragment(t, rec, accountID)
	account := readHTML(body).byID(accountID)
	if field := account.first(isTag("input")); field != nil {
		t.Errorf("the swap after a save holds a field, want the values as text:\n%s", field)
	}
	for _, want := range []string{"Eleanor", "eleanor", "Asia/Tokyo", "Saved"} {
		if !strings.Contains(account.text(), want) {
			t.Errorf("the swap after a save does not say %q:\n%s", want, account.text())
		}
	}
	if got := announcement(body); got != savedAnnouncement {
		t.Errorf("the swap announces %q, want %q", got, savedAnnouncement)
	}
}

func TestAccount_ASaveWithNoDisplayNameKeepsThePageAndReadsOutTheMessage(t *testing.T) {
	f := openAccount(t)
	form := url.Values{"name": {""}, "handle": {"ellie"}, "timezone": {"Europe/London"}}

	rec := f.swap(t, f.handler.saveAccount, accountPath, accountID, "", "", form)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	body := rec.Body.String()
	doc := readHTML(body)
	if doc.first(isTag("html")) != nil || doc.byID(accountID) == nil {
		t.Fatalf("the refusal is not a swap of the account element:\n%.200s", body)
	}
	if got := announcement(body); got != nameMissing {
		t.Errorf("the refusal announces %q, want %q", got, nameMissing)
	}
	if !strings.Contains(doc.byID(accountID).text(), nameMissing) || doc.byID("name") == nil {
		t.Errorf("the refusal does not show the form with %q in it:\n%s", nameMissing, doc.byID(accountID).text())
	}
}

func TestAccount_AMemberWhoCannotManagePeopleIsToldTheyHaveNoRecoveryCodes(t *testing.T) {
	f := openAccount(t)
	f.principal.Capabilities = auth.Capabilities{auth.TokenManage: true}

	if got := noteOn(t, f.page(t, f.handler.show, accountPath), "Recovery codes"); got != accountCodesNote {
		t.Errorf("the row says %q to a member, want %q", got, accountCodesNote)
	}
}

func TestAccount_ThePageLinksToCloseAccount(t *testing.T) {
	f := openAccount(t)

	page := f.page(t, f.handler.show, accountPath)

	if link := readHTML(page).first(isTag("a"), attrIs("href", closeAccountPath)); link.text() != "Close account" {
		t.Errorf("the link to %s reads %q, want Close account:\n%s", closeAccountPath, link.text(), text(page))
	}
}

func TestAccount_ThePageOpensWithTheValuesAsTextAndNoField(t *testing.T) {
	f := openAccount(t)

	account := readHTML(f.page(t, f.handler.show, accountPath)).byID(accountID)

	for _, tag := range []string{"form", "input", "select"} {
		if field := account.first(isTag(tag)); field != nil {
			t.Errorf("the page opens with a %s in it:\n%s", tag, field)
		}
	}
	for _, want := range []string{"Ellie", "ellie", "Europe/London"} {
		if !strings.Contains(account.text(), want) {
			t.Errorf("the page does not say %q:\n%s", want, account.text())
		}
	}
	edit := account.first(isTag("a"), textIs("Edit"))
	for name, want := range map[string]string{"href": accountEditPath, "hx-get": accountEditPath, "hx-target": "#" + accountID} {
		if got := edit.attr(name); got != want {
			t.Errorf("Edit has %s %q, want %q", name, got, want)
		}
	}
}

func TestAccount_AZoneWithAnUnderscoreIsShownWithASpace(t *testing.T) {
	f := openAccount(t)
	f.principal.User.Timezone = "America/New_York"

	account := readHTML(f.page(t, f.handler.show, accountPath)).byID(accountID)

	if !strings.Contains(account.text(), "America/New York") {
		t.Errorf("the page does not say America/New York:\n%s", account.text())
	}
}

func TestAccount_EditSwapsInTheFormWithCancel(t *testing.T) {
	f := openAccount(t)

	rec := f.swap(t, f.handler.edit, accountEditPath, accountID, "", "", nil)

	account := readHTML(fragment(t, rec, accountID)).byID(accountID)
	if form := account.first(isTag("form")); form.attr("action") != accountPath || form.attr("hx-target") != "#"+accountID {
		t.Errorf("the form posts to %q and swaps %q, want %s and #%s", form.attr("action"), form.attr("hx-target"), accountPath, accountID)
	}
	if got := account.byID("name").attr("value"); got != "Ellie" {
		t.Errorf("the display name field holds %q, want Ellie", got)
	}
	cancel := account.first(isTag("a"), textIs("Cancel"))
	for name, want := range map[string]string{"href": accountPath, "hx-get": accountPath, "hx-target": "#" + accountID} {
		if got := cancel.attr(name); got != want {
			t.Errorf("Cancel has %s %q, want %q", name, got, want)
		}
	}
}

func TestAccount_CancelSwapsInTheValuesAsText(t *testing.T) {
	f := openAccount(t)

	rec := f.swap(t, f.handler.show, accountPath, accountID, "", "", nil)

	account := readHTML(fragment(t, rec, accountID)).byID(accountID)
	if field := account.first(isTag("input")); field != nil {
		t.Errorf("the swap after Cancel holds a field, want the values as text:\n%s", field)
	}
	if !strings.Contains(account.text(), "Ellie") {
		t.Errorf("the swap after Cancel does not say Ellie:\n%s", account.text())
	}
}

// A link inside the account element is rendered again by every Edit, Save
// changes and Cancel swap.
func TestAccount_RecoveryCodesAndCloseAccountAreOutsideTheSwappedElement(t *testing.T) {
	f := openAccount(t)

	page := readHTML(f.page(t, f.handler.show, accountPath))

	for _, href := range []string{recoveryPath, closeAccountPath} {
		if page.first(isTag("a"), attrIs("href", href)) == nil {
			t.Errorf("the page has no link to %s", href)
		}
		if page.byID(accountID).first(isTag("a"), attrIs("href", href)) != nil {
			t.Errorf("the link to %s is inside the %s element", href, accountID)
		}
	}
}

func TestAccount_TheNameFieldHasAutofocusWhenTheFormOpensAndNotAfterARefusal(t *testing.T) {
	f := openAccount(t)

	opened := readHTML(f.page(t, f.handler.edit, accountEditPath))
	refused := readHTML(f.saveAccount(t, "Ellie", "", "Europe/London").Body.String())

	if !opened.byID("name").has("autofocus") {
		t.Error("the name field has no autofocus when the form opens")
	}
	if refused.byID("name").has("autofocus") {
		t.Error("the name field has autofocus after a refused post, and focus would leave the field with the error")
	}
}
