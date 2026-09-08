package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

// samsSession is the token hash of the session the accept tests post from.
const samsSession = "sams-session"

// samOnFairview is Sam signed in on Fairview, the garden he owns. His timezone
// is Honolulu, eleven hours behind London where the inviter is, so an invite's
// end date falls on a different day for each of them.
func samOnFairview() auth.Principal {
	return auth.Principal{
		User:       store.AppUser{ID: otherUserID, DisplayName: "Sam", Handle: "sam", Timezone: "Pacific/Honolulu"},
		Garden:     store.Garden{ID: otherGardenID, Name: "Fairview"},
		Membership: store.Membership{GardenID: otherGardenID, UserID: otherUserID, Role: "owner"},
	}
}

// removeSamFromRosewood deletes Sam's membership of Rosewood, so he is an
// account in one garden being invited to another.
func (f *invitedFixture) removeSamFromRosewood(t *testing.T) {
	t.Helper()
	f.exec(t, "DELETE FROM membership WHERE garden_id = $1 AND user_id = $2", moreGardenID, otherUserID)
}

// startSessionFor inserts a session row for principal on its garden under
// tokenHash and returns principal with that hash set, the way Authenticate
// leaves it on a real request.
func (f *invitedFixture) startSessionFor(t *testing.T, principal auth.Principal, tokenHash string) auth.Principal {
	t.Helper()
	f.exec(t, "INSERT INTO session (token_hash, user_id, garden_id) VALUES ($1, $2, $3)", tokenHash, principal.User.ID, principal.Garden.ID)
	principal.Session.TokenHash = tokenHash
	return principal
}

// asAccount calls handler for token with principal on the request. A nil form
// makes it a GET.
func (f *invitedFixture) asAccount(t *testing.T, handler http.HandlerFunc, token string, principal auth.Principal, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, principal)
	method, body := http.MethodGet, strings.NewReader("")
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(ctx, method, acceptPath(token), body)
	req.SetPathValue("token", token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func (f *invitedFixture) showAccept(t *testing.T, token string, principal auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	return f.asAccount(t, f.handler.showAccept, token, principal, nil)
}

func (f *invitedFixture) accept(t *testing.T, token string, principal auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	return f.asAccount(t, f.handler.accept, token, principal, url.Values{})
}

// membershipOf returns the membership row for user on garden.
func (f *invitedFixture) membershipOf(t *testing.T, gardenID, userID uuid.UUID) store.Membership {
	t.Helper()
	row, err := f.queries.GetMembershipWithUserAndGarden(t.Context(), gardenID, userID)
	if err != nil {
		t.Fatalf("reading the membership: %v", err)
	}
	return row.Membership
}

// sessionGarden returns the garden the session under tokenHash is on.
func (f *invitedFixture) sessionGarden(t *testing.T, tokenHash string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT garden_id FROM session WHERE token_hash = $1", tokenHash).Scan(&id); err != nil {
		t.Fatalf("reading the session's garden: %v", err)
	}
	return id
}

// linkTo reports whether the page holds a link to href reading label.
func linkTo(page, href, label string) bool {
	anchor := `(?s)<a[^>]*href="` + regexp.QuoteMeta(href) + `"[^>]*>\s*` + regexp.QuoteMeta(label) + `\s*</a>`
	return regexp.MustCompile(anchor).MatchString(page)
}

// cannotAccept fails the test unless rec is the page for a link that cannot be
// accepted, and returns the page.
func cannotAccept(t *testing.T, rec *httptest.ResponseRecorder) string {
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
	if !linkTo(page, todayPath, "Back to Fairview") {
		t.Errorf("the page has no link back to the garden the session is on:\n%s", page)
	}
	return page
}

func TestAccept_ThePageOffersJoinWithTheEndDateInTheAccountsOwnZone(t *testing.T) {
	f := invitedGarden(t)
	f.removeSamFromRosewood(t)

	rec := f.showAccept(t, sitterLink, samOnFairview())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"Join Rosewood as Sam?",
		"Ellie invited you to Rosewood. You’ll join as a sitter. Sitters can log care and view everything, but not add plants or photos. Your access ends on 17 Sep.",
		`<form method="post" action="` + acceptPath(sitterLink) + `">`,
		"Not Sam? <a href=\"" + signInToAcceptPath(sitterLink) + "\">Sign in as someone else</a>.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	buttonNamed(t, page, "Join Rosewood")
	if strings.Contains(page, "nav__item") {
		t.Error("the page renders the tab bar, and it is about a garden the session is not on")
	}
	if at := f.redeemedAt(t, sitterLink); at != nil {
		t.Error("opening the page marked the invite used")
	}
}

