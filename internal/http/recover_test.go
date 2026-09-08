package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/auth/passkeytest"
	"github.com/ismailshak/sprig/internal/store"
)

// The recovery codes the fixture writes, in plaintext. The rows hold their
// hashes.
const (
	// elliesCode is an unused code of Ellie's. elliesUsedCode is one from the
	// same batch that has been used.
	elliesCode     = "k4rt-9wme-3xqd"
	elliesUsedCode = "h72p-vc8n-md4s"
	// samsCode is an unused code of Sam's, the second account.
	samsCode = "b9xa-3fkt-7rjw"
	// noSuchCode is the shape of a code and was never made.
	noSuchCode = "zzzz-zzzz-zzzz"
)

// recoverFixture is the Recover an account handler over Rosewood as the More
// tests seed it, with two codes for Ellie, one of them used, and one for Sam.
type recoverFixture struct {
	*moreFixture
	handler *recoverAccount
	queries *store.Queries
}

func recoverGarden(t *testing.T) *recoverFixture {
	t.Helper()

	f := moreGarden(t)
	f.exec(t, `INSERT INTO recovery_code (user_id, code_hash, generated_at, used_at)
		VALUES ($1, $2, $3, NULL), ($1, $4, $3, $3), ($5, $6, $3, NULL)`,
		moreUserID, auth.HashToken(elliesCode), thursday.AddDate(0, 0, -34), auth.HashToken(elliesUsedCode),
		otherUserID, auth.HashToken(samsCode))

	queries := store.New(f.tx)
	cookie := auth.CookieSettings{Name: "__Host-sprig_session", Secure: true}
	passkeys, err := auth.NewPasskeys(queries, "localhost", "sprig", "http://localhost:8080", cookie)
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	return &recoverFixture{
		moreFixture: f,
		handler: &recoverAccount{
			logger:    testLogger,
			passkeys:  passkeys,
			queries:   queries,
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
		queries: queries,
	}
}

// request calls handler with no principal on the request, because the routes
// are public. A nil form makes it a GET. The User-Agent is Chrome on a Mac, so
// a passkey saved by the request is named Mac · Chrome.
func (f *recoverFixture) request(t *testing.T, handler http.HandlerFunc, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	method, body := http.MethodGet, strings.NewReader("")
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", chromeOnMac)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func codeForm(code string) url.Values {
	return url.Values{codeField: {code}}
}

func (f *recoverFixture) check(t *testing.T, code string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, f.handler.check, recoverPath, codeForm(code))
}

