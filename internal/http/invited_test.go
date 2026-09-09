package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/auth/passkeytest"
	"github.com/ismailshak/sprig/internal/store"
)

// The tokens of the invites the fixture writes to Rosewood. Each is the
// plaintext of a link, and the row holds its hash.
const (
	// sitterLink is a join invite for a sitter, with a day their access ends.
	sitterLink = "join-as-a-sitter"
	// memberLink is a join invite for a member, with no end date.
	memberLink = "join-as-a-member"
	// ranOutLink expired yesterday.
	ranOutLink = "ran-out"
	// usedLink was redeemed yesterday.
	usedLink = "already-used"
	// endedBeforeJoiningLink is a join invite whose membership ended yesterday,
	// so joining would create a membership that grants nothing.
	endedBeforeJoiningLink = "ended-before-joining"
	// samsLink adds a device to Sam's account. Sam is a member of Rosewood.
	samsLink = "add-a-device-for-sam"
	// claresLink adds a device to Clare's account. Clare's access to Rosewood
	// ended twelve days ago.
	claresLink = "add-a-device-for-clare"
	// expiringNowLink expires at the instant the handler calls now.
	expiringNowLink = "expiring-this-instant"
	noSuchLink      = "never-issued"
)

var (
	invitedSitterID = uuid.MustParse("00000000-0000-7000-8000-000000000350")
	invitedMemberID = uuid.MustParse("00000000-0000-7000-8000-000000000351")
)

// sitterAccessEnds is when the sitter link's membership runs out. Half an
// hour before midnight UTC is already the next day in Europe/London, where the
// inviter is, so the day the page names says which zone it read the date in.
var sitterAccessEnds = time.Date(2026, time.September, 17, 23, 30, 0, 0, time.UTC)

// invitedFixture is the invite handler over Rosewood as the People tests seed
// it, with one invite row for each of the tokens above.
type invitedFixture struct {
	*moreFixture
	handler *invited
	queries *store.Queries
	cookie  auth.CookieSettings
}

