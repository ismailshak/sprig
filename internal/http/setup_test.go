package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/auth/passkeytest"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

// createLabel is the label on the Set up your garden submit button.
const createLabel = "Create the garden"

// setupFixture is the Set up your garden handler over an empty database,
// inside a transaction rolled back when the test ends. Nothing is seeded,
// because the page is used before any account exists.
type setupFixture struct {
	handler *setup
	tx      pgx.Tx
	queries *store.Queries
	cookie  auth.CookieSettings
}

// setupOn returns the fixture with sign-up on or off.
func setupOn(t *testing.T, enabled bool) *setupFixture {
	t.Helper()

	tx := pgtest.Tx(t, migrateSchema)
	queries := store.New(tx)
	cookie := auth.CookieSettings{Name: "__Host-sprig_session", Secure: true}
	passkeys, err := auth.NewPasskeys(queries, "localhost", "sprig", "http://localhost:8080", cookie)
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	sessions := auth.NewSessions(queries, testTTL, cookie)
	return &setupFixture{
		handler: &setup{
			logger:    testLogger,
			passkeys:  passkeys,
			sessions:  sessions,
			resolver:  auth.NewResolver(sessions, queries),
			queries:   queries,
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
			enabled:   enabled,
		},
		tx:      tx,
		queries: queries,
		cookie:  cookie,
	}
}

func aGardenForm() url.Values {
	return url.Values{"garden": {"Greenhouse"}, "name": {"Robin"}, "timezone": {"Europe/London"}}
}