// challenge posts code for a registration challenge and returns the options
// and the ceremony cookie. It fails the test on any status but 200.
func (f *recoverFixture) challenge(t *testing.T, code string) (*protocol.CredentialCreation, *http.Cookie) {
	t.Helper()

	rec := f.request(t, f.handler.challenge, recoverChallengePath, codeForm(code))
	if rec.Code != http.StatusOK {
		t.Fatalf("the challenge: status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var creation protocol.CredentialCreation
	if err := json.Unmarshal(rec.Body.Bytes(), &creation); err != nil {
		t.Fatalf("the challenge is not JSON: %v\n%s", err, rec.Body.String())
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_ceremony")
	if cookie == nil {
		t.Fatal("the challenge set no ceremony cookie")
	}
	return &creation, cookie
}

// answer posts the credential device made for creation, with code in the form's
// hidden input and cookie as the ceremony cookie.
func (f *recoverFixture) answer(t *testing.T, code string, creation *protocol.CredentialCreation, cookie *http.Cookie, device *passkeytest.Authenticator) *httptest.ResponseRecorder {
	t.Helper()

	form := codeForm(code)
	form.Set(credentialField, device.Register(creation))
	return f.request(t, f.handler.register, recoverPasskeyPath, form, cookie)
}

// register runs the whole ceremony for code on device: the post for the
// challenge, then the post with the credential it made.
func (f *recoverFixture) register(t *testing.T, code string, device *passkeytest.Authenticator) *httptest.ResponseRecorder {
	t.Helper()

	creation, cookie := f.challenge(t, code)
	return f.answer(t, code, creation, cookie, device)
}

// usedAt returns when code was used, or nil.
func (f *recoverFixture) usedAt(t *testing.T, code string) *time.Time {
	t.Helper()

	var at *time.Time
	if err := f.tx.QueryRow(t.Context(), "SELECT used_at FROM recovery_code WHERE code_hash = $1", auth.HashToken(code)).Scan(&at); err != nil {
		t.Fatalf("reading the code: %v", err)
	}
	return at
}

func (f *recoverFixture) count(t *testing.T, table string) int {
	t.Helper()
	return countRows(t, f.tx, table)
}

// cannotBeUsed fails the test unless rec is the "That code cannot be used"
// page, and returns the page.
func cannotBeUsed(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	page := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(page, codeCannotBeUsedTitle) || !strings.Contains(text(page), codeCannotBeUsedLine) {
		t.Errorf("the page does not say the code cannot be used:\n%s", text(page))
	}
	if !strings.Contains(page, `href="`+recoverPath+`">Try another code</a>`) {
		t.Errorf("the page has no link back to the form:\n%s", text(page))
	}
	if strings.Contains(page, "<form") {
		t.Errorf("the page has a form on it, and there is nothing to post:\n%s", text(page))
	}
	return page
}

// addDevice fails the test unless rec is the Add this device page with code
// in its hidden input, and returns the page.
func addDevice(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) string {
	t.Helper()

	page := rec.Body.String()
	if rec.Code != status {
		t.Errorf("status = %d, want %d:\n%s", rec.Code, status, text(page))
	}
	if !strings.Contains(page, `<h1 class="bare__title">`+addThisDevice+`</h1>`) {
		t.Errorf("the page is not Add this device:\n%s", text(page))
	}
	if !strings.Contains(page, `<input type="hidden" name="`+codeField+`" value="`+code+`">`) {
		t.Errorf("the form does not carry the code %q:\n%s", code, page)
	}
	if !strings.Contains(page, `action="`+recoverPasskeyPath+`"`) || !strings.Contains(page, `data-challenge="`+recoverChallengePath+`"`) {
		t.Errorf("the form does not post to the passkey route with the challenge route on it:\n%s", page)
	}
	if !strings.Contains(page, `<button class="wide-action" type="submit" disabled>`+registerLabel+`</button>`) {
		t.Errorf("the button is not %q, disabled until the script runs:\n%s", registerLabel, page)
	}
	return page
}

func TestRecover_ThePageIsOneCodeFieldAndSaysItCannotHelpSomebodyWhoNeverMadeCodes(t *testing.T) {
	f := recoverGarden(t)

	rec := f.request(t, f.handler.show, recoverPath, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, text(rec.Body.String()))
	}
	page := rec.Body.String()
	if !strings.Contains(page, `<h1 class="bare__title">`+recoverTitle+`</h1>`) {
		t.Errorf("the heading is not %q:\n%s", recoverTitle, text(page))
	}
	if !strings.Contains(page, `name="`+codeField+`" type="text"`) {
		t.Errorf("the page has no field for the code:\n%s", page)
	}
	if !strings.Contains(page, `action="`+recoverPath+`"`) || !strings.Contains(page, ">Continue</button>") {
		t.Errorf("the form does not post the code to the page's own URL:\n%s", page)
	}
	if strings.Contains(page, "<nav") {
		t.Errorf("the page has a tab bar, and nobody here is signed in:\n%s", page)
	}
	if !strings.Contains(page, "No recovery codes? Ask the garden’s owner for a new invite link.") {
		t.Errorf("the page does not say who it cannot help:\n%s", text(page))
	}
}

func TestRecover_ALiveCodeTypedInCapitalsAndSpacesOpensAddThisDevice(t *testing.T) {
	f := recoverGarden(t)

	rec := f.check(t, " K4RT 9WME3XQD\n")

	addDevice(t, rec, http.StatusOK, elliesCode)
	if !strings.Contains(rec.Body.String(), "Code accepted. Add a passkey on this device, then sign in with it.") {
		t.Errorf("the page does not say the code worked:\n%s", text(rec.Body.String()))
	}
	if at := f.usedAt(t, elliesCode); at != nil {
		t.Error("checking the code marked it used, and no passkey has been registered yet")
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("checking the code set a session cookie")
	}
}

func TestRecover_AUsedCodeACodeNobodyMadeAndSomethingThatIsNotACodeGetOnePage(t *testing.T) {
	f := recoverGarden(t)

	pages := map[string]string{}
	for _, code := range []string{elliesUsedCode, noSuchCode, "not a code", ""} {
		pages[code] = cannotBeUsed(t, f.check(t, code))
	}

	for code, page := range pages {
		if page != pages[noSuchCode] {
			t.Errorf("the page for %q differs from the page for a code nobody made, so the two can be told apart", code)
		}
	}
}

func TestRecover_RegisteringAddsAPasskeyToTheCodesAccountMarksItUsedAndStartsNoSession(t *testing.T) {
	f := recoverGarden(t)
	device := aDevice()

	rec := f.register(t, elliesCode, device)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signInPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, signInPath, text(rec.Body.String()))
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("the post set a session cookie, and a code buys a passkey and not a session")
	}
	if ceremony := cookieNamed(t, rec, "__Host-sprig_ceremony"); ceremony == nil || ceremony.MaxAge >= 0 {
		t.Error("the post did not clear the ceremony cookie")
	}
	keys, err := f.queries.ListPasskeys(t.Context(), moreUserID)
	if err != nil {
		t.Fatalf("listing the passkeys: %v", err)
	}
	if len(keys) != 3 || keys[2].CredentialID != device.CredentialID() || keys[2].Name != "Mac · Chrome" {
		t.Errorf("Ellie's passkeys are %+v, want the two seeded and the device, named after the browser", keys)
	}
	if n := f.count(t, "passkey_credential"); n != 4 {
		t.Errorf("%d passkeys in all, want the 3 seeded and the one added, so nothing went on another account", n)
	}
	if at := f.usedAt(t, elliesCode); at == nil || !at.Equal(thursday) {
		t.Errorf("the code was used at %v, want %v", at, thursday)
	}
	if at := f.usedAt(t, samsCode); at != nil {
		t.Error("another account's code was marked used")
	}
	cannotBeUsed(t, f.check(t, elliesCode))
}