func invitedGarden(t *testing.T) *invitedFixture {
	t.Helper()

	f := peopleGarden(t)
	f.exec(t, `INSERT INTO invite (id, garden_id, token_hash, role, user_id, created_by, expires_at, redeemed_at, membership_expires_at) VALUES
		($1, $2, $3, 'sitter', NULL, $4, $5, NULL, $6),
		($7, $2, $8, 'member', NULL, $4, $5, NULL, NULL),
		(uuidv7(), $2, $9, 'sitter', NULL, $4, $10, NULL, NULL),
		(uuidv7(), $2, $11, 'sitter', NULL, $4, $5, $10, NULL),
		(uuidv7(), $2, $12, 'sitter', NULL, $4, $5, NULL, $10),
		(uuidv7(), $2, $13, 'member', $14, $4, $5, NULL, NULL),
		(uuidv7(), $2, $15, 'sitter', $16, $4, $5, NULL, NULL),
		(uuidv7(), $2, $17, 'sitter', NULL, $4, $18, NULL, NULL)`,
		invitedSitterID, moreGardenID, auth.HashToken(sitterLink), moreUserID, thursday.AddDate(0, 0, 5), sitterAccessEnds,
		invitedMemberID, auth.HashToken(memberLink),
		auth.HashToken(ranOutLink), thursday.AddDate(0, 0, -1),
		auth.HashToken(usedLink),
		auth.HashToken(endedBeforeJoiningLink),
		auth.HashToken(samsLink), otherUserID,
		auth.HashToken(claresLink), peopleClareID,
		auth.HashToken(expiringNowLink), thursday)

	queries := store.New(f.tx)
	cookie := auth.CookieSettings{Name: "__Host-sprig_session", Secure: true}
	passkeys, err := auth.NewPasskeys(queries, "localhost", "sprig", "http://localhost:8080", cookie)
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	sessions := auth.NewSessions(queries, testTTL, cookie)
	return &invitedFixture{
		moreFixture: f,
		handler: &invited{
			logger:    testLogger,
			passkeys:  passkeys,
			sessions:  sessions,
			resolver:  auth.NewResolver(sessions, queries),
			queries:   queries,
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
		queries: queries,
		cookie:  cookie,
	}
}

func aJoinForm() url.Values {
	return url.Values{"name": {"Robin"}, "timezone": {"Asia/Tokyo"}}
}

// request calls handler for token with no principal on the request, because
// the routes are public, and the token set as the path value the mux would
// have taken from the URL. A nil form makes it a GET. The User-Agent is
// Chrome on a Mac, so a passkey saved by the request is named Mac · Chrome.
func (f *invitedFixture) request(t *testing.T, handler http.HandlerFunc, token, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	method, body := http.MethodGet, strings.NewReader("")
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	req.SetPathValue("token", token)
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

func (f *invitedFixture) show(t *testing.T, token string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, f.handler.show, token, InvitedPath(token), nil)
}

// challenge posts form for a registration challenge on token and returns the
// options and the ceremony cookie. It fails the test on any status but 200.
func (f *invitedFixture) challenge(t *testing.T, token string, form url.Values) (*protocol.CredentialCreation, *http.Cookie) {
	t.Helper()

	rec := f.request(t, f.handler.challenge, token, invitedChallengePath(token), form)
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

// redeem runs the whole flow on token with device: the post for the challenge,
// then the post with the credential it made in the credential field. cookies go
// on the second post beside the ceremony cookie.
func (f *invitedFixture) redeem(t *testing.T, token string, form url.Values, device *passkeytest.Authenticator, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	creation, cookie := f.challenge(t, token, form)
	answered := url.Values{}
	for k, v := range form {
		answered[k] = v
	}
	answered.Set(credentialField, device.Register(creation))
	return f.request(t, f.handler.redeem, token, InvitedPath(token), answered, append(cookies, cookie)...)
}

// sessionOf fails the test unless rec redirected to next with a session
// cookie, and returns the principal that session resolves to.
func (f *invitedFixture) sessionOf(t *testing.T, rec *httptest.ResponseRecorder, next string) auth.Principal {
	t.Helper()

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != next {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, next, text(rec.Body.String()))
	}
	session := cookieNamed(t, rec, "__Host-sprig_session")
	if session == nil {
		t.Fatal("the post set no session cookie")
	}
	principal, err := auth.NewResolver(f.handler.sessions, f.queries).Resolve(t.Context(), thursday, session.Value)
	if err != nil {
		t.Fatalf("resolving the session: %v", err)
	}
	return principal
}

func (f *invitedFixture) count(t *testing.T, table string) int {
	t.Helper()
	return countRows(t, f.tx, table)
}

// redeemedAt returns when the invite for token was redeemed, or nil.
func (f *invitedFixture) redeemedAt(t *testing.T, token string) *time.Time {
	t.Helper()

	var at *time.Time
	if err := f.tx.QueryRow(t.Context(), "SELECT redeemed_at FROM invite WHERE token_hash = $1", auth.HashToken(token)).Scan(&at); err != nil {
		t.Fatalf("reading the invite: %v", err)
	}
	return at
}

// unusable fails the test unless rec is the page for a link that cannot be
// used, and returns the page.
func unusable(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	page := rec.Body.String()
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(page, "This invite link can’t be used") {
		t.Errorf("the page does not say the link cannot be used:\n%s", text(page))
	}
	if strings.Contains(page, "<form") {
		t.Errorf("the page has a form on it, and there is nothing to post:\n%s", text(page))
	}
	return page
}

func TestInvited_AJoinLinkRendersTheFormWithTheGardenTheInviterAndTheRoleAndNoTabBar(t *testing.T) {
	f := invitedGarden(t)

	rec := f.show(t, sitterLink)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"Ellie invited you to Rosewood",
		`action="` + InvitedPath(sitterLink) + `"`,
		`data-passkey="create"`,
		`data-challenge="` + invitedChallengePath(sitterLink) + `"`,
		`id="name" name="name"`,
		`name="timezone" data-propose`,
		`<option value="Europe/London" selected>`,
		`<input type="hidden" name="` + credentialField + `">`,
		"Requires JavaScript and a browser with passkey support.",
		"You’ll join as a sitter. Sitters can log care and view everything, but not add plants or photos. Your access ends on 18 Sep.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	if !buttonIsDisabled(t, page, "Join Rosewood") {
		t.Errorf("Join Rosewood is not disabled:\n%s", page)
	}
	if strings.Contains(page, "nav__item") {
		t.Error("the page renders the tab bar, and there is no garden to tab to")
	}
}

