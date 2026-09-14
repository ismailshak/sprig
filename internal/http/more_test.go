package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/build"
	"github.com/ismailshak/sprig/internal/pgtest"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	moreGardenID     = uuid.MustParse("00000000-0000-7000-8000-000000000301")
	otherGardenID    = uuid.MustParse("00000000-0000-7000-8000-000000000302")
	moreUserID       = uuid.MustParse("00000000-0000-7000-8000-000000000303")
	otherUserID      = uuid.MustParse("00000000-0000-7000-8000-000000000304")
	moreMembershipID = uuid.MustParse("00000000-0000-7000-8000-000000000305")
	phoneKeyID       = uuid.MustParse("00000000-0000-7000-8000-000000000306")
	laptopKeyID      = uuid.MustParse("00000000-0000-7000-8000-000000000307")
	strangerKeyID    = uuid.MustParse("00000000-0000-7000-8000-000000000308")
	phonePushID      = uuid.MustParse("00000000-0000-7000-8000-000000000309")
	laptopPushID     = uuid.MustParse("00000000-0000-7000-8000-000000000310")
	strangerPushID   = uuid.MustParse("00000000-0000-7000-8000-000000000311")
)

// The phone and the laptop are Ellie's subscribed browsers. The stranger is
// Sam's, in the other garden.
const (
	phoneEndpoint    = "https://push.invalid/phone"
	laptopEndpoint   = "https://push.invalid/laptop"
	strangerEndpoint = "https://push.invalid/stranger"
)

// moreRosewood is Ellie's garden on the pages under More, and moreFairview is
// Sam's.
var (
	moreRosewood = ownedGarden{
		id:           moreGardenID,
		name:         "Rosewood",
		owner:        store.AppUser{ID: moreUserID, DisplayName: "Ellie", Handle: "ellie", Timezone: "Europe/London"},
		membershipID: moreMembershipID,
	}
	moreFairview = ownedGarden{
		id:    otherGardenID,
		name:  "Fairview",
		owner: store.AppUser{ID: otherUserID, DisplayName: "Sam", Handle: "sam", Timezone: "Europe/London"},
	}
)

type moreFixture struct {
	handler *more
	// todayHandler serves POST /gardens. The switch tests use this fixture
	// because its seed already has two gardens: Ellie's Rosewood and Sam's
	// Fairview.
	todayHandler *today
	tx           pgx.Tx
	principal    auth.Principal
}

// moreGarden sets up the account the four pages under More read: Ellie owns
// Rosewood, has two passkeys and two subscribed browsers, gets the digest and
// not the activity messages, and has one invite out that nobody has taken up.
// Push is on, so the Notifications page has its form.
// Sam is a second account in a second garden, so a row that belongs to
// somebody else is in reach of every query the pages make.
func moreGarden(t *testing.T) *moreFixture {
	t.Helper()

	tx := pgtest.Tx(t, migrateSchema)
	moreRosewood.insert(t, tx)
	moreFairview.insert(t, tx)
	mustExec(t, tx, `INSERT INTO notification_preference (membership_id, kind, enabled)
		VALUES ($1, 'digest', true), ($1, 'activity', false)`, moreMembershipID)

	// The laptop's last_used_at is null, so its row says "Never used" in place
	// of a date.
	mustExec(t, tx, `INSERT INTO passkey_credential (id, user_id, credential_id, name, public_key, last_used_at)
		VALUES ($1, $2, 'phone', 'iPhone', '\x00', $3),
		       ($4, $2, 'laptop', 'MacBook Air', '\x00', NULL),
		       ($5, $6, 'stranger', 'iPhone', '\x00', NULL)`,
		phoneKeyID, moreUserID, thursday, laptopKeyID, strangerKeyID, otherUserID)
	mustExec(t, tx, `INSERT INTO push_subscription (id, user_id, endpoint, p256dh_key, auth_key, user_agent, last_sent_at)
		VALUES ($1, $2, $9, 'p', 'a', $3, $4),
		       ($5, $2, $10, 'p', 'a', $6, NULL),
		       ($7, $8, $11, 'p', 'a', $3, NULL)`,
		phonePushID, moreUserID, safariOniPhone, thursday, laptopPushID, chromeOnMac, strangerPushID, otherUserID,
		phoneEndpoint, laptopEndpoint, strangerEndpoint)

	// One pending invite, beside three that are not: one that has run out, one
	// that has been redeemed, and one in the other garden.
	mustExec(t, tx, `INSERT INTO invite (garden_id, token_hash, role, created_by, expires_at, redeemed_at)
		VALUES ($1, 'pending', 'sitter', $2, $3, NULL),
		       ($1, 'expired', 'sitter', $2, $4, NULL),
		       ($1, 'redeemed', 'sitter', $2, $3, $4),
		       ($5, 'other-garden', 'sitter', $6, $3, NULL)`,
		moreGardenID, moreUserID, thursday.AddDate(0, 0, 5), thursday.AddDate(0, 0, -1), otherGardenID, otherUserID)

	return &moreFixture{
		handler: &more{
			logger:    testLogger,
			queries:   store.New(tx),
			photos:    testPhotos(t),
			templates: testTemplates(),
			build:     build.Info{Version: "0.1.0", Revision: "8f2c1a4d3b29e7c05a1"},
			now:       func() time.Time { return thursday },
			pushKey:   testPushKey,
		},
		todayHandler: &today{
			logger:    testLogger,
			queries:   store.New(tx),
			templates: testTemplates(),
			now:       func() time.Time { return thursday },
		},
		tx: tx,
		// Ellie signed in to Rosewood. A test that needs another capability
		// adds it.
		principal: moreRosewood.principal(auth.GardenEdit, auth.MemberManage, auth.MemberInvite, auth.TokenManage),
	}
}