func TestAccept_AcceptingWritesTheMembershipFromTheInviteMovesTheSessionAndLandsOnToday(t *testing.T) {
	f := invitedGarden(t)
	f.removeSamFromRosewood(t)
	sam := f.startSessionFor(t, samOnFairview(), samsSession)
	users, memberships, passkeys := f.count(t, "app_user"), f.count(t, "membership"), f.count(t, "passkey_credential")

	rec := f.accept(t, sitterLink, sam)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	m := f.membershipOf(t, moreGardenID, otherUserID)
	if m.Role != "sitter" || m.InvitedBy == nil || *m.InvitedBy != moreUserID || m.DigestHour != digestHourDefault {
		t.Errorf("the membership is %+v, want a sitter invited by Ellie with the digest at %d", m, digestHourDefault)
	}
	if m.ExpiresAt == nil || !m.ExpiresAt.Equal(sitterAccessEnds) {
		t.Errorf("the membership ends %v, want the day the invite set, %v", m.ExpiresAt, sitterAccessEnds)
	}
	var preferences int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM notification_preference WHERE membership_id = $1", m.ID).Scan(&preferences); err != nil {
		t.Fatal(err)
	}
	if preferences != 2 {
		t.Errorf("the membership has %d notification preferences, want 2", preferences)
	}
	if got := f.sessionGarden(t, samsSession); got != moreGardenID {
		t.Errorf("the session is on %s, want Rosewood", got)
	}
	var last *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT last_garden_id FROM app_user WHERE id = $1", otherUserID).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last == nil || *last != moreGardenID {
		t.Errorf("the account's last garden is %v, want Rosewood, so the next session starts there", last)
	}
	if at := f.redeemedAt(t, sitterLink); at == nil || !at.Equal(thursday) {
		t.Errorf("the invite was redeemed at %v, want %v", at, thursday)
	}
	if f.count(t, "app_user") != users || f.count(t, "passkey_credential") != passkeys {
		t.Error("accepting wrote an account or a passkey, and the account has both already")
	}
	if f.count(t, "membership") != memberships+1 {
		t.Errorf("%d memberships, want one more than before", f.count(t, "membership"))
	}
	cannotAccept(t, f.showAccept(t, sitterLink, sam))
	unusable(t, f.show(t, sitterLink))
}

func TestAccept_AcceptingAnInviteWakesTheDigestJob(t *testing.T) {
	f := invitedGarden(t)
	f.removeSamFromRosewood(t)
	sam := f.startSessionFor(t, samOnFairview(), samsSession)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.accept(t, sitterLink, sam)

	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, so the new membership waits for the job's timer", woken)
	}
}

func TestAccept_AnEndedMembershipIsRenewedFromTheInviteRatherThanDuplicated(t *testing.T) {
	f := invitedGarden(t)
	// Clare's access to Rosewood ended twelve days ago. She gets a sitter
	// membership of Fairview to be signed in on. Her digest hour on Rosewood is
	// set to 21 rather than the default, so a renewal that reset the row shows
	// up.
	f.exec(t, "UPDATE membership SET digest_hour = 21 WHERE garden_id = $1 AND user_id = $2", moreGardenID, peopleClareID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'sitter', 8)", otherGardenID, peopleClareID)
	clare := f.startSessionFor(t, auth.Principal{
		User:       store.AppUser{ID: peopleClareID, DisplayName: "Clare", Handle: "clare", Timezone: "Europe/London"},
		Garden:     store.Garden{ID: otherGardenID, Name: "Fairview"},
		Membership: store.Membership{GardenID: otherGardenID, UserID: peopleClareID, Role: "sitter"},
	}, "clares-session")
	memberships := f.count(t, "membership")

	if rec := f.showAccept(t, memberLink, clare); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Join Rosewood as Clare?") {
		t.Fatalf("the page: status = %d:\n%s", rec.Code, text(rec.Body.String()))
	}
	rec := f.accept(t, memberLink, clare)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	m := f.membershipOf(t, moreGardenID, peopleClareID)
	if m.ID != peopleClareID {
		t.Errorf("the membership is %s, want the row Clare already had, %s", m.ID, peopleClareID)
	}
	if m.Role != "member" || m.ExpiresAt != nil || m.InvitedBy == nil || *m.InvitedBy != moreUserID {
		t.Errorf("the membership is %+v, want a member with no end date invited by Ellie", m)
	}
	if m.DigestHour != 21 {
		t.Errorf("the digest is at %d, want the hour Clare had chosen, 21", m.DigestHour)
	}
	if f.count(t, "membership") != memberships {
		t.Errorf("%d memberships, want the same as before, because the row was renewed", f.count(t, "membership"))
	}
	if other := f.membershipOf(t, otherGardenID, peopleClareID); other.Role != "sitter" || other.ExpiresAt != nil || other.InvitedBy != nil {
		t.Errorf("Clare's membership of Fairview is %+v, want the sitter row she had, untouched", other)
	}
	if got := f.sessionGarden(t, "clares-session"); got != moreGardenID {
		t.Errorf("the session is on %s, want Rosewood", got)
	}
	if at := f.redeemedAt(t, memberLink); at == nil {
		t.Error("the invite was not marked used")
	}
}

