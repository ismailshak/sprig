package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

// signedInTo runs the public setup post. That writes the account Robin, the
// garden Greenhouse and a session on it. It returns the principal Authenticate
// would put on a request for that session, and the session token.
func (f *setupFixture) signedInTo(t *testing.T) (auth.Principal, string) {
	t.Helper()
	token := f.mustCreate(t, aGardenForm(), aDevice())
	return f.principalOf(t, token), token
}

// asAccount calls handler with principal on the request. A nil form makes it
// a GET.
func (f *setupFixture) asAccount(t *testing.T, handler http.HandlerFunc, principal auth.Principal, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, principal)
	method, body := http.MethodGet, strings.NewReader("")
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(ctx, method, setupSignedInPath, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// counts returns the number of rows in each table a setup writes to.
func (f *setupFixture) counts(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{"app_user", "garden", "membership", "care_type", "passkey_credential", "session"} {
		counts[table] = f.count(t, table)
	}
	return counts
}

// lastGarden returns the garden recorded on the account as the one to start
// the next session on.
func (f *setupFixture) lastGarden(t *testing.T, userID uuid.UUID) *uuid.UUID {
	t.Helper()
	var last *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT last_garden_id FROM app_user WHERE id = $1", userID).Scan(&last); err != nil {
		t.Fatalf("reading the account's last garden: %v", err)
	}
	return last
}

func TestSetupSignedIn_ThePageNamesTheAccountAndAsksForTheGardenNameAlone(t *testing.T) {
	f := setupOn(t, true)
	robin, _ := f.signedInTo(t)

	rec := f.asAccount(t, f.handler.showSignedIn, robin, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	page := rec.Body.String()
	for _, want := range []string{
		"Set up a garden as Robin?",
		`<form method="post" action="` + setupSignedInPath + `"`,
		"Garden name",
		`id="garden" name="garden"`,
		"Not Robin? <a href=\"" + signInToSetUpPath + "\">Sign in as somebody else</a>.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %s:\n%s", want, page)
		}
	}
	buttonNamed(t, page, createLabel)
	for _, absent := range []string{`name="name"`, `name="timezone"`, "data-passkey", "nav__item"} {
		if strings.Contains(page, absent) {
			t.Errorf("the page has %s on it, and the account already has a name, a timezone and a passkey:\n%s", absent, page)
		}
	}
}