// do calls one of the More handlers with the fixture's principal on the
// request. A nil form makes it a GET.
func (f *moreFixture) do(t *testing.T, handler http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	method, body := http.MethodGet, io.Reader(nil)
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// remove posts to one of the Remove buttons, setting the path value the mux
// would have taken from the URL. These tests call the handler rather than the
// mux, so nothing else sets it.
func (f *moreFixture) remove(t *testing.T, handler http.HandlerFunc, wildcard string, id uuid.UUID, path string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, nil)
	req.SetPathValue(wildcard, id.String())
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// swap makes the request an htmx swap aimed at target sends: a POST of form,
// or a GET when form is nil. wildcard is set to id the way the mux would set
// it from the URL, and left alone when it is empty.
func (f *moreFixture) swap(t *testing.T, handler http.HandlerFunc, path, target, wildcard, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	method, body := http.MethodGet, io.Reader(nil)
	if form != nil {
		method, body = http.MethodPost, strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", target)
	if wildcard != "" {
		req.SetPathValue(wildcard, id)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// fragment returns the body of a swap's response. It fails on any status but
// 200, on a whole page, and on a response without the element with id in it.
func fragment(t *testing.T, rec *httptest.ResponseRecorder, id string) string {
	t.Helper()

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, body)
	}
	doc := readHTML(body)
	if doc.first(isTag("html")) != nil {
		t.Fatalf("the response is the whole page, want the %s element alone:\n%.120s", id, body)
	}
	if doc.byID(id) == nil {
		t.Fatalf("the response does not hold the %s element:\n%.200s", id, body)
	}
	return body
}

// page renders a GET and fails on any status but 200.
func (f *moreFixture) page(t *testing.T, handler http.HandlerFunc, path string) string {
	t.Helper()

	rec := f.do(t, handler, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func (f *moreFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	mustExec(t, f.tx, sql, args...)
}

// renderedLinkRow is a list item that is a link: the URL it points at, the
// label, and the note after the label, empty when the row has none.
type renderedLinkRow struct {
	href  string
	label string
	note  string
}

// linkRowsOf reads the page's link rows in page order.
func linkRowsOf(page string) []renderedLinkRow {
	return linkRowsIn(readHTML(page))
}

// linkRowsIn reads the link rows below scope in page order. A link row is a
// list item that holds a link and no form. A list item holding a form is a row
// open for editing.
func linkRowsIn(scope *element) []renderedLinkRow {
	var out []renderedLinkRow
	for _, item := range scope.all(isTag("li")) {
		link := item.first(isTag("a"))
		if link == nil || item.first(isTag("form")) != nil {
			continue
		}
		row := renderedLinkRow{href: link.attr("href")}
		runs := textRuns(link)
		if len(runs) > 0 {
			row.label = runs[0]
		}
		if len(runs) > 1 {
			row.note = runs[1]
		}
		out = append(out, row)
	}
	return out
}

// textRuns returns the text below e as a person reads it, one string for each
// run of text between elements that holds more than whitespace. A row's label
// and the note beside it are two runs.
func textRuns(e *element) []string {
	if e == nil {
		return nil
	}
	var runs []string
	for _, c := range e.children {
		switch c := c.(type) {
		case string:
			if run := (&element{children: []any{c}}).text(); run != "" {
				runs = append(runs, run)
			}
		case *element:
			runs = append(runs, textRuns(c)...)
		}
	}
	return runs
}

// buildLineOf returns the line at the bottom of More that names the running
// version.
func buildLineOf(page string) string {
	return readHTML(page).first(isTag("p"), func(e *element) bool { return strings.HasPrefix(e.text(), "sprig ") }).text()
}

func labelsOf(rows []renderedLinkRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.label)
	}
	return out
}

// noteOn is the note beside a row, and "" when the row has none.
func noteOn(t *testing.T, page, label string) string {
	t.Helper()

	for _, row := range linkRowsOf(page) {
		if row.label == label {
			return row.note
		}
	}
	t.Fatalf("the page has no %s row:\n%v", label, labelsOf(linkRowsOf(page)))
	return ""
}

func TestMore_AnOwnerSeesEveryRow(t *testing.T) {
	f := moreGarden(t)

	got := labelsOf(linkRowsOf(f.page(t, f.handler.show, morePath)))

	want := []string{"Account", "Appearance", "Passkeys", "Notifications", "Garden", "People", "Tokens"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows are %v, want %v", got, want)
	}
}

func TestMore_AMembersIndexHasTokensAndNeitherGardenNorPeople(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities = auth.Capabilities{auth.TokenManage: true}

	got := labelsOf(linkRowsOf(f.page(t, f.handler.show, morePath)))

	want := []string{"Account", "Appearance", "Passkeys", "Notifications", "Tokens"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows are %v, want %v", got, want)
	}
}

func TestMore_ASitterSeesNeitherGardenPeopleNorTokens(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities = auth.Capabilities{auth.CareLog: true}

	got := labelsOf(linkRowsOf(f.page(t, f.handler.show, morePath)))

	want := []string{"Account", "Appearance", "Passkeys", "Notifications"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows are %v, want %v", got, want)
	}
}

func TestMore_EachRowLinksToItsPage(t *testing.T) {
	f := moreGarden(t)

	var got []string
	for _, row := range linkRowsOf(f.page(t, f.handler.show, morePath)) {
		got = append(got, row.href)
	}

	want := []string{"/more/account", "/more/appearance", "/more/passkeys", "/more/notifications", "/more/garden", "/more/people", "/more/tokens"}
	if !slices.Equal(got, want) {
		t.Errorf("the rows point at %v, want %v", got, want)
	}
}

func TestMore_TheNotificationsRowSaysOffOnlyWhenBothTypesAreOff(t *testing.T) {
	f := moreGarden(t)

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Notifications"); got != "" {
		t.Errorf("with the digest on the row says %q, want nothing", got)
	}

	f.exec(t, "UPDATE notification_preference SET enabled = false WHERE membership_id = $1", moreMembershipID)
	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Notifications"); got != "Off" {
		t.Errorf("with both types off the row says %q, want %q", got, "Off")
	}

	f.exec(t, "UPDATE notification_preference SET enabled = true WHERE membership_id = $1 AND kind = 'activity'", moreMembershipID)
	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Notifications"); got != "" {
		t.Errorf("with the activity messages on the row says %q, want nothing", got)
	}
}