func TestInvited_AJoinLinkWithNoEndDateSaysNothingAboutAccessEnding(t *testing.T) {
	f := invitedGarden(t)

	page := text(f.show(t, memberLink).Body.String())

	if !strings.Contains(page, "You’ll join as a member. Members can log care, add and edit plants, and add photos.") {
		t.Errorf("the page does not say what a member can do:\n%s", page)
	}
	if strings.Contains(page, "access ends") {
		t.Errorf("the page says when access ends, and this invite set no end:\n%s", page)
	}
}

func TestInvited_AReenrolmentLinkAsksForNothingAndNamesTheAccountsGarden(t *testing.T) {
	f := invitedGarden(t)

	rec := f.show(t, samsLink)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"Add this device to your account",
		"Ellie sent this link so you can sign in to Rosewood from this device.",
		`data-passkey="create"`,
		"Requires JavaScript and a browser with passkey support.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	if strings.Contains(page, `name="name"`) || strings.Contains(page, `name="timezone"`) {
		t.Error("the page asks for a name or a timezone, and the account already has both")
	}
	if !buttonIsDisabled(t, page, addDeviceLabel) {
		t.Errorf("%s is not disabled:\n%s", addDeviceLabel, page)
	}
}

func TestInvited_AnExpiredAUsedAndANeverIssuedLinkGetOnePage(t *testing.T) {
	f := invitedGarden(t)

	pages := map[string]string{}
	for _, token := range []string{ranOutLink, usedLink, noSuchLink} {
		pages[token] = unusable(t, f.show(t, token))
	}

	for token, page := range pages {
		if page != pages[noSuchLink] {
			t.Errorf("the page for %s differs from the page for a link that was never issued, so the two can be told apart", token)
		}
	}
	if !strings.Contains(pages[noSuchLink], `<a href="`+signInPath+`">sign in</a>`) {
		t.Error("the page has no link to sign in, for somebody who already used theirs")
	}
}

func TestInvited_ARevokedLinkCannotBeUsed(t *testing.T) {
	f := invitedGarden(t)
	f.exec(t, "DELETE FROM invite WHERE id = $1", invitedSitterID)

	unusable(t, f.show(t, sitterLink))
}

func TestInvited_AJoinLinkWhoseAccessWouldAlreadyHaveEndedCannotBeUsed(t *testing.T) {
	f := invitedGarden(t)

	unusable(t, f.show(t, endedBeforeJoiningLink))

	rec := f.request(t, f.handler.challenge, endedBeforeJoiningLink, invitedChallengePath(endedBeforeJoiningLink), aJoinForm())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("the challenge: status = %d, want %d, so no passkey is made for a link the post refuses", rec.Code, http.StatusUnprocessableEntity)
	}
	unusable(t, f.request(t, f.handler.redeem, endedBeforeJoiningLink, InvitedPath(endedBeforeJoiningLink), aJoinForm()))
}