func TestSetupSignedIn_CreatingWritesAGardenTheAccountOwnsMovesTheSessionAndLandsOnToday(t *testing.T) {
	f := setupOn(t, true)
	robin, token := f.signedInTo(t)
	before := f.counts(t)

	rec := f.asAccount(t, f.handler.createSignedIn, robin, url.Values{"garden": {" Allotment "}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	after := f.counts(t)
	for table, more := range map[string]int{"app_user": 0, "passkey_credential": 0, "session": 0, "garden": 1, "membership": 1, "care_type": 3} {
		if after[table] != before[table]+more {
			t.Errorf("%s has %d rows, want %d", table, after[table], before[table]+more)
		}
	}

	now := f.principalOf(t, token)
	if now.User.ID != robin.User.ID {
		t.Errorf("the session is %s's, want Robin's, %s", now.User.ID, robin.User.ID)
	}
	if now.Garden.Name != "Allotment" || now.Membership.Role != "owner" {
		t.Errorf("the session is on %q as %s, want Allotment as owner", now.Garden.Name, now.Membership.Role)
	}
	if last := f.lastGarden(t, robin.User.ID); last == nil || *last != now.Garden.ID {
		t.Errorf("the account's last garden is %v, want Allotment", last)
	}
	types, err := f.queries.ListCareTypes(t.Context(), now.Garden.ID)
	if err != nil {
		t.Fatalf("listing the care types: %v", err)
	}
	if len(types) != 3 {
		t.Errorf("Allotment has %d care types, want 3 of its own", len(types))
	}

	// liveGardens is the query behind the garden sheet.
	gardens, err := liveGardens(t.Context(), f.queries, robin.User.ID, thursday)
	if err != nil {
		t.Fatalf("listing the gardens: %v", err)
	}
	names := map[string]string{}
	for _, g := range gardens {
		names[g.Garden.Name] = g.Membership.Role
	}
	if len(names) != 2 || names["Greenhouse"] != "owner" || names["Allotment"] != "owner" {
		t.Errorf("the account's gardens are %v, want Greenhouse and Allotment, both as owner", names)
	}
}

func TestSetupSignedIn_CreatingAGardenWakesTheDigestJob(t *testing.T) {
	f := setupOn(t, true)
	robin, _ := f.signedInTo(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.asAccount(t, f.handler.createSignedIn, robin, url.Values{"garden": {"Allotment"}})

	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1, so the new garden's digest waits for the job's timer", woken)
	}
}

func TestSetupSignedIn_ASitterMakesAGardenTheyOwn(t *testing.T) {
	f := setupOn(t, true)
	robin, token := f.signedInTo(t)
	// Robin is demoted to sitter, the role that cannot add a plant. The role
	// held in one garden does not decide the role in a garden the same account
	// makes.
	if _, err := f.tx.Exec(t.Context(), "UPDATE membership SET role = 'sitter' WHERE user_id = $1", robin.User.ID); err != nil {
		t.Fatalf("changing Robin's role: %v", err)
	}
	sitter := f.principalOf(t, token)
	if sitter.Can(auth.PlantCreate) {
		t.Fatalf("Robin can still add a plant, so the role is not %s", "sitter")
	}

	rec := f.asAccount(t, f.handler.createSignedIn, sitter, url.Values{"garden": {"Allotment"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	now := f.principalOf(t, token)
	if now.Garden.Name != "Allotment" || now.Membership.Role != "owner" {
		t.Errorf("the session is on %q as %s, want Allotment as owner", now.Garden.Name, now.Membership.Role)
	}
	if !now.Can(auth.MemberInvite) {
		t.Error("Robin cannot invite people to the garden they just made")
	}
}

func TestSetupSignedIn_AnAccountInNoGardenCreatesAGardenAndTheSessionIsOnTheNewGarden(t *testing.T) {
	f := setupOn(t, true)
	robin, _ := f.signedInTo(t)
	// Robin's only membership is deleted and a new session starts with no
	// garden, the way a sign-in does.
	if _, err := f.tx.Exec(t.Context(), "DELETE FROM membership WHERE user_id = $1", robin.User.ID); err != nil {
		t.Fatalf("removing Robin: %v", err)
	}
	token, session, err := f.handler.sessions.Create(t.Context(), thursday, robin.User.ID, nil, nil, "")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}
	principal := auth.Principal{Session: session, User: robin.User}
	if page := f.asAccount(t, f.handler.showSignedIn, principal, nil).Body.String(); !strings.Contains(page, "Set up a garden as Robin?") {
		t.Errorf("the page does not offer the form to an account in no garden:\n%s", text(page))
	}

	rec := f.asAccount(t, f.handler.createSignedIn, principal, url.Values{"garden": {"Allotment"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != todayPath {
		t.Fatalf("status = %d, Location = %q, want %d to %s:\n%s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, todayPath, text(rec.Body.String()))
	}
	now := f.principalOf(t, token)
	if !now.InGarden() || now.Garden.Name != "Allotment" || now.Membership.Role != "owner" {
		t.Errorf("the session is on %q as %s, want Allotment as owner", now.Garden.Name, now.Membership.Role)
	}
}

func TestSetupSignedIn_AnEmptyGardenNameIsRefusedUnderTheFieldAndTheSessionIsStillOnTheGardenItWasOn(t *testing.T) {
	f := setupOn(t, true)
	robin, token := f.signedInTo(t)
	before := f.counts(t)

	rec := f.asAccount(t, f.handler.createSignedIn, robin, url.Values{"garden": {"   "}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), setupGardenMissing) {
		t.Errorf("the page does not say the garden needs a name:\n%s", text(rec.Body.String()))
	}
	if after := f.counts(t); after["garden"] != before["garden"] || after["membership"] != before["membership"] || after["care_type"] != before["care_type"] {
		t.Errorf("the refused post wrote rows: before %v, after %v", before, after)
	}
	if now := f.principalOf(t, token); now.Garden.Name != "Greenhouse" {
		t.Errorf("the session is on %q, want Greenhouse, the garden it was on", now.Garden.Name)
	}
}

func TestSetupSignedIn_WithSignUpOffBothRoutesAre404AndWriteNothing(t *testing.T) {
	f := setupOn(t, false)
	// The first account is the one registration an install with sign-up off
	// allows.
	robin, _ := f.signedInTo(t)
	before := f.counts(t)

	if rec := f.asAccount(t, f.handler.showSignedIn, robin, nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	rec := f.asAccount(t, f.handler.createSignedIn, robin, url.Values{"garden": {"Allotment"}})

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if after := f.counts(t); after["garden"] != before["garden"] || after["membership"] != before["membership"] {
		t.Errorf("the closed route wrote rows: before %v, after %v", before, after)
	}
}

func TestSetup_ThePageLinksToSignInForSomebodyWithAnAccount(t *testing.T) {
	f := setupOn(t, true)

	rec := f.request(t, f.handler.show, setupPath, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !linkTo(rec.Body.String(), signInToSetUpPath, "Sign in to set up a garden as yourself") {
		t.Errorf("the page has no link to sign in with the next path set to %s:\n%s", setupSignedInPath, rec.Body.String())
	}
}

func TestSetup_WithSignUpOffTheFirstRunPageHasNoSignInLink(t *testing.T) {
	f := setupOn(t, false)

	rec := f.request(t, f.handler.show, setupPath, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	page := rec.Body.String()
	if linkTo(page, signInToSetUpPath, "Sign in to set up a garden as yourself") || strings.Contains(page, "Already have a sprig account?") {
		t.Errorf("the page offers sign in, and with sign-up off it is served on an install with no account:\n%s", page)
	}
}

func TestSetup_ASignedInBrowserOpeningTheFormIsSentToSetUpAGardenAsThatAccount(t *testing.T) {
	f := setupOn(t, true)
	token := f.mustCreate(t, aGardenForm(), aDevice())

	rec := f.request(t, f.handler.show, setupPath, nil, &http.Cookie{Name: "__Host-sprig_session", Value: token})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != setupSignedInPath {
		t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, setupSignedInPath)
	}
}

func TestSetup_ASessionOnNoGardenOpeningTheFormIsSentToSetUpAGardenAsThatAccount(t *testing.T) {
	f := setupOn(t, true)
	robin, token := f.signedInTo(t)
	// Robin's only membership is deleted, so the foreign key clears the
	// session's garden. The form at /setup writes a new account, and a person
	// whose access ended must not be handed it.
	if _, err := f.tx.Exec(t.Context(), "DELETE FROM membership WHERE user_id = $1", robin.User.ID); err != nil {
		t.Fatalf("removing Robin: %v", err)
	}

	rec := f.request(t, f.handler.show, setupPath, nil, &http.Cookie{Name: "__Host-sprig_session", Value: token})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != setupSignedInPath {
		t.Errorf("status = %d, Location = %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, setupSignedInPath)
	}
}

func TestSetup_ASessionCookieThatResolvesToNothingStillGetsTheForm(t *testing.T) {
	f := setupOn(t, true)
	f.mustCreate(t, aGardenForm(), aDevice())

	rec := f.request(t, f.handler.show, setupPath, nil, &http.Cookie{Name: "__Host-sprig_session", Value: "not-a-session"})

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, text(rec.Body.String()))
	}
	buttonNamed(t, rec.Body.String(), createLabel)
}