func TestMore_ThePeopleRowCountsOnlyInvitesThatCanStillBeRedeemed(t *testing.T) {
	f := moreGarden(t)

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "People"); got != "1 invite pending" {
		t.Errorf("the row says %q, want %q", got, "1 invite pending")
	}

	f.exec(t, `INSERT INTO invite (garden_id, token_hash, role, created_by, expires_at)
		VALUES ($1, 'second', 'member', $2, $3)`, moreGardenID, moreUserID, thursday.AddDate(0, 0, 5))
	if got := noteOn(t, f.page(t, f.handler.show, morePath), "People"); got != "2 invites pending" {
		t.Errorf("with two out the row says %q, want %q", got, "2 invites pending")
	}

	f.exec(t, "DELETE FROM invite WHERE garden_id = $1 AND redeemed_at IS NULL", moreGardenID)
	if got := noteOn(t, f.page(t, f.handler.show, morePath), "People"); got != "" {
		t.Errorf("with none out the row says %q, want nothing", got)
	}
}

func TestMore_ThePeopleRowDoesNotCountAReenrolmentLink(t *testing.T) {
	f := moreGarden(t)

	f.exec(t, `INSERT INTO invite (garden_id, token_hash, role, user_id, created_by, expires_at)
		VALUES ($1, 'reenrolment', 'member', $2, $3, $4)`, moreGardenID, otherUserID, moreUserID, thursday.AddDate(0, 0, 5))

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "People"); got != "1 invite pending" {
		t.Errorf("the row says %q, want %q; a re-enrolment link goes to somebody already in the garden", got, "1 invite pending")
	}
}