func TestRecover_ThePasskeyGoesOnTheAccountTheCodeBelongsTo(t *testing.T) {
	f := recoverGarden(t)
	device := aDevice()

	rec := f.register(t, samsCode, device)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	sams, err := f.queries.ListPasskeys(t.Context(), otherUserID)
	if err != nil {
		t.Fatalf("listing Sam's passkeys: %v", err)
	}
	if len(sams) != 2 || sams[1].CredentialID != device.CredentialID() {
		t.Errorf("Sam's passkeys are %+v, want the seeded one and the device", sams)
	}
	ellies, err := f.queries.ListPasskeys(t.Context(), moreUserID)
	if err != nil {
		t.Fatalf("listing Ellie's passkeys: %v", err)
	}
	if len(ellies) != 2 {
		t.Errorf("Ellie holds %d passkeys, want the 2 seeded, because the code was not hers", len(ellies))
	}
	if at := f.usedAt(t, elliesCode); at != nil {
		t.Error("Ellie's code was marked used by a post that named Sam's")
	}
}

func TestRecover_TheSameCodeAnsweredTwiceSavesOnePasskey(t *testing.T) {
	f := recoverGarden(t)
	first, firstCookie := f.challenge(t, elliesCode)
	second, secondCookie := f.challenge(t, elliesCode)

	if rec := f.answer(t, elliesCode, first, firstCookie, aDevice()); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first answer: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	rec := f.answer(t, elliesCode, second, secondCookie, aDevice())

	cannotBeUsed(t, rec)
	if n := f.count(t, "passkey_credential"); n != 4 {
		t.Errorf("%d passkeys, want the 3 seeded and one added", n)
	}
}

func TestRecover_ADeviceThatDidNotCheckWhoWasUsingItIsRefusedAndTheCodeStaysLive(t *testing.T) {
	f := recoverGarden(t)
	device := aDevice()
	device.Verified = false

	rec := f.register(t, elliesCode, device)

	page := addDevice(t, rec, http.StatusUnprocessableEntity, elliesCode)
	if !strings.Contains(page, "This device didn’t verify you.") {
		t.Errorf("the page does not say the device did not verify:\n%s", text(page))
	}
	if at := f.usedAt(t, elliesCode); at != nil {
		t.Error("the refused post marked the code used")
	}
	if n := f.count(t, "passkey_credential"); n != 3 {
		t.Errorf("%d passkeys, want the 3 seeded", n)
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("the refused post set a session cookie")
	}
}

