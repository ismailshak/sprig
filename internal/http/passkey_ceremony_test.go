package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/auth/passkeytest"
	"github.com/ismailshak/sprig/internal/store"
)

// ceremonyOn returns the handler serving the four passkey requests, over the
// fixture's own transaction, with the Secure cookies a deployment uses.
func ceremonyOn(t *testing.T, f *moreFixture) *passkeyCeremony {
	t.Helper()
	return ceremonyWithCookies(t, f, auth.CookieSettings{Name: "__Host-sprig_session", Secure: true})
}

// ceremonyWithCookies is ceremonyOn with the session cookie's settings chosen,
// because the ceremony cookie takes its name and its Secure flag from them.
func ceremonyWithCookies(t *testing.T, f *moreFixture, cookie auth.CookieSettings) *passkeyCeremony {
	t.Helper()

	queries := store.New(f.tx)
	passkeys, err := auth.NewPasskeys(queries, "localhost", "sprig", "http://localhost:8080", cookie)
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	return &passkeyCeremony{
		logger:    testLogger,
		passkeys:  passkeys,
		sessions:  auth.NewSessions(queries, testTTL, cookie),
		queries:   queries,
		templates: testTemplates(),
		now:       f.handler.now,
	}
}

// post calls one of the ceremony handlers as a form post, with the fixture's
// principal on the request. Any cookies given are added to it. The ceremony
// cookie is how a post says which challenge it belongs to.
func (f *moreFixture) post(t *testing.T, handler http.HandlerFunc, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	body := strings.NewReader(form.Encode())
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// challengeCookie starts a ceremony with handler and returns the cookie the
// request answering it must present.
func (f *moreFixture) challengeCookie(t *testing.T, handler http.HandlerFunc, path string) *http.Cookie {
	t.Helper()

	rec := f.post(t, handler, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("starting a challenge: status = %d:\n%s", rec.Code, rec.Body.String())
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_ceremony")
	if cookie == nil {
		t.Fatal("the challenge set no ceremony cookie")
	}
	return cookie
}

// buttonLabelled returns the button whose text is label. It fails the test
// when the page has no such button.
func buttonLabelled(t *testing.T, page, label string) *element {
	t.Helper()

	button := readHTML(page).first(isTag("button"), textIs(label))
	if button == nil {
		t.Fatalf("the page has no %s button:\n%s", label, page)
	}
	return button
}

// buttonNamed returns the markup of the button whose text is label. It fails
// the test when the page has no such button.
func buttonNamed(t *testing.T, page, label string) string {
	t.Helper()
	return buttonLabelled(t, page, label).String()
}

// buttonIsDisabled reports whether the button whose text is label is disabled.
func buttonIsDisabled(t *testing.T, page, label string) bool {
	t.Helper()
	return buttonLabelled(t, page, label).has("disabled")
}

// formTo returns the form on page that posts to action, or nil.
func formTo(page, action string) *element {
	return readHTML(page).first(isTag("form"), attrIs("action", action))
}

// hiddenValue returns the value of the hidden input named name inside e. The
// bool is false when e holds no such input.
func hiddenValue(e *element, name string) (string, bool) {
	input := e.first(isTag("input"), attrIs("type", "hidden"), attrIs("name", name))
	return input.attr("value"), input != nil
}

// hasTabBar reports whether page renders the tab bar. The tab bar is the only
// nav element the layout renders.
func hasTabBar(page string) bool {
	return readHTML(page).first(isTag("nav")) != nil
}

// selectedValue returns the value of the option chosen in sel. It is empty
// when no option is chosen.
func selectedValue(sel *element) string {
	return sel.first(isTag("option"), hasAttr("selected")).attr("value")
}

func TestPasskeys_TheCeremonyCookieDropsTheHostPrefixWhereCookiesAreNotSecure(t *testing.T) {
	f := moreGarden(t)
	// A browser drops a __Host- cookie that is not Secure, and the post that
	// finishes a ceremony would then have no way to name its challenge.
	h := ceremonyWithCookies(t, f, auth.CookieSettings{Name: "sprig_session", Secure: false})

	rec := f.post(t, h.registerChallenge, registerPath, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	cookie := cookieNamed(t, rec, "sprig_ceremony")
	if cookie == nil {
		t.Fatalf("the challenge set no sprig_ceremony cookie:\n%v", rec.Result().Cookies())
	}
	if cookie.Secure {
		t.Error("the ceremony cookie is Secure on a deployment whose session cookie is not")
	}
}

func TestPasskeys_AChallengeSetsACookieScriptCannotRead(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)

	rec := f.post(t, h.registerChallenge, registerPath, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_ceremony")
	if cookie == nil {
		t.Fatalf("the challenge set no ceremony cookie, so its answer has no way to say which challenge it is for")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("the ceremony cookie is %+v, want HttpOnly and SameSite=Lax", cookie)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store, since a challenge is answerable once", rec.Header().Get("Cache-Control"))
	}
	var body struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the challenge is not JSON: %v\n%s", err, rec.Body.String())
	}
	if body.PublicKey.Challenge == "" {
		t.Errorf("the response holds no challenge:\n%s", rec.Body.String())
	}
}

func TestPasskeys_AnAnswerWithNoChallengeBehindItSaysTheRequestExpired(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)

	// A request with no ceremony cookie is refused the same way as one whose
	// challenge expired, because neither finds a row.
	rec := f.post(t, h.register, passkeysPath, url.Values{credentialField: {"{}"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(text(rec.Body.String()), "The request timed out") {
		t.Errorf("the page does not say the request expired:\n%s", text(rec.Body.String()))
	}
	// The page it renders still lists the devices already enrolled.
	if !strings.Contains(text(rec.Body.String()), "MacBook Air") {
		t.Errorf("the page lost the passkeys the account already has:\n%s", text(rec.Body.String()))
	}
}

func TestPasskeys_AnAnswerThatIsNotACredentialIsRefused(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)

	cookie := f.challengeCookie(t, h.registerChallenge, registerPath)

	rec := f.post(t, h.register, passkeysPath, url.Values{credentialField: {"not a credential"}}, cookie)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d:\n%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestPasskeys_ASignInAnswerThatIsNotACredentialIsRefused(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)

	cookie := f.challengeCookie(t, h.signInChallenge, challengePath)

	rec := f.post(t, h.signIn, signInPath, url.Values{credentialField: {""}}, cookie)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d:\n%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("the refused sign-in set a session cookie")
	}
}

// signInMux is the whole handler over the fixture's transaction, so a request
// to a sign-in route goes through the rate limiters the route table wraps it
// in. The trusted header is empty, so an address is the request's RemoteAddr.
func signInMux(t *testing.T, f *moreFixture) http.Handler {
	t.Helper()

	queries := store.New(f.tx)
	cookie := auth.CookieSettings{Name: "__Host-sprig_session", Secure: true}
	passkeys, err := auth.NewPasskeys(queries, "localhost", "sprig", "http://localhost:8080", cookie)
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	sessions := auth.NewSessions(queries, testTTL, cookie)
	deps := testDependencies(t)
	deps.Sessions = sessions
	deps.Passkeys = passkeys
	deps.Queries = queries
	return New(deps)
}

// challengeFrom posts for a sign-in challenge from address.
func challengeFrom(t *testing.T, handler http.Handler, address string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, challengePath, nil)
	req.RemoteAddr = address + ":40000"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ceremonies counts the rows in webauthn_ceremony.
func ceremonies(t *testing.T, f *moreFixture) int {
	t.Helper()

	var rows int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM webauthn_ceremony").Scan(&rows); err != nil {
		t.Fatalf("counting the ceremonies: %v", err)
	}
	return rows
}

func TestSignIn_AnAddressPastItsBudgetIsRefusedAndStartsNoCeremony(t *testing.T) {
	f := moreGarden(t)
	handler := signInMux(t, f)

	for i := range 6 {
		if rec := challengeFrom(t, handler, "203.0.113.1"); rec.Code != http.StatusOK {
			t.Fatalf("challenge %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	rec := challengeFrom(t, handler, "203.0.113.1")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the seventh challenge in a minute: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("the refusal sets no Retry-After, so nothing says when to try again")
	}
	if strings.TrimSpace(rec.Body.String()) != tooManySignIns {
		t.Errorf("the refusal's body is %q, want the sentence the page shows above the button", rec.Body.String())
	}
	if got := ceremonies(t, f); got != 6 {
		t.Errorf("%d challenges are waiting, want 6, so the refused request started one", got)
	}
}

func TestSignIn_AnAddressIsStillServedWhenAnotherHasSpentItsBudget(t *testing.T) {
	f := moreGarden(t)
	handler := signInMux(t, f)

	for range 6 {
		challengeFrom(t, handler, "203.0.113.1")
	}
	if rec := challengeFrom(t, handler, "203.0.113.1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the address that spent its budget: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	rec := challengeFrom(t, handler, "198.51.100.7")

	if rec.Code != http.StatusOK {
		t.Errorf("a second address: status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// aDevice is a phone with a screen lock, synced by the platform, at the
// origin the test handlers are built for.
func aDevice() *passkeytest.Authenticator {
	device := passkeytest.New("localhost", "http://localhost:8080")
	device.BackupEligible = true
	return device
}

// challengeJSON posts to handler for a challenge and decodes the options it
// returns into v. It returns the ceremony cookie the next post has to send.
func (f *moreFixture) challengeJSON(t *testing.T, handler http.HandlerFunc, path string, v any) *http.Cookie {
	t.Helper()

	rec := f.post(t, handler, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("starting a challenge: status = %d:\n%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("the challenge is not JSON: %v\n%s", err, rec.Body.String())
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_ceremony")
	if cookie == nil {
		t.Fatal("the challenge set no ceremony cookie")
	}
	return cookie
}

// registerDevice runs a registration through the two handlers and returns the
// response to the second post.
func (f *moreFixture) registerDevice(t *testing.T, h *passkeyCeremony, device *passkeytest.Authenticator) *httptest.ResponseRecorder {
	t.Helper()

	var creation protocol.CredentialCreation
	cookie := f.challengeJSON(t, h.registerChallenge, registerPath, &creation)
	return f.post(t, h.register, passkeysPath, url.Values{credentialField: {device.Register(&creation)}}, cookie)
}

// signInWith runs a sign-in through the two handlers and returns the response
// to the second post.
func (f *moreFixture) signInWith(t *testing.T, h *passkeyCeremony, device *passkeytest.Authenticator) *httptest.ResponseRecorder {
	t.Helper()

	var assertion protocol.CredentialAssertion
	cookie := f.challengeJSON(t, h.signInChallenge, challengePath, &assertion)
	return f.post(t, h.signIn, signInPath, url.Values{credentialField: {device.Assert(&assertion)}}, cookie)
}

// enrolDevice is registerDevice for a registration the test expects to be
// accepted.
func (f *moreFixture) enrolDevice(t *testing.T, h *passkeyCeremony, device *passkeytest.Authenticator) {
	t.Helper()

	rec := f.registerDevice(t, h, device)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != passkeysPath {
		t.Fatalf("registering: status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, passkeysPath, text(rec.Body.String()))
	}
}

func TestPasskeys_ARegisteredDeviceIsOnTheListAndSignsIn(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()

	f.enrolDevice(t, h, device)

	page := f.page(t, passkeysOn(f).handler.show, passkeysPath)
	if got := len(readHTML(page).all(isTag("button"), textIs("Remove"))); got != 3 {
		t.Errorf("the page offers Remove %d times, want 3 for the two seeded devices and the new one:\n%s", got, text(page))
	}

	rec := f.signInWith(t, h, device)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("signing in: status = %d, Location = %q, want %d to /:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, rec.Body.String())
	}
	if cookieNamed(t, rec, "__Host-sprig_session") == nil {
		t.Error("the sign-in set no session cookie")
	}
	if cookie := cookieNamed(t, rec, "__Host-sprig_ceremony"); cookie == nil || cookie.MaxAge != -1 {
		t.Errorf("the sign-in left the ceremony cookie in place: %+v", cookie)
	}
}

func TestSignIn_ACopiedPasskeyIsRefusedAndTheLogNamesThePasskey(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	var log bytes.Buffer
	h.logger = slog.New(slog.NewJSONHandler(&log, nil))
	key := aDevice()
	key.BackupEligible = false
	key.Counter = 1
	f.enrolDevice(t, h, key)

	key.Counter = 2
	if rec := f.signInWith(t, h, key); rec.Code != http.StatusSeeOther {
		t.Fatalf("the original key: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	copied := key.Clone()
	copied.Counter = 2
	rec := f.signInWith(t, h, copied)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the copy: status = %d, want %d:\n%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("the refused sign-in set a session cookie")
	}
	if !strings.Contains(log.String(), `"level":"WARN"`) || !strings.Contains(log.String(), "copy") {
		t.Errorf("the log does not warn about the copy:\n%s", log.String())
	}
	if !strings.Contains(log.String(), key.CredentialID()) && !strings.Contains(log.String(), moreUserID.String()) {
		t.Errorf("the log names neither the passkey nor the account:\n%s", log.String())
	}
}

func TestSignIn_AnAnswerThatDoesNotCheckOutIsRefusedAndIsNotAServerError(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	var log bytes.Buffer
	h.logger = slog.New(slog.NewJSONHandler(&log, nil))
	device := aDevice()
	f.enrolDevice(t, h, device)

	// The credential is signed for a page on another origin.
	device.Origin = "https://sprig.example"
	rec := f.signInWith(t, h, device)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d:\n%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(text(rec.Body.String()), "couldn’t be verified") {
		t.Errorf("the refusal does not say the passkey could not be checked:\n%s", rec.Body.String())
	}
	if strings.Contains(log.String(), `"level":"ERROR"`) {
		t.Errorf("a client's bad answer was logged as a server error:\n%s", log.String())
	}
	if !strings.Contains(log.String(), `"level":"WARN"`) {
		t.Errorf("the refusal left no line in the log:\n%s", log.String())
	}
}

func TestPasskeys_AnAnswerThatDoesNotCheckOutSaysSoOnThePage(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	device.Origin = "https://sprig.example"

	rec := f.registerDevice(t, h, device)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if !strings.Contains(text(rec.Body.String()), "couldn’t be verified") {
		t.Errorf("the page does not say the passkey could not be checked:\n%s", text(rec.Body.String()))
	}
}

func TestPasskeys_ADeviceRegisteredASecondTimeIsRefusedOnThePage(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)

	// The software device ignores the exclude list, as a hand-made client
	// would, and returns the credential id it already registered.
	rec := f.registerDevice(t, h, device)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if !strings.Contains(text(rec.Body.String()), "already has a passkey") {
		t.Errorf("the page does not say the device already has a passkey:\n%s", text(rec.Body.String()))
	}
}

func TestSignIn_SigningInAgainEndsTheSessionTheBrowserAlreadyHad(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)
	now := f.handler.now()
	old, _, err := h.sessions.Create(t.Context(), now, moreUserID, &moreGardenID, nil, "")
	if err != nil {
		t.Fatalf("creating the first session: %v", err)
	}

	var assertion protocol.CredentialAssertion
	cookie := f.challengeJSON(t, h.signInChallenge, challengePath, &assertion)
	answer := url.Values{credentialField: {device.Assert(&assertion)}}
	rec := f.post(t, h.signIn, signInPath, answer, cookie, &http.Cookie{Name: "__Host-sprig_session", Value: old})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	fresh := cookieNamed(t, rec, "__Host-sprig_session")
	if fresh == nil || fresh.Value == old {
		t.Fatalf("the sign-in set %+v, want a new session cookie", fresh)
	}
	if _, _, err := h.sessions.Lookup(t.Context(), now, old); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("the first session still resolves after a second sign-in: %v, want %v", err, auth.ErrNoSession)
	}
}

func TestSignIn_TheSameAnswerCannotStartASecondSession(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)

	var assertion protocol.CredentialAssertion
	cookie := f.challengeJSON(t, h.signInChallenge, challengePath, &assertion)
	answer := url.Values{credentialField: {device.Assert(&assertion)}}
	if rec := f.post(t, h.signIn, signInPath, answer, cookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first answer: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	rec := f.post(t, h.signIn, signInPath, answer, cookie)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the same answer again: status = %d, want %d:\n%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("the replayed answer set a session cookie")
	}
}