func TestInvited_ALinkExpiringAtThisInstantCannotBeUsed(t *testing.T) {
	f := invitedGarden(t)

	unusable(t, f.show(t, expiringNowLink))

	rec := f.request(t, f.handler.challenge, expiringNowLink, invitedChallengePath(expiringNowLink), aJoinForm())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("the challenge: status = %d, want %d, so no passkey is made for a link the post refuses", rec.Code, http.StatusUnprocessableEntity)
	}
	unusable(t, f.request(t, f.handler.redeem, expiringNowLink, InvitedPath(expiringNowLink), aJoinForm()))
}

func TestInvited_AReenrolmentLinkForSomebodyWhoseAccessEndedOrWasRemovedCannotBeUsed(t *testing.T) {
	f := invitedGarden(t)

	unusable(t, f.show(t, claresLink))

	f.exec(t, "DELETE FROM membership WHERE garden_id = $1 AND user_id = $2", moreGardenID, otherUserID)
	unusable(t, f.show(t, samsLink))
}

func TestInvited_AnEmptyFieldIsRefusedOnTheChallengeAndTheMessageIsUnderItOnThePost(t *testing.T) {
	cases := []struct {
		field   string
		posted  string
		message string
	}{
		{"name", "  ", "Enter a display name."},
		{"timezone", "", "Choose a timezone."},
	}
	for _, c := range cases {
		t.Run(c.field+" empty", func(t *testing.T) {
			f := invitedGarden(t)
			form := aJoinForm()
			form.Set(c.field, c.posted)

			if rec := f.request(t, f.handler.challenge, sitterLink, invitedChallengePath(sitterLink), form); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("the challenge: status = %d, want %d, so no passkey is made for a form the post refuses", rec.Code, http.StatusUnprocessableEntity)
			}
			if n := f.count(t, "webauthn_ceremony"); n != 0 {
				t.Errorf("the challenge wrote %d ceremony rows, want none", n)
			}

			rec := f.request(t, f.handler.redeem, sitterLink, InvitedPath(sitterLink), form)

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
			if strings.Contains(page, "data-propose") {
				t.Error("the select is marked for the script to change, and the zone posted is the person's own choice")
			}
			if at := f.redeemedAt(t, sitterLink); at != nil {
				t.Error("the refused post marked the invite used")
			}
		})
	}
}

