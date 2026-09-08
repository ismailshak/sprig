package http

import (
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

// signInLabel is the label on the sign-in page's submit button.
const signInLabel = "Sign in with a passkey"

func TestSignIn_ThePageIsServedWithNoSessionAndPostsToTheSignInRoutes(t *testing.T) {
	f := moreGarden(t)
	handler := signInMux(t, f)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, signInPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"Sign in to sprig",
		`action="` + signInPath + `"`,
		`data-passkey="get"`,
		`data-challenge="` + challengePath + `"`,
		`<input type="hidden" name="` + credentialField + `">`,
		"Requires JavaScript and a browser with passkey support.",
		`href="` + recoverPath + `"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	// Only the browser can talk to the device, so a press with no script
	// running would post an empty credential.
	if !buttonIsDisabled(t, page, signInLabel) {
		t.Errorf("%s is not disabled:\n%s", signInLabel, page)
	}
	if strings.Contains(page, "nav__item") {
		t.Error("the sign-in page renders the tab bar, and nobody is signed in to use it")
	}
}

func TestSignIn_AnAnswerWithNoChallengeBehindItSaysTheRequestExpiredOnThePage(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)
	if rec := f.signInWith(t, h, device); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first sign-in: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	// A ceremony cookie that matches no row is what a browser sends once the
	// challenge has already been used.
	rec := f.post(t, h.signIn, signInPath, url.Values{credentialField: {"{}"}}, &http.Cookie{Name: "__Host-sprig_ceremony", Value: "gone"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnauthorized, text(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "Sign-in timed out. Try again.") {
		t.Errorf("the page does not say the request expired:\n%s", text(rec.Body.String()))
	}
	// The form is still on the page, so a second attempt takes no navigation.
	buttonNamed(t, rec.Body.String(), signInLabel)
}

func TestSignIn_APasskeyRemovedFromTheAccountIsRefusedOnThePage(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)
	// The device keeps the credential after the row is deleted, so it signs in
	// with a passkey the server no longer knows.
	f.exec(t, "DELETE FROM passkey_credential WHERE credential_id = $1", device.CredentialID())

	rec := f.signInWith(t, h, device)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnauthorized, text(rec.Body.String()))
	}
	// The comparison is against the rendered text, because the apostrophe in
	// the sentence is escaped in the markup.
	if !strings.Contains(text(rec.Body.String()), "This passkey isn’t registered. It may have been removed on the Passkeys page.") {
		t.Errorf("the page does not say the passkey is unknown:\n%s", text(rec.Body.String()))
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("a sign-in with a removed passkey set a session cookie")
	}
}

func TestSignIn_AnAccountInNoGardenGetsASessionOnNoGarden(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)
	f.exec(t, "DELETE FROM membership WHERE user_id = $1", moreUserID)

	rec := f.signInWith(t, h, device)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	session := cookieNamed(t, rec, "__Host-sprig_session")
	if session == nil {
		t.Fatal("the sign-in set no session cookie")
	}
	resolver := auth.NewResolver(h.sessions, store.New(f.tx))
	principal, err := resolver.Resolve(t.Context(), thursday, session.Value)
	if err != nil {
		t.Fatalf("resolving the session: %v", err)
	}
	if principal.InGarden() || principal.User.ID != moreUserID {
		t.Errorf("the session is %s on %q, want Ellie in no garden", principal.User.DisplayName, principal.Garden.Name)
	}
}

func TestSignIn_AnAnswerPastTheBudgetIsThePageWithTheRateLimitLine(t *testing.T) {
	f := moreGarden(t)
	handler := signInMux(t, f)
	answerFrom := func(address string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, signInPath, strings.NewReader(url.Values{credentialField: {"{}"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = address + ":40000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for i := range 6 {
		if rec := answerFrom("203.0.113.1"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("answer %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusUnauthorized, text(rec.Body.String()))
		}
	}

	rec := answerFrom("203.0.113.1")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the seventh answer in a minute: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("the refusal sets no Retry-After")
	}
	if !strings.Contains(text(rec.Body.String()), tooManySignIns) {
		t.Errorf("the refusal does not carry the rate-limit line:\n%s", text(rec.Body.String()))
	}
	// The response is the page rather than the plain status text, so the line
	// is read above the button it is about.
	buttonNamed(t, rec.Body.String(), signInLabel)
}

// signInWithNext signs in with device and posts next in the form's next field.
func (f *moreFixture) signInWithNext(t *testing.T, h *passkeyCeremony, device *passkeytest.Authenticator, next string) *httptest.ResponseRecorder {
	t.Helper()

	var assertion protocol.CredentialAssertion
	cookie := f.challengeJSON(t, h.signInChallenge, challengePath, &assertion)
	return f.post(t, h.signIn, signInPath, url.Values{credentialField: {device.Assert(&assertion)}, nextField: {next}}, cookie)
}

func TestSignIn_ASignInWithANextPathLandsOnThatPath(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	device := aDevice()
	f.enrolDevice(t, h, device)
	next := acceptPath("join-as-a-sitter")

	rec := httptest.NewRecorder()
	h.showSignIn(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, signInPath+"?"+nextField+"="+url.QueryEscape(next), nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<input type="hidden" name="`+nextField+`" value="`+next+`">`) {
		t.Errorf("the form has no hidden input for %s:\n%s", next, rec.Body.String())
	}

	rec = f.signInWithNext(t, h, device, next)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != next {
		t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, next)
	}
	if cookieNamed(t, rec, "__Host-sprig_session") == nil {
		t.Error("the sign-in set no session cookie")
	}
}

func TestSignIn_ANextPathOnAnotherSiteIsRefusedAndTheSignInLandsOnToday(t *testing.T) {
	// A browser strips a tab or a newline from a URL before parsing it, so
	// "/\t/example.com/" would arrive as "//example.com/".
	for _, next := range []string{"https://example.com/", "//example.com/", "/\\example.com/", "example.com", "", "/\t/example.com/", "/\n//example.com"} {
		t.Run(next, func(t *testing.T) {
			f := moreGarden(t)
			h := ceremonyOn(t, f)
			device := aDevice()
			f.enrolDevice(t, h, device)

			rec := httptest.NewRecorder()
			h.showSignIn(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, signInPath+"?"+nextField+"="+url.QueryEscape(next), nil))
			if strings.Contains(rec.Body.String(), `name="`+nextField+`"`) {
				t.Errorf("the form has a hidden input for %q:\n%s", next, rec.Body.String())
			}

			rec = f.signInWithNext(t, h, device, next)

			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
				t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath)
			}
		})
	}
}

func TestSignIn_ARefusedSignInKeepsTheNextPathOnTheForm(t *testing.T) {
	f := moreGarden(t)
	h := ceremonyOn(t, f)
	next := acceptPath("join-as-a-sitter")

	rec := f.post(t, h.signIn, signInPath, url.Values{credentialField: {"{}"}, nextField: {next}}, &http.Cookie{Name: "__Host-sprig_ceremony", Value: "gone"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnauthorized, text(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), `<input type="hidden" name="`+nextField+`" value="`+next+`">`) {
		t.Errorf("the form lost %s:\n%s", next, rec.Body.String())
	}
}