// request calls handler with no principal on it, because the routes are
// public. A nil form makes it a GET. The User-Agent is Chrome on a Mac, so a
// passkey saved by the request is named Mac · Chrome.
func (f *setupFixture) request(t *testing.T, handler http.HandlerFunc, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
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

// challenge posts form for a registration challenge and returns the options
// and the ceremony cookie. It fails the test on any status but 200.
func (f *setupFixture) challenge(t *testing.T, form url.Values) (*protocol.CredentialCreation, *http.Cookie) {
	t.Helper()

	rec := f.request(t, f.handler.challenge, setupChallengePath, form)
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

// create runs the whole flow with device answering: the post for the
// challenge, then the post with the device's answer in the credential field.
func (f *setupFixture) create(t *testing.T, form url.Values, device *passkeytest.Authenticator) *httptest.ResponseRecorder {
	t.Helper()

	creation, cookie := f.challenge(t, form)
	answered := url.Values{}
	for k, v := range form {
		answered[k] = v
	}
	answered.Set(credentialField, device.Register(creation))
	return f.request(t, f.handler.create, setupPath, answered, cookie)
}

// mustCreate is create for a post the test expects to go through. It returns
// the session token the response set.
func (f *setupFixture) mustCreate(t *testing.T, form url.Values, device *passkeytest.Authenticator) string {
	t.Helper()

	rec := f.create(t, form, device)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("creating the garden: status = %d, Location = %q, want %d to /:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, text(rec.Body.String()))
	}
	session := cookieNamed(t, rec, "__Host-sprig_session")
	if session == nil {
		t.Fatal("creating the garden set no session cookie")
	}
	return session.Value
}

// principalOf resolves token the way the middleware would.
func (f *setupFixture) principalOf(t *testing.T, token string) auth.Principal {
	t.Helper()

	principal, err := auth.NewResolver(f.handler.sessions, f.queries).Resolve(t.Context(), thursday, token)
	if err != nil {
		t.Fatalf("resolving the session: %v", err)
	}
	return principal
}

func (f *setupFixture) count(t *testing.T, table string) int {
	t.Helper()

	var n int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// nothingWritten fails the test if any of the rows a setup creates exists.
func (f *setupFixture) nothingWritten(t *testing.T) {
	t.Helper()

	for _, table := range []string{"app_user", "garden", "membership", "care_type", "passkey_credential", "session"} {
		if n := f.count(t, table); n != 0 {
			t.Errorf("%s has %d rows, want none", table, n)
		}
	}
}

func TestSetup_WithSignUpOffAnEmptyInstallServesTheFormWithACreateButtonThatStartsDisabled(t *testing.T) {
	f := setupOn(t, false)

	rec := f.request(t, f.handler.show, setupPath, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"Set up your garden",
		`action="` + setupPath + `"`,
		`data-passkey="create"`,
		`data-challenge="` + setupChallengePath + `"`,
		`id="garden" name="garden"`,
		`id="name" name="name"`,
		`name="timezone" data-propose`,
		`<input type="hidden" name="` + credentialField + `">`,
		"Setting up needs JavaScript and a browser that supports passkeys.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	if !buttonIsDisabled(t, page, createLabel) {
		t.Errorf("%s is not disabled:\n%s", createLabel, page)
	}
	// The select opens on the placeholder, so a form the script did not fill
	// in posts an empty zone and gets the message under the field.
	if !strings.Contains(page, `<option value="" selected>Timezone</option>`) {
		t.Errorf("the select does not open on the empty option labelled Timezone:\n%s", page)
	}
	if strings.Contains(page, "nav__item") {
		t.Error("the page renders the tab bar, and there is no garden to tab to")
	}
}

func TestSetup_WithSignUpOffEveryRouteIs404OnceAnAccountExists(t *testing.T) {
	f := setupOn(t, false)
	if _, err := f.queries.CreateAccount(t.Context(), uuid.NewV7(), "Ellie", "Europe/London"); err != nil {
		t.Fatalf("seeding an account: %v", err)
	}

	for _, route := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
		form    url.Values
	}{
		{"the page", f.handler.show, setupPath, nil},
		{"the challenge", f.handler.challenge, setupChallengePath, aGardenForm()},
		{"the post", f.handler.create, setupPath, aGardenForm()},
	} {
		rec := f.request(t, route.handler, route.path, route.form)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", route.name, rec.Code, http.StatusNotFound)
		}
	}
}

func TestSetup_CreatingTheGardenWritesEveryRowAndSignsInAsItsOwner(t *testing.T) {
	f := setupOn(t, false)
	device := aDevice()

	rec := f.create(t, aGardenForm(), device)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("status = %d, Location = %q, want %d to /:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, text(rec.Body.String()))
	}
	if cookie := cookieNamed(t, rec, "__Host-sprig_ceremony"); cookie == nil || cookie.MaxAge != -1 {
		t.Errorf("the post left the ceremony cookie in place: %+v", cookie)
	}
	session := cookieNamed(t, rec, "__Host-sprig_session")
	if session == nil {
		t.Fatal("the post set no session cookie")
	}

	principal := f.principalOf(t, session.Value)
	if principal.User.DisplayName != "Robin" || principal.User.Handle != "robin" || principal.User.Timezone != "Europe/London" {
		t.Errorf("the account is %+v, want Robin, robin, Europe/London", principal.User)
	}
	if principal.Garden.Name != "Greenhouse" {
		t.Errorf("the session is on %q, want Greenhouse", principal.Garden.Name)
	}
	if principal.Membership.Role != "owner" || principal.Membership.DigestHour != digestHourDefault {
		t.Errorf("the membership is %+v, want an owner with the digest at %d", principal.Membership, digestHourDefault)
	}
	if !principal.Can(auth.MemberInvite) {
		t.Error("the owner cannot invite people to the garden they just made")
	}

	var kinds []string
	rows, err := f.tx.Query(t.Context(), "SELECT kind FROM notification_preference WHERE membership_id = $1 AND enabled ORDER BY kind", principal.Membership.ID)
	if err != nil {
		t.Fatalf("reading the preferences: %v", err)
	}
	if kinds, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatalf("reading the preferences: %v", err)
	}
	if strings.Join(kinds, ",") != digestKind {
		t.Errorf("the preferences turned on are %v, want the digest alone", kinds)
	}

	types, err := f.queries.ListCareTypes(t.Context(), principal.Garden.ID)
	if err != nil {
		t.Fatalf("listing the care types: %v", err)
	}
	var got []string
	for _, ct := range types {
		got = append(got, ct.Name+"/"+ct.Slug)
	}
	if strings.Join(got, " ") != "Water/water Feed/feed Repot/repot" {
		t.Errorf("the care types are %v, want Water, Feed and Repot in that order", got)
	}

	keys, err := f.queries.ListPasskeys(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatalf("listing the passkeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "Mac · Chrome" || keys[0].CredentialID != device.CredentialID() {
		t.Errorf("the passkeys are %+v, want the device alone, named after the browser", keys)
	}
	var passkeyID *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT passkey_credential_id FROM session WHERE user_id = $1", principal.User.ID).Scan(&passkeyID); err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if passkeyID == nil || *passkeyID != keys[0].ID {
		t.Errorf("the session names passkey %v, want %s, so removing the device ends it", passkeyID, keys[0].ID)
	}
}