func TestAccept_AnAccountAlreadyInTheGardenIsToldSoAndTheLinkStaysUnused(t *testing.T) {
	f := invitedGarden(t)
	sam := f.startSessionFor(t, samOnFairview(), samsSession)
	memberships := f.count(t, "membership")

	rec := f.showAccept(t, sitterLink, sam)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"You’re already in Rosewood",
		"This account is already a member. The link hasn’t been used.",
		`<form method="post" action="` + gardensPath + `">`,
		`<input type="hidden" name="` + gardenField + `" value="` + moreGardenID.String() + `">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	buttonNamed(t, page, "Open Rosewood")
	if strings.Contains(page, "Join Rosewood") {
		t.Errorf("the page offers Join, and the account is in the garden already:\n%s", text(page))
	}

	rec = f.accept(t, sitterLink, sam)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("the post: status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if at := f.redeemedAt(t, sitterLink); at != nil {
		t.Error("the post marked the invite used")
	}
	if f.count(t, "membership") != memberships {
		t.Error("the post wrote a membership")
	}
	if m := f.membershipOf(t, moreGardenID, otherUserID); m.Role != "member" || m.ExpiresAt != nil {
		t.Errorf("Sam's membership is %+v, want the member row he had, untouched", m)
	}
	if got := f.sessionGarden(t, samsSession); got != otherGardenID {
		t.Errorf("the session moved to %s", got)
	}
}

func TestAccept_ALinkThatCannotBeAcceptedGetsOnePageWhateverTheReason(t *testing.T) {
	f := invitedGarden(t)
	sam := f.startSessionFor(t, samOnFairview(), samsSession)
	memberships := f.count(t, "membership")

	pages := map[string]string{}
	for _, token := range []string{usedLink, ranOutLink, noSuchLink, endedBeforeJoiningLink, samsLink} {
		pages[token] = cannotAccept(t, f.showAccept(t, token, sam))
		cannotAccept(t, f.accept(t, token, sam))
	}

	for token, page := range pages {
		if page != pages[noSuchLink] {
			t.Errorf("the page for %s differs from the page for a link that was never issued, so the two can be told apart", token)
		}
	}
	if f.count(t, "membership") != memberships {
		t.Error("a post wrote a membership")
	}
	if at := f.redeemedAt(t, samsLink); at != nil {
		t.Error("the re-enrolment link was marked used, and accepting it adds no device")
	}
}

func TestAccept_TheSameLinkAcceptedByTwoAccountsAdmitsTheFirst(t *testing.T) {
	f := invitedGarden(t)
	f.removeSamFromRosewood(t)
	sam := f.startSessionFor(t, samOnFairview(), samsSession)
	robinID := uuid.NewV7()
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Robin', 'robin', 'Europe/London')", robinID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'member', 8)", otherGardenID, robinID)
	robin := f.startSessionFor(t, auth.Principal{
		User:       store.AppUser{ID: robinID, DisplayName: "Robin", Handle: "robin", Timezone: "Europe/London"},
		Garden:     store.Garden{ID: otherGardenID, Name: "Fairview"},
		Membership: store.Membership{GardenID: otherGardenID, UserID: robinID, Role: "member"},
	}, "robins-session")
	memberships := f.count(t, "membership")

	if rec := f.accept(t, sitterLink, sam); rec.Code != http.StatusSeeOther {
		t.Fatalf("the first post: status = %d:\n%s", rec.Code, text(rec.Body.String()))
	}
	rec := f.accept(t, sitterLink, robin)

	cannotAccept(t, rec)
	if f.count(t, "membership") != memberships+1 {
		t.Errorf("%d memberships, want one more than before", f.count(t, "membership"))
	}
	if got := f.sessionGarden(t, "robins-session"); got != otherGardenID {
		t.Errorf("Robin's session moved to %s", got)
	}
}

func TestInvited_AJoinLinkOffersSignInForSomebodyWithAnAccountAndAReenrolmentLinkDoesNot(t *testing.T) {
	f := invitedGarden(t)

	page := f.show(t, sitterLink).Body.String()
	if !strings.Contains(page, `Already have an account? <a href="`+signInToAcceptPath(sitterLink)+`">Sign in to join</a>.`) {
		t.Errorf("the join page does not offer sign in for somebody with an account:\n%s", page)
	}
	if !strings.Contains(signInToAcceptPath(sitterLink), signInPath+"?"+nextField+"=") {
		t.Errorf("the sign-in link %s carries no path to come back to", signInToAcceptPath(sitterLink))
	}

	page = f.show(t, samsLink).Body.String()
	if strings.Contains(page, "Sign in to join as yourself") {
		t.Errorf("the re-enrolment page offers sign in to join, and the account is in the garden already:\n%s", text(page))
	}
}

// ceremonyAs returns the sign-in handlers over the fixture's transaction with
// principal as the account the fixture posts as, so a device can be enrolled
// for an account other than Ellie.
func (f *invitedFixture) ceremonyAs(t *testing.T, principal auth.Principal) *passkeyCeremony {
	t.Helper()
	f.principal = principal
	return ceremonyOn(t, f.moreFixture)
}

// onNoGarden inserts a session row with no garden for user under tokenHash. It
// returns the principal Authenticate would attach for that session, with only
// Session and User set.
func (f *invitedFixture) onNoGarden(t *testing.T, user store.AppUser, tokenHash string) auth.Principal {
	t.Helper()
	f.exec(t, "INSERT INTO session (token_hash, user_id) VALUES ($1, $2)", tokenHash, user.ID)
	return auth.Principal{Session: store.Session{TokenHash: tokenHash, UserID: user.ID}, User: user}
}

func (f *invitedFixture) sessionGardenOrNil(t *testing.T, tokenHash string) *uuid.UUID {
	t.Helper()
	var id *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT garden_id FROM session WHERE token_hash = $1", tokenHash).Scan(&id); err != nil {
		t.Fatalf("reading the session's garden: %v", err)
	}
	return id
}

func TestSignIn_AnAccountInNoGardenSigningInFromAnInviteLinkLandsOnTheAcceptPageWithASession(t *testing.T) {
	f := invitedGarden(t)
	h := f.ceremonyAs(t, samOnFairview())
	device := aDevice()
	f.enrolDevice(t, h, device)
	// Sam was removed from Rosewood and his own garden is gone, so no
	// membership is live.
	f.exec(t, "DELETE FROM membership WHERE user_id = $1", otherUserID)
	memberships := f.count(t, "membership")

	rec := f.signInWithNext(t, h, device, acceptPath(sitterLink))

	principal := f.sessionOf(t, rec, acceptPath(sitterLink))
	if principal.InGarden() || principal.User.ID != otherUserID {
		t.Errorf("the session is %s on %q, want Sam in no garden", principal.User.DisplayName, principal.Garden.Name)
	}
	if f.count(t, "membership") != memberships {
		t.Error("the sign-in wrote a membership, and accepting is the accept page's post")
	}
	if at := f.redeemedAt(t, sitterLink); at != nil {
		t.Error("the sign-in marked the invite used")
	}
}

func TestAccept_AnAccountInNoGardenSeesJoinAndAcceptingPutsTheSessionOnTheGarden(t *testing.T) {
	f := invitedGarden(t)
	f.exec(t, "DELETE FROM membership WHERE user_id = $1", otherUserID)
	sam := f.onNoGarden(t, samOnFairview().User, samsSession)
	users, passkeys := f.count(t, "app_user"), f.count(t, "passkey_credential")

	rec := f.showAccept(t, sitterLink, sam)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Join Rosewood as Sam?") {
		t.Fatalf("status = %d, want the Join page:\n%s", rec.Code, text(rec.Body.String()))
	}

	rec = f.accept(t, sitterLink, sam)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	if m := f.membershipOf(t, moreGardenID, otherUserID); m.Role != "sitter" || m.InvitedBy == nil || *m.InvitedBy != moreUserID {
		t.Errorf("the membership is %+v, want a sitter invited by Ellie", m)
	}
	if got := f.sessionGardenOrNil(t, samsSession); got == nil || *got != moreGardenID {
		t.Errorf("the session is on %v, want Rosewood", got)
	}
	if f.count(t, "app_user") != users || f.count(t, "passkey_credential") != passkeys {
		t.Error("accepting wrote an account or a passkey")
	}
}

func TestAccept_ALinkThatCannotBeUsedOffersBackAndNamesNoGardenForASessionOnNone(t *testing.T) {
	f := invitedGarden(t)
	f.exec(t, "DELETE FROM membership WHERE user_id = $1", otherUserID)
	sam := f.onNoGarden(t, samOnFairview().User, samsSession)

	page := f.showAccept(t, usedLink, sam).Body.String()

	if !linkTo(page, todayPath, "Back") || strings.Contains(page, "Back to") {
		t.Errorf("the page for a link that cannot be used names a garden the session is not on:\n%s", text(page))
	}
}

func TestAccept_AnAccountWhoseMembershipEndedAcceptsFromNoGardenAndHasTheRowRenewed(t *testing.T) {
	f := invitedGarden(t)
	// Clare's access to Rosewood ended twelve days ago, and she is in no
	// other garden.
	clare := f.onNoGarden(t, store.AppUser{ID: peopleClareID, DisplayName: "Clare", Handle: "clare", Timezone: "Europe/London"}, "clares-session")
	memberships := f.count(t, "membership")

	rec := f.accept(t, memberLink, clare)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	if m := f.membershipOf(t, moreGardenID, peopleClareID); m.ID != peopleClareID || m.Role != "member" || m.ExpiresAt != nil {
		t.Errorf("the membership is %+v, want the row Clare had, now a member with no end date", m)
	}
	if f.count(t, "membership") != memberships {
		t.Errorf("%d memberships, want the same as before, because the row was renewed", f.count(t, "membership"))
	}
	if got := f.sessionGardenOrNil(t, "clares-session"); got == nil || *got != moreGardenID {
		t.Errorf("the session is on %v, want Rosewood", got)
	}
}

func TestInvited_ASignedInBrowserOpeningAJoinLinkIsSentToAcceptItAsThatAccount(t *testing.T) {
	f := invitedGarden(t)
	token, _, err := f.handler.sessions.Create(t.Context(), thursday, otherUserID, &otherGardenID, nil, "")
	if err != nil {
		t.Fatalf("starting Sam's session: %v", err)
	}
	cookie := f.handler.sessions.Cookie(token)

	rec := f.request(t, f.handler.show, sitterLink, invitedPath(sitterLink), nil, cookie)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != acceptPath(sitterLink) {
		t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, acceptPath(sitterLink))
	}
	if rec := f.request(t, f.handler.show, samsLink, invitedPath(samsLink), nil, cookie); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Add this device to your account") {
		t.Errorf("a re-enrolment link: status = %d, want the form, because the link adds a device to the account it names:\n%s", rec.Code, text(rec.Body.String()))
	}
	unusable(t, f.request(t, f.handler.show, usedLink, invitedPath(usedLink), nil, cookie))
}

func TestInvited_ABrowserWhoseMembershipEndedOpeningAJoinLinkIsSentToAcceptItAsThatAccount(t *testing.T) {
	f := invitedGarden(t)
	// Clare's access to Rosewood ended twelve days ago. Her session row still
	// names Rosewood until a request resolves it.
	token, _, err := f.handler.sessions.Create(t.Context(), thursday, peopleClareID, &moreGardenID, nil, "")
	if err != nil {
		t.Fatalf("starting Clare's session: %v", err)
	}

	rec := f.request(t, f.handler.show, sitterLink, invitedPath(sitterLink), nil, f.handler.sessions.Cookie(token))

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != acceptPath(sitterLink) {
		t.Errorf("status = %d, Location = %q, want %d to %s, because the account is still Clare's", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, acceptPath(sitterLink))
	}
}