func TestRecover_TheChallengeForACodeThatIsNotLiveIs422AndWritesNoCeremony(t *testing.T) {
	f := recoverGarden(t)

	for _, code := range []string{elliesUsedCode, noSuchCode, ""} {
		rec := f.request(t, f.handler.challenge, recoverChallengePath, codeForm(code))
		if rec.Code != http.StatusUnprocessableEntity || rec.Body.Len() != 0 {
			t.Errorf("the challenge for %q: status = %d with %d bytes, want %d and no body", code, rec.Code, rec.Body.Len(), http.StatusUnprocessableEntity)
		}
	}
	if n := f.count(t, "webauthn_ceremony"); n != 0 {
		t.Errorf("%d ceremony rows, want none", n)
	}
}

func TestRecover_AnAnswerWithNoCeremonyLeavesTheCodeLiveAndSaysToPressAgain(t *testing.T) {
	f := recoverGarden(t)

	rec := f.request(t, f.handler.register, recoverPasskeyPath, url.Values{codeField: {elliesCode}, credentialField: {"{}"}})

	page := addDevice(t, rec, http.StatusUnprocessableEntity, elliesCode)
	if !strings.Contains(page, "The request timed out. Try again.") {
		t.Errorf("the page does not say the request expired:\n%s", text(page))
	}
	if at := f.usedAt(t, elliesCode); at != nil {
		t.Error("an answer with no ceremony marked the code used")
	}
}

// recoverMux is the whole handler over the fixture's transaction, so a
// request to a recover route goes through the rate limiters the route table
// wraps it in. The trusted header is empty, so an address is the request's
// RemoteAddr.
func recoverMux(t *testing.T, f *recoverFixture) http.Handler {
	t.Helper()
	return New(testLogger, testSessions(), f.handler.passkeys, rejectEveryToken, noLiveToken, f.queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)
}

func TestRecover_TheNinthCodePostedFromOneAddressInAMinuteIsRefusedWithTooManyAttempts(t *testing.T) {
	f := recoverGarden(t)
	handler := recoverMux(t, f)

	for i := range 8 {
		if rec := postFrom(t, handler, recoverPath, "203.0.113.1", codeForm(noSuchCode)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("post %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
		}
	}
	rec := postFrom(t, handler, recoverPath, "203.0.113.1", codeForm(elliesCode))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the ninth post: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	page := rec.Body.String()
	if !strings.Contains(page, `<h1 class="bare__title">`+tooManyAttemptsTitle+`</h1>`) || !strings.Contains(text(page), tooManyAttemptsLine) {
		t.Errorf("the page does not say there were too many attempts:\n%s", text(page))
	}
	if strings.Contains(page, "<form") || strings.Contains(page, addThisDevice) {
		t.Errorf("the ninth post checked the code, and a live one opened the registration form:\n%s", text(page))
	}
	if rec := postFrom(t, handler, recoverPath, "203.0.113.2", codeForm(noSuchCode)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("another address: status = %d, want %d, so the budget spent was one address's", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestRecover_TheThreeRoutesShareOneBudgetOfTenAMinuteAcrossEveryAddress(t *testing.T) {
	f := recoverGarden(t)
	handler := recoverMux(t, f)

	// Ten posts from ten addresses, spread over the three routes, each one
	// well inside its own address's limit of six.
	paths := []string{recoverPath, recoverChallengePath, recoverPasskeyPath}
	for i := range 10 {
		address := fmt.Sprintf("203.0.113.%d", 10+i)
		if rec := postFrom(t, handler, paths[i%3], address, codeForm(noSuchCode)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("post %d to %s: status = %d, want %d:\n%s", i+1, paths[i%3], rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
		}
	}
	rec := postFrom(t, handler, recoverChallengePath, "203.0.113.99", codeForm(elliesCode))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the eleventh post: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if strings.TrimSpace(rec.Body.String()) != tooManyAttemptsTitle+". "+tooManyAttemptsLine {
		t.Errorf("the body is %q, want the rate-limit sentence alone, because the page's script shows it as it is", rec.Body.String())
	}
	if n := f.count(t, "webauthn_ceremony"); n != 0 {
		t.Errorf("%d ceremony rows, want none, because the eleventh post was refused before it looked the code up", n)
	}
}