func TestInvited_AZoneTheSelectDoesNotOfferIs400(t *testing.T) {
	f := invitedGarden(t)
	form := aJoinForm()
	form.Set("timezone", "Mars/Olympus_Mons")

	if rec := f.request(t, f.handler.challenge, sitterLink, invitedChallengePath(sitterLink), form); rec.Code != http.StatusBadRequest {
		t.Errorf("the challenge: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if rec := f.request(t, f.handler.redeem, sitterLink, InvitedPath(sitterLink), form); rec.Code != http.StatusBadRequest {
		t.Errorf("the post: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestInvited_JoiningWritesTheAccountTheMembershipAndThePasskeyMarksTheLinkUsedAndSignsIn(t *testing.T) {
	f := invitedGarden(t)
	device := aDevice()
	users, memberships := f.count(t, "app_user"), f.count(t, "membership")

	rec := f.redeem(t, sitterLink, aJoinForm(), device)

	if cookie := cookieNamed(t, rec, "__Host-sprig_ceremony"); cookie == nil || cookie.MaxAge != -1 {
		t.Errorf("the post left the ceremony cookie in place: %+v", cookie)
	}
	principal := f.sessionOf(t, rec, remindersPath)
	if principal.User.DisplayName != "Robin" || principal.User.Handle != "robin" || principal.User.Timezone != "Asia/Tokyo" {
		t.Errorf("the account is %+v, want Robin, robin, Asia/Tokyo", principal.User)
	}
	if principal.Garden.ID != moreGardenID {
		t.Errorf("the session is on %q, want Rosewood", principal.Garden.Name)
	}
	m := principal.Membership
	if m.Role != "sitter" || m.InvitedBy == nil || *m.InvitedBy != moreUserID || m.DigestHour != digestHourDefault {
		t.Errorf("the membership is %+v, want a sitter invited by Ellie with the digest at %d", m, digestHourDefault)
	}
	if m.ExpiresAt == nil || !m.ExpiresAt.Equal(sitterAccessEnds) {
		t.Errorf("the membership ends %v, want the day the invite set, %v", m.ExpiresAt, sitterAccessEnds)
	}
	if principal.Can(auth.PlantCreate) {
		t.Error("the sitter can add plants")
	}
	if !principal.Can(auth.CareLog) {
		t.Error("the sitter cannot log care")
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

	if at := f.redeemedAt(t, sitterLink); at == nil || !at.Equal(thursday) {
		t.Errorf("the invite was redeemed at %v, want %v", at, thursday)
	}
	if f.count(t, "app_user") != users+1 || f.count(t, "membership") != memberships+1 {
		t.Errorf("%d accounts and %d memberships, want one more of each than before", f.count(t, "app_user"), f.count(t, "membership"))
	}
	pending, err := f.queries.CountPendingInvites(t.Context(), moreGardenID, thursday)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture leaves four join invites pending, and the join redeems one
	// of them. Expired, redeemed and re-enrolment rows are not counted.
	if pending != 3 {
		t.Errorf("%d invites are pending on People after the join, want 3", pending)
	}
}

func TestInvited_TheSameLinkRedeemedTwiceCreatesOneAccount(t *testing.T) {
	f := invitedGarden(t)
	users := f.count(t, "app_user")
	f.sessionOf(t, f.redeem(t, sitterLink, aJoinForm(), aDevice()), remindersPath)

	unusable(t, f.show(t, sitterLink))
	second := url.Values{"name": {"Sam"}, "timezone": {"Europe/London"}}
	if rec := f.request(t, f.handler.challenge, sitterLink, invitedChallengePath(sitterLink), second); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("the second challenge: status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	unusable(t, f.request(t, f.handler.redeem, sitterLink, InvitedPath(sitterLink), second))

	if n := f.count(t, "app_user"); n != users+1 {
		t.Errorf("%d accounts, want one more than before", n)
	}
}

func TestInvited_TheSecondOfTwoChallengesOnOneLinkIsRefusedOnceTheFirstIsAnswered(t *testing.T) {
	f := invitedGarden(t)
	users := f.count(t, "app_user")
	// Both challenges are issued while the invite can still be redeemed,
	// before either is answered.
	first, firstCookie := f.challenge(t, sitterLink, aJoinForm())
	second := url.Values{"name": {"Sam"}, "timezone": {"Europe/London"}}
	secondCreation, secondCookie := f.challenge(t, sitterLink, second)

	answered := aJoinForm()
	answered.Set(credentialField, aDevice().Register(first))
	f.sessionOf(t, f.request(t, f.handler.redeem, sitterLink, InvitedPath(sitterLink), answered, firstCookie), remindersPath)
	second.Set(credentialField, aDevice().Register(secondCreation))
	rec := f.request(t, f.handler.redeem, sitterLink, InvitedPath(sitterLink), second, secondCookie)

	unusable(t, rec)
	if n := f.count(t, "app_user"); n != users+1 {
		t.Errorf("%d accounts, want one more than before", n)
	}
}

func TestInvited_ReenrollingAddsAPasskeyToTheAccountWritesNothingElseAndSignsInOnTheGarden(t *testing.T) {
	f := invitedGarden(t)
	device := aDevice()
	users, memberships := f.count(t, "app_user"), f.count(t, "membership")

	rec := f.redeem(t, samsLink, nil, device)

	principal := f.sessionOf(t, rec, "/")
	if principal.User.ID != otherUserID || principal.Garden.ID != moreGardenID {
		t.Errorf("the session is %s on %s, want Sam on Rosewood", principal.User.DisplayName, principal.Garden.Name)
	}
	if principal.Membership.Role != "member" {
		t.Errorf("the membership is a %s, want the member row Sam already had", principal.Membership.Role)
	}
	keys, err := f.queries.ListPasskeys(t.Context(), otherUserID)
	if err != nil {
		t.Fatalf("listing the passkeys: %v", err)
	}
	if len(keys) != 2 || keys[1].CredentialID != device.CredentialID() || keys[1].Name != "Mac · Chrome" {
		t.Errorf("Sam's passkeys are %+v, want the one seeded and the device, named after the browser", keys)
	}
	if f.count(t, "app_user") != users || f.count(t, "membership") != memberships {
		t.Error("re-enrolling wrote an account or a membership, and Sam already has both")
	}
	if at := f.redeemedAt(t, samsLink); at == nil {
		t.Error("the link was not marked used")
	}
	unusable(t, f.show(t, samsLink))
}

func TestInvited_ALinkFromSprigAdminInviteAddsAPasskeyToTheSameAccountAndChangesNoMembership(t *testing.T) {
	f := invitedGarden(t)
	device := aDevice()
	users, memberships := f.count(t, "app_user"), f.count(t, "membership")
	made, err := auth.IssueSignInLink(t.Context(), f.queries, thursday, "sam")
	if err != nil {
		t.Fatalf("issuing the link: %v", err)
	}

	page := f.show(t, made.Token).Body.String()
	if !strings.Contains(page, "This link signs you in to") || strings.Contains(page, "Sam sent this link") {
		t.Errorf("the page names a sender, and the operator made the link:\n%s", text(page))
	}
	rec := f.redeem(t, made.Token, nil, device)

	principal := f.sessionOf(t, rec, "/")
	if principal.User.ID != otherUserID || principal.Garden.ID != moreGardenID {
		t.Errorf("the session is %s on %s, want Sam on Rosewood", principal.User.DisplayName, principal.Garden.Name)
	}
	keys, err := f.queries.ListPasskeys(t.Context(), otherUserID)
	if err != nil {
		t.Fatalf("listing the passkeys: %v", err)
	}
	if len(keys) != 2 || keys[1].CredentialID != device.CredentialID() {
		t.Errorf("Sam's passkeys are %+v, want the one seeded and the device", keys)
	}
	if f.count(t, "app_user") != users || f.count(t, "membership") != memberships {
		t.Error("redeeming wrote an account or a membership, and Sam already has both")
	}
}

func TestInvited_ADeviceThatDidNotCheckWhoWasUsingItIsRefusedAndNothingIsWritten(t *testing.T) {
	f := invitedGarden(t)
	device := aDevice()
	device.Verified = false
	users := f.count(t, "app_user")

	rec := f.redeem(t, sitterLink, aJoinForm(), device)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	page := rec.Body.String()
	if !strings.Contains(page, "This device didn’t verify you.") {
		t.Errorf("the page does not say the device did not verify:\n%s", text(page))
	}
	if !strings.Contains(page, `value="Robin"`) || !strings.Contains(page, `<option value="Asia/Tokyo" selected>`) {
		t.Errorf("the form lost what was typed:\n%s", page)
	}
	if cookieNamed(t, rec, "__Host-sprig_session") != nil {
		t.Error("the refused post set a session cookie")
	}
	if n := f.count(t, "app_user"); n != users {
		t.Errorf("%d accounts, want none added", n)
	}
	if at := f.redeemedAt(t, sitterLink); at != nil {
		t.Error("the refused post marked the invite used")
	}
	if n := f.count(t, "passkey_credential"); n != 3 {
		t.Errorf("%d passkeys, want the 3 seeded", n)
	}
}

func TestInvited_ABrowserAlreadySignedInGetsANewSessionInPlaceOfItsOld(t *testing.T) {
	f := invitedGarden(t)
	old, _, err := f.handler.sessions.Create(t.Context(), thursday, moreUserID, &moreGardenID, nil, "")
	if err != nil {
		t.Fatalf("starting Ellie's session: %v", err)
	}

	rec := f.redeem(t, sitterLink, aJoinForm(), aDevice(), f.handler.sessions.Cookie(old))

	principal := f.sessionOf(t, rec, remindersPath)
	if principal.User.DisplayName != "Robin" {
		t.Errorf("the session is %s's, want Robin's", principal.User.DisplayName)
	}
	if _, err := auth.NewResolver(f.handler.sessions, f.queries).Resolve(t.Context(), thursday, old); err == nil {
		t.Error("the session the browser arrived with still resolves")
	}
}

// invitedMux is the whole handler over the fixture's transaction, so a
// request to an invite route goes through the rate limiters the route table
// wraps it in. The trusted header is empty, so an address is the request's
// RemoteAddr.
//
// New builds its handlers on time.Now, not the fixture's Thursday, so the
// sitter's invite gets an expiry and an access end counted from now.
func invitedMux(t *testing.T, f *invitedFixture) http.Handler {
	t.Helper()
	f.exec(t, `UPDATE invite SET expires_at = now() + interval '7 days', membership_expires_at = now() + interval '14 days' WHERE token_hash = $1`, auth.HashToken(sitterLink))
	return New(testLogger, f.handler.sessions, f.handler.passkeys, rejectEveryToken, noLiveToken, f.queries, testPhotos(t), testTemplates(), testAssets(), "", false, testPushKey, nil, nil, nil)
}

// postFrom posts form to path from address through handler.
func postFrom(t *testing.T, handler http.Handler, path, address string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = address + ":40000"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestInvited_TheSeventhPostInAMinuteFromOneAddressIsRefusedWithTheSentenceAboveTheForm(t *testing.T) {
	f := invitedGarden(t)
	handler := invitedMux(t, f)

	// A post with no ceremony cookie is refused as expired. Each one still
	// spends from the address's budget.
	for i := range 6 {
		if rec := postFrom(t, handler, InvitedPath(sitterLink), "203.0.113.1", aJoinForm()); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("post %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
		}
	}
	rec := postFrom(t, handler, InvitedPath(sitterLink), "203.0.113.1", aJoinForm())

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the seventh post: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	page := rec.Body.String()
	if !strings.Contains(text(page), tooManyInviteAttempts) {
		t.Errorf("the page does not show the rate-limit sentence:\n%s", text(page))
	}
	if !strings.Contains(page, "Ellie invited you to Rosewood") || !strings.Contains(page, "<form") {
		t.Errorf("the page is not the join form, and it is still worth another press:\n%s", text(page))
	}
	if !strings.Contains(page, `value="Robin"`) || !strings.Contains(page, `<option value="Asia/Tokyo" selected>`) {
		t.Errorf("the form lost what was typed:\n%s", page)
	}
	if strings.Contains(page, "data-propose") {
		t.Error("the select is marked for the script to change, and the zone posted is the person's own choice")
	}
	if rec := postFrom(t, handler, InvitedPath(sitterLink), "203.0.113.2", aJoinForm()); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("another address: status = %d, want %d, so the budget spent was one address's", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestInvited_TheSeventhChallengeInAMinuteFromOneAddressIsRefusedAsPlainText(t *testing.T) {
	f := invitedGarden(t)
	handler := invitedMux(t, f)

	for i := range 6 {
		if rec := postFrom(t, handler, invitedChallengePath(sitterLink), "203.0.113.1", aJoinForm()); rec.Code != http.StatusOK {
			t.Fatalf("challenge %d: status = %d, want %d:\n%s", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
	rec := postFrom(t, handler, invitedChallengePath(sitterLink), "203.0.113.1", aJoinForm())

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the seventh challenge: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if strings.TrimSpace(rec.Body.String()) != tooManyInviteAttempts {
		t.Errorf("the body is %q, want the rate-limit sentence alone, because the page's script shows it as it is", rec.Body.String())
	}
	if n := f.count(t, "webauthn_ceremony"); n != 6 {
		t.Errorf("%d ceremony rows, want the 6 the challenges before the limit wrote", n)
	}
}