func TestSetup_WithSignUpOffTheSecondRegistrationIsRefused(t *testing.T) {
	f := setupOn(t, false)
	f.mustCreate(t, aGardenForm(), aDevice())

	if rec := f.request(t, f.handler.show, setupPath, nil); rec.Code != http.StatusNotFound {
		t.Errorf("the page after the first registration: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	second := url.Values{"garden": {"Allotment"}, "name": {"Sam"}, "timezone": {"Europe/London"}}
	if rec := f.request(t, f.handler.challenge, setupChallengePath, second); rec.Code != http.StatusNotFound {
		t.Errorf("the challenge after the first registration: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if n := f.count(t, "garden"); n != 1 {
		t.Errorf("%d gardens, want 1", n)
	}
}

func TestSetup_WithSignUpOffTheSecondOfTwoChallengesIssuedOnAnEmptyInstallIs404WhenAnswered(t *testing.T) {
	f := setupOn(t, false)
	// Both challenges are issued while the install is empty, as two browsers
	// on the page at once would get them.
	first, firstCookie := f.challenge(t, aGardenForm())
	second := url.Values{"garden": {"Allotment"}, "name": {"Sam"}, "timezone": {"Europe/London"}}
	secondCreation, secondCookie := f.challenge(t, second)

	answered := aGardenForm()
	answered.Set(credentialField, aDevice().Register(first))
	if rec := f.request(t, f.handler.create, setupPath, answered, firstCookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first post: status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	second.Set(credentialField, aDevice().Register(secondCreation))
	rec := f.request(t, f.handler.create, setupPath, second, secondCookie)

	if rec.Code != http.StatusNotFound {
		t.Errorf("the second post: status = %d, want %d:\n%s", rec.Code, http.StatusNotFound, text(rec.Body.String()))
	}
	if n := f.count(t, "garden"); n != 1 {
		t.Errorf("%d gardens, want 1", n)
	}
}

func TestSetup_WithSignUpOnASecondPersonGetsAGardenOfTheirOwnWithItsOwnCareTypes(t *testing.T) {
	f := setupOn(t, true)
	robin := f.mustCreate(t, aGardenForm(), aDevice())

	sam := f.mustCreate(t, url.Values{"garden": {"Allotment"}, "name": {"Sam"}, "timezone": {"Asia/Tokyo"}}, aDevice())

	robinsGarden := f.principalOf(t, robin).Garden
	samsGarden := f.principalOf(t, sam).Garden
	if robinsGarden.Name != "Greenhouse" || samsGarden.Name != "Allotment" || robinsGarden.ID == samsGarden.ID {
		t.Errorf("the sessions are on %q and %q, want Greenhouse and Allotment, two gardens", robinsGarden.Name, samsGarden.Name)
	}
	for _, garden := range []store.Garden{robinsGarden, samsGarden} {
		types, err := f.queries.ListCareTypes(t.Context(), garden.ID)
		if err != nil {
			t.Fatalf("listing %s's care types: %v", garden.Name, err)
		}
		if len(types) != 3 {
			t.Errorf("%s has %d care types, want 3 of its own", garden.Name, len(types))
		}
	}
	if n := f.count(t, "membership"); n != 2 {
		t.Errorf("%d memberships, want one per person", n)
	}
}

func TestSetup_ADisplayNameAnotherAccountHoldsGetsAHandleWithASuffix(t *testing.T) {
	f := setupOn(t, true)
	if _, err := f.queries.CreateAccount(t.Context(), uuid.NewV7(), "Robin", "Europe/London"); err != nil {
		t.Fatalf("seeding an account: %v", err)
	}

	token := f.mustCreate(t, aGardenForm(), aDevice())

	handle := f.principalOf(t, token).User.Handle
	if !regexp.MustCompile(`^robin_[a-z2-7]{4}$`).MatchString(handle) {
		t.Errorf("the second Robin's handle is %q, want robin and a random suffix", handle)
	}
}

func TestSetup_AnEmptyFieldIsRefusedOnTheChallengeAndTheMessageIsUnderItOnThePost(t *testing.T) {
	// The two text fields are posted as spaces, because a name of spaces is
	// no name. The select posts the placeholder's empty value.
	cases := []struct {
		field   string
		posted  string
		message string
	}{
		{"garden", "  ", "Give the garden a name."},
		{"name", "  ", "Give yourself a name to sign your care with."},
		{"timezone", "", "Pick a timezone."},
	}
	for _, c := range cases {
		t.Run(c.field+" empty", func(t *testing.T) {
			f := setupOn(t, false)
			form := aGardenForm()
			form.Set(c.field, c.posted)

			if rec := f.request(t, f.handler.challenge, setupChallengePath, form); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("the challenge: status = %d, want %d, so no passkey is made for a form the post refuses", rec.Code, http.StatusUnprocessableEntity)
			}
			if n := f.count(t, "webauthn_ceremony"); n != 0 {
				t.Errorf("the challenge wrote %d ceremony rows, want none", n)
			}

			rec := f.request(t, f.handler.create, setupPath, form)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("the post: status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
			}
			page := rec.Body.String()
			if got := errorUnder(page, c.field); got != c.message {
				t.Errorf("under %s: %q, want %q", c.field, got, c.message)
			}
			for _, other := range cases {
				if other.field != c.field && errorUnder(page, other.field) != "" {
					t.Errorf("a message under %s, and that field was filled in", other.field)
				}
			}
			f.nothingWritten(t)
		})
	}
}

func TestSetup_TheRefusedFormComesBackWithWhatWasTypedAndTheZoneChosen(t *testing.T) {
	f := setupOn(t, false)
	form := url.Values{"garden": {""}, "name": {"Robin"}, "timezone": {"Asia/Tokyo"}}

	rec := f.request(t, f.handler.create, setupPath, form)

	page := rec.Body.String()
	if got := valueOf(t, page, "name"); got != "Robin" {
		t.Errorf("Display name came back as %q, want Robin", got)
	}
	if zone := selectedZone(t, page); zone.value != "Asia/Tokyo" {
		t.Errorf("the select came back on %q, want Asia/Tokyo", zone.value)
	}
	// The zone chosen must not be replaced by the browser's when the page is
	// rendered again.
	if strings.Contains(page, "data-propose") {
		t.Error("the refused form is marked for the browser's zone to be proposed, so the zone chosen would be replaced")
	}
}

func TestSetup_AZoneTheSelectDidNotOfferIsRefusedWithoutAPage(t *testing.T) {
	f := setupOn(t, false)
	form := aGardenForm()
	form.Set("timezone", "Mars/Olympus_Mons")

	if rec := f.request(t, f.handler.challenge, setupChallengePath, form); rec.Code != http.StatusBadRequest {
		t.Errorf("the challenge: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if rec := f.request(t, f.handler.create, setupPath, form); rec.Code != http.StatusBadRequest {
		t.Errorf("the post: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	f.nothingWritten(t)
}

func TestSetup_AnAnswerWithNoChallengeBehindItSaysTheRequestExpiredAndWritesNothing(t *testing.T) {
	f := setupOn(t, false)
	form := aGardenForm()
	form.Set(credentialField, "{}")

	rec := f.request(t, f.handler.create, setupPath, form, &http.Cookie{Name: "__Host-sprig_ceremony", Value: "gone"})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "That took too long, so the request has expired. Press Create the garden again.") {
		t.Errorf("the page does not say the request expired:\n%s", text(rec.Body.String()))
	}
	if got := valueOf(t, rec.Body.String(), "garden"); got != "Greenhouse" {
		t.Errorf("Garden name came back as %q, want Greenhouse", got)
	}
	buttonNamed(t, rec.Body.String(), createLabel)
	f.nothingWritten(t)
}

func TestSetup_ADeviceThatDidNotCheckItWasYouLeavesNoGardenBehind(t *testing.T) {
	f := setupOn(t, false)
	device := aDevice()
	device.Verified = false

	rec := f.create(t, aGardenForm(), device)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "This device did not check that it was you.") {
		t.Errorf("the page does not say the device did not verify:\n%s", text(rec.Body.String()))
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("a refused registration set a session cookie")
	}
	// The rows were written before the answer was checked, in the same
	// transaction, so the refusal rolled them back.
	f.nothingWritten(t)
	// The install is still empty, so the page is still served.
	if rec := f.request(t, f.handler.show, setupPath, nil); rec.Code != http.StatusOK {
		t.Errorf("the page after a refused registration: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSetup_AChallengeFromThePasskeysPageDoesNotCreateAGarden(t *testing.T) {
	f := setupOn(t, true)
	ellie, err := f.queries.CreateAccount(t.Context(), uuid.NewV7(), "Ellie", "Europe/London")
	if err != nil {
		t.Fatalf("seeding an account: %v", err)
	}
	// A challenge the Passkeys page issued for Ellie, answered at /setup with
	// Robin's fields. The ceremony cookie outlives a sign-out, so the browser
	// can hold one.
	creation, cookie, err := f.handler.passkeys.BeginRegistration(t.Context(), thursday, ellie, nil)
	if err != nil {
		t.Fatalf("starting a registration: %v", err)
	}
	form := aGardenForm()
	form.Set(credentialField, aDevice().Register(creation))

	rec := f.request(t, f.handler.create, setupPath, form, cookie)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "the request has expired") {
		t.Errorf("the page does not say the request expired:\n%s", text(rec.Body.String()))
	}
	if n := f.count(t, "garden"); n != 0 {
		t.Errorf("%d gardens, want none", n)
	}
	if n := f.count(t, "passkey_credential"); n != 0 {
		t.Errorf("%d passkeys, want none: Ellie's challenge enrolled a device from the setup page", n)
	}
}

func TestSetup_ABrowserAlreadySignedInHasThatSessionEnded(t *testing.T) {
	f := setupOn(t, true)
	first := f.mustCreate(t, aGardenForm(), aDevice())

	creation, cookie := f.challenge(t, url.Values{"garden": {"Allotment"}, "name": {"Sam"}, "timezone": {"Europe/London"}})
	form := url.Values{"garden": {"Allotment"}, "name": {"Sam"}, "timezone": {"Europe/London"}, credentialField: {aDevice().Register(creation)}}
	rec := f.request(t, f.handler.create, setupPath, form, cookie, &http.Cookie{Name: "__Host-sprig_session", Value: first})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, text(rec.Body.String()))
	}
	if _, err := f.handler.sessions.Lookup(t.Context(), thursday, first); err == nil {
		t.Error("the session the browser arrived with still resolves")
	}
}

func TestSetup_APersonWhoSignsUpAndIsThenInvitedElsewhereSignsInToTheirOwnGarden(t *testing.T) {
	f := setupOn(t, true)
	device := aDevice()
	token := f.mustCreate(t, aGardenForm(), device)
	robin := f.principalOf(t, token).User
	// Another garden invites Robin as a sitter, some time after their own
	// garden was made.
	var fairviewID uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "INSERT INTO garden (name) VALUES ('Fairview') RETURNING id").Scan(&fairviewID); err != nil {
		t.Fatalf("seeding Fairview: %v", err)
	}
	if _, err := createMembership(t.Context(), f.queries, newMembership{GardenID: fairviewID, UserID: robin.ID, Role: "sitter"}); err != nil {
		t.Fatalf("seeding the second membership: %v", err)
	}

	h := &passkeyCeremony{
		logger:    testLogger,
		passkeys:  f.handler.passkeys,
		sessions:  f.handler.sessions,
		queries:   f.queries,
		templates: testTemplates(),
		now:       f.handler.now,
	}
	rec := f.request(t, h.signInChallenge, challengePath, url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("the sign-in challenge: status = %d:\n%s", rec.Code, rec.Body.String())
	}
	var assertion protocol.CredentialAssertion
	if err := json.Unmarshal(rec.Body.Bytes(), &assertion); err != nil {
		t.Fatalf("the challenge is not JSON: %v", err)
	}
	rec = f.request(t, h.signIn, signInPath, url.Values{credentialField: {device.Assert(&assertion)}}, cookieNamed(t, rec, "__Host-sprig_ceremony"))

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("signing in: status = %d, Location = %q, want %d to /:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, text(rec.Body.String()))
	}
	principal := f.principalOf(t, cookieNamed(t, rec, "__Host-sprig_session").Value)
	if principal.Garden.Name != "Greenhouse" || principal.Membership.Role != "owner" {
		t.Errorf("the sign-in landed on %q as %s, want Greenhouse as owner, the older membership", principal.Garden.Name, principal.Membership.Role)
	}
}