func TestMore_TheAccountRowSaysThereAreNoRecoveryCodesUntilABatchExists(t *testing.T) {
	f := moreGarden(t)

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Account"); got != "No recovery codes" {
		t.Errorf("holding none the row says %q, want %q", got, "No recovery codes")
	}

	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'one'), ($1, 'two')", moreUserID)
	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Account"); got != "" {
		t.Errorf("holding a batch the row says %q, want nothing", got)
	}
}

func TestMore_TheAccountRowSaysNoRecoveryCodesWhenEveryCodeHasBeenUsed(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash, used_at) VALUES ($1, 'spent-one', $2), ($1, 'spent-two', $2)", moreUserID, thursday)

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Account"); got != "No recovery codes" {
		t.Errorf("with every code used the row says %q, want %q", got, "No recovery codes")
	}
}

func TestMore_AMemberIsNotToldTheyHaveNoRecoveryCodes(t *testing.T) {
	f := moreGarden(t)
	f.principal.Capabilities = auth.Capabilities{auth.TokenManage: true}

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Account"); got != "" {
		t.Errorf("the row says %q to a member, want nothing", got)
	}
}

func TestMore_TheAccountRowStillSaysNoRecoveryCodesWhenAnotherAccountHoldsABatch(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'someone-elses')", otherUserID)

	if got := noteOn(t, f.page(t, f.handler.show, morePath), "Account"); got != "No recovery codes" {
		t.Errorf("the row says %q, want %q", got, "No recovery codes")
	}
}

func TestMore_TheBuildLineShowsTheVersionAndTheShortRevision(t *testing.T) {
	f := moreGarden(t)

	got := buildLineOf(f.page(t, f.handler.show, morePath))

	if want := "sprig 0.1.0 · 8f2c1a4"; got != want {
		t.Errorf("the build line is %q, want %q", got, want)
	}
}

func TestMore_TheBuildLineShowsTheVersionAloneWhenTheBinaryHasNoRevision(t *testing.T) {
	f := moreGarden(t)
	f.handler.build = build.Info{Version: "(devel)"}

	got := buildLineOf(f.page(t, f.handler.show, morePath))

	if want := "sprig (devel)"; got != want {
		t.Errorf("the build line is %q, want %q", got, want)
	}
}

func TestMore_SigningOutDeletesTheSessionAndClearsTheCookie(t *testing.T) {
	f := moreGarden(t)
	queries := store.New(f.tx)
	sessions := auth.NewSessions(queries, testTTL, auth.CookieSettings{Name: "__Host-sprig_session", Secure: true})
	f.handler.sessions = sessions
	token, _, err := sessions.Create(t.Context(), thursday, moreUserID, &moreGardenID, nil, "a browser")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, signOutPath, nil)
	req.AddCookie(&http.Cookie{Name: "__Host-sprig_session", Value: token})
	rec := httptest.NewRecorder()
	f.handler.signOut(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signInPath {
		t.Errorf("status = %d to %q, want %d to %s", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, signInPath)
	}
	if _, _, err := sessions.Lookup(t.Context(), thursday, token); err == nil {
		t.Error("the session still resolves after signing out")
	}
	cookie := rec.Result().Cookies()[0]
	if cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Errorf("the cookie is %q with MaxAge %d, want an empty value and a negative MaxAge", cookie.Value, cookie.MaxAge)
	}
}

func TestMore_TheAppearancePageOffersLightDarkAndSystemWithSystemChecked(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.appearance, appearancePath)

	var values, checked []string
	for _, radio := range readHTML(page).all(isTag("input"), attrIs("name", "mode")) {
		values = append(values, radio.attr("value"))
		if radio.has("checked") {
			checked = append(checked, radio.attr("value"))
		}
		// The page's script enables the radios. Without it they stay disabled.
		if !radio.has("disabled") {
			t.Errorf("the %s radio is rendered enabled, want disabled", radio.attr("value"))
		}
	}
	if want := []string{"light", "dark", "system"}; !slices.Equal(values, want) {
		t.Errorf("the radios are %v, want %v", values, want)
	}
	if want := []string{"system"}; !slices.Equal(checked, want) {
		t.Errorf("the checked radios are %v, want %v, because without a script the page follows the system", checked, want)
	}
	if !strings.Contains(text(page), "Without JavaScript, sprig follows your device’s light or dark setting.") {
		t.Errorf("the page does not say what happens without JavaScript:\n%s", text(page))
	}
}
