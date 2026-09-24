package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

var (
	peopleSamID      = uuid.MustParse("00000000-0000-7000-8000-000000000330")
	peopleJoID       = uuid.MustParse("00000000-0000-7000-8000-000000000331")
	peopleClareID    = uuid.MustParse("00000000-0000-7000-8000-000000000332")
	peopleSecondID   = uuid.MustParse("00000000-0000-7000-8000-000000000333")
	peoplePlantID    = uuid.MustParse("00000000-0000-7000-8000-000000000334")
	peopleWaterID    = uuid.MustParse("00000000-0000-7000-8000-000000000335")
	peopleEventID    = uuid.MustParse("00000000-0000-7000-8000-000000000336")
	pendingInviteID  = uuid.MustParse("00000000-0000-7000-8000-000000000337")
	peopleExpiredID  = uuid.MustParse("00000000-0000-7000-8000-000000000338")
	peopleRedeemedID = uuid.MustParse("00000000-0000-7000-8000-000000000340")
	peopleFinnID     = uuid.MustParse("00000000-0000-7000-8000-000000000341")
)

type peopleFixture struct {
	*moreFixture
	handler *people
}

// peopleGarden gives Rosewood the four rows the People page has to tell apart:
// Ellie who is reading it, Sam whose membership has no end date, Jo whose
// access ends in eight days, and Clare whose access ended twelve days ago.
// Rosewood's invites are replaced so this page's list is known. Fairview's is
// left where the More fixture put it, so another garden's row is in reach of
// every query the page makes.
func peopleGarden(t *testing.T) *peopleFixture {
	t.Helper()

	f := moreGarden(t)
	// The owner created the garden, so their membership is the oldest and
	// their row is at the top of the list.
	f.exec(t, "UPDATE membership SET created_at = '2026-01-01T00:00:00Z' WHERE id = $1", moreMembershipID)
	f.exec(t, `INSERT INTO app_user (id, display_name, handle, timezone) VALUES
		($1, 'Jo', 'jo', 'Europe/London'),
		($2, 'Clare', 'clare', 'Europe/London')`, peopleJoID, peopleClareID)
	f.exec(t, `INSERT INTO membership (id, garden_id, user_id, role, created_at, expires_at, digest_hour) VALUES
		($1, $2, $3, 'member', '2026-01-02T00:00:00Z', NULL, 8),
		($4, $2, $5, 'sitter', '2026-01-03T00:00:00Z', $6, 8),
		($7, $2, $8, 'sitter', '2026-01-04T00:00:00Z', $9, 8)`,
		peopleSamID, moreGardenID, otherUserID,
		peopleJoID, peopleJoID, thursday.AddDate(0, 0, 8),
		peopleClareID, peopleClareID, thursday.AddDate(0, 0, -12))

	f.exec(t, "DELETE FROM invite WHERE garden_id = $1", moreGardenID)
	f.exec(t, `INSERT INTO invite (id, garden_id, token_hash, role, created_by, created_at, expires_at, redeemed_at) VALUES
		($1, $2, 'pending', 'sitter', $3, $4, $5, NULL),
		($6, $2, 'ran-out', 'member', $3, $7, $8, NULL),
		($9, $2, 'redeemed', 'sitter', $3, $4, $5, $4)`,
		pendingInviteID, moreGardenID, moreUserID, thursday.AddDate(0, 0, -2), thursday.AddDate(0, 0, 5),
		peopleExpiredID, thursday.AddDate(0, 0, -8), thursday.AddDate(0, 0, -1), peopleRedeemedID)
	return &peopleFixture{
		moreFixture: f,
		handler:     &people{logger: testLogger, queries: store.New(f.tx), templates: testTemplates(), now: func() time.Time { return thursday }},
	}
}

func TestPeople_TheReadersOwnRowHasNoRoleSelectAndNoRemove(t *testing.T) {
	f := peopleGarden(t)

	row := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Ellie")

	if row.role != "" {
		t.Errorf("your own row offers the role %q, and an owner demoting themselves leaves a garden nobody can administer", row.role)
	}
	if len(row.acts) != 0 {
		t.Errorf("your own row offers %v, and it should offer nothing", row.acts)
	}
	// The row names the role and says nothing under the name, because there is
	// no choice for it to explain.
	if want := "Ellie You Owner"; row.says != want {
		t.Errorf("your own row reads %q, want %q", row.says, want)
	}
}

func TestPeople_APermanentMemberHasBothRolesTheirOwnSelectedAndNoEndDate(t *testing.T) {
	f := peopleGarden(t)

	row := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Sam")

	if row.role != "member" {
		t.Errorf("Sam's row has %q selected, want %q", row.role, "member")
	}
	if !slices.Equal(row.roles, []string{"member", "sitter"}) {
		t.Errorf("the select offers %v, want member and sitter", row.roles)
	}
	if want := "Sam " + roleWhat["member"]; row.says != want {
		t.Errorf("Sam's row reads %q, want %q", row.says, want)
	}
	if row.until != "" {
		t.Errorf("a permanent membership offers the end date %q; a date arrives with the invite", row.until)
	}
	if !slices.Equal(row.acts, []string{"Sign-in link", "Remove"}) {
		t.Errorf("Sam's row offers %v, want Sign-in link and Remove", row.acts)
	}
}

func TestPeople_ASitterWhoseAccessEndsShowsTheDayAndKeepsTheRoleSelect(t *testing.T) {
	f := peopleGarden(t)

	row := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Jo")

	if want := "Jo Sitter · until 11 Sep"; row.says != want {
		t.Errorf("Jo's row reads %q, want %q", row.says, want)
	}
	if row.until != "2026-09-11" {
		t.Errorf("the end date field holds %q, want %q", row.until, "2026-09-11")
	}
	if row.role != "sitter" {
		t.Errorf("a sitter whose access has not ended has %q selected, want %q", row.role, "sitter")
	}
}

func TestPeople_AMembershipThatHasEndedKeepsItsPlaceAndHasNoRoleSelect(t *testing.T) {
	f := peopleGarden(t)
	page := f.page(t, f.handler.show, PeoplePath)

	row := memberNamed(t, page, "Clare")

	if row.role != "" {
		t.Errorf("a membership that has ended offers the role %q, and there is no role to choose for access that has ended", row.role)
	}
	if want := "Clare Sitter · Access ended 22 Aug"; row.says != want {
		t.Errorf("Clare's row reads %q, want %q", row.says, want)
	}
	if row.until != "2026-08-22" {
		t.Errorf("the end date field holds %q, want %q; a lapsed sitter is re-dated from their own row", row.until, "2026-08-22")
	}
	if names := memberNames(page); !slices.Equal(names, []string{"Ellie", "Sam", "Jo", "Clare"}) {
		t.Errorf("the list reads %v, and a membership that has ended keeps its place in it", names)
	}
}

func TestPeople_AHandleIsShownOnlyWhereTwoMembersShareADisplayName(t *testing.T) {
	f := peopleGarden(t)
	if says := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Sam").says; says != "Sam "+roleWhat["member"] {
		t.Fatalf("Sam's row reads %q, and no other member is called Sam, so it shows no handle", says)
	}

	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ellie', 'ellie2', 'Europe/London')", peopleSecondID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'sitter', 8)", moreGardenID, peopleSecondID)
	page := f.page(t, f.handler.show, PeoplePath)

	for _, handle := range []string{"ellie", "ellie2"} {
		if !slices.ContainsFunc(membersShown(page), func(row shownMember) bool { return strings.HasPrefix(row.says, "Ellie "+handle+" ") }) {
			t.Errorf("no row shows the handle %q after the name, and two members are called Ellie", handle)
		}
	}
	if label := ariaLabels(page)["role.ellie2"]; label != "Ellie (ellie2)’s role" {
		t.Errorf("the select is named %q, and a name alone names both Ellies", label)
	}
}

func TestPeople_PendingInvitesIsAbsentOnceEveryInviteIsGone(t *testing.T) {
	f := peopleGarden(t)

	if got := invitesShown(f.page(t, f.handler.show, PeoplePath)); len(got) != 2 {
		t.Fatalf("Pending invites holds %v, want the live invite and the one that ran out", got)
	}

	f.exec(t, "DELETE FROM invite WHERE garden_id = $1", moreGardenID)
	page := f.page(t, f.handler.show, PeoplePath)

	if strings.Contains(text(page), "Pending invites") {
		t.Error("Pending invites is on the page with nothing in it")
	}
}

func TestPeople_AnInviteSaysWhenItWasSentAndWhetherItHasRunOut(t *testing.T) {
	f := peopleGarden(t)

	got := invitesShown(f.page(t, f.handler.show, PeoplePath))

	want := []string{"Sitter Sent Tuesday · expires in 5 days", "Member Sent 26 Aug · expired"}
	if !slices.Equal(got, want) {
		t.Errorf("Pending invites reads %v, want %v", got, want)
	}
}

func TestPeople_AnInviteThatRunsOutLaterTodayReadsAsExpiringInADay(t *testing.T) {
	f := peopleGarden(t)
	f.exec(t, "DELETE FROM invite WHERE garden_id = $1", moreGardenID)
	f.exec(t, `INSERT INTO invite (id, garden_id, token_hash, role, created_by, created_at, expires_at)
		VALUES ($1, $2, 'later-today', 'sitter', $3, $4, $5)`,
		pendingInviteID, moreGardenID, moreUserID, thursday.AddDate(0, 0, -7), thursday.Add(3*time.Hour))

	got := invitesShown(f.page(t, f.handler.show, PeoplePath))

	if want := []string{"Sitter Sent 27 Aug · expires in 1 day"}; !slices.Equal(got, want) {
		t.Errorf("Pending invites reads %v, want %v; the link works until this afternoon", got, want)
	}
	if note := noteOn(t, f.page(t, f.moreFixture.handler.show, morePath), "People"); note != "1 invite pending" {
		t.Errorf("More says %q, and the row it is counting is still pending", note)
	}
}

func TestPeople_ARoleSavedOnAMemberIsTheOneTheirRowShowsAfterwards(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"role.sam": {"sitter"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	row := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Sam")
	if row.role != "sitter" {
		t.Errorf("Sam's row has %q selected after the save, want %q", row.role, "sitter")
	}
	if want := "Sam " + roleWhat["sitter"]; row.says != want {
		t.Errorf("Sam's row reads %q, want %q", row.says, want)
	}
}

func TestPeople_ARoleTheSelectDoesNotOfferIsRefusedAndTheMemberIsUnchanged(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"role.sam": {"owner"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if role := roleOf(t, f.moreFixture, otherUserID); role != "member" {
		t.Errorf("Sam's role is %q, and the select never offered owner", role)
	}
}

func TestPeople_ARoleTheSelectDoesNotOfferLeavesTheRowsAboveItUnchanged(t *testing.T) {
	f := peopleGarden(t)

	// Sam's row is above Jo's, so Sam is written first if the save writes as
	// it reads.
	rec := f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"role.sam": {"sitter"}, "role.jo": {"owner"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if role := roleOf(t, f.moreFixture, otherUserID); role != "member" {
		t.Errorf("Sam's role is %q, and the post that carried it was refused", role)
	}
}

func TestPeople_ARolePostedForTheReadersOwnRowIsIgnored(t *testing.T) {
	f := peopleGarden(t)

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"role.ellie": {"sitter"}})

	if role := roleOf(t, f.moreFixture, moreUserID); role != "owner" {
		t.Errorf("the reader's role is %q, and their own row has no select to post from", role)
	}
}

func TestPeople_ANewDateOnAMembershipThatEndedGivesTheRowItsRoleSelectBack(t *testing.T) {
	f := peopleGarden(t)

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"until.clare": {"2026-10-01"}})

	row := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Clare")
	if row.role != "sitter" {
		t.Errorf("Clare's row has %q selected, want %q", row.role, "sitter")
	}
	if want := "Clare Sitter · until 1 Oct"; row.says != want {
		t.Errorf("Clare's row reads %q, want %q", row.says, want)
	}
}

func TestPeople_AnEndDatePostedForAPermanentMembershipIsNotWritten(t *testing.T) {
	f := peopleGarden(t)

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"until.sam": {"2026-10-01"}})

	var ends *time.Time
	row := f.tx.QueryRow(t.Context(), "SELECT expires_at FROM membership WHERE garden_id = $1 AND user_id = $2", moreGardenID, otherUserID)
	if err := row.Scan(&ends); err != nil {
		t.Fatal(err)
	}
	if ends != nil {
		t.Errorf("Sam's access ends at %s, and a permanent membership has no date field to post from", ends)
	}
}

func TestPeople_AMembersEndDateIsShownAndSavedInTheirOwnZone(t *testing.T) {
	f := peopleGarden(t)
	// Finn is in New York, five hours behind the reader in London. Their
	// access ends at an instant that is 13 September where they are and 14
	// September where the reader is.
	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Finn', 'finn', 'America/New_York')", peopleFinnID)
	f.exec(t, `INSERT INTO membership (garden_id, user_id, role, created_at, expires_at, digest_hour)
		VALUES ($1, $2, 'sitter', '2026-01-05T00:00:00Z', '2026-09-14T02:00:00Z', 8)`, moreGardenID, peopleFinnID)

	row := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Finn")
	if want := "Finn Sitter · until 13 Sep"; row.says != want {
		t.Errorf("Finn's row reads %q, want %q; the day is the one where they are", row.says, want)
	}
	if row.until != "2026-09-13" {
		t.Errorf("the end date field holds %q, want %q", row.until, "2026-09-13")
	}

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"until.finn": {"2026-09-20"}})

	var ends time.Time
	stored := f.tx.QueryRow(t.Context(), "SELECT expires_at FROM membership WHERE garden_id = $1 AND user_id = $2", moreGardenID, peopleFinnID)
	if err := stored.Scan(&ends); err != nil {
		t.Fatal(err)
	}
	// 20 September begins in New York at 04:00 UTC.
	if want := time.Date(2026, time.September, 20, 4, 0, 0, 0, time.UTC); !ends.Equal(want) {
		t.Errorf("Finn's access ends at %s, want %s; it stops when their day begins", ends.UTC(), want)
	}
}

func TestPeople_TheRemoveQuestionNamesThePersonAndWhatTheyKeep(t *testing.T) {
	f := peopleGarden(t)

	page := f.memberPage(t, http.MethodGet, f.handler.confirmRemoveMember, "sam", removeMemberPath("sam"))

	row := memberAsking(t, page)
	if !strings.HasPrefix(row.says, "Remove Sam? ") {
		t.Errorf("the row reads %q, want it to ask %q", row.says, "Remove Sam?")
	}
	if !strings.Contains(row.says, "Their name stays on everything they’ve logged") {
		t.Errorf("the question reads %q, and it has to say what removing them keeps", row.says)
	}
}

func TestPeople_RemovingAMemberTakesTheirSessionsOffThatGardenOnly(t *testing.T) {
	f := peopleGarden(t)
	f.exec(t, `INSERT INTO session (token_hash, user_id, garden_id) VALUES
		('rosewood', $1, $2), ('fairview', $1, $3)`, otherUserID, moreGardenID, otherGardenID)

	rec := f.member(t, http.MethodPost, f.handler.removeMember, "sam", removeMemberPath("sam"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	gardens := map[string]*uuid.UUID{}
	rows, err := f.tx.Query(t.Context(), "SELECT token_hash, garden_id FROM session WHERE user_id = $1", otherUserID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		var garden *uuid.UUID
		if err := rows.Scan(&hash, &garden); err != nil {
			t.Fatal(err)
		}
		gardens[hash] = garden
	}
	if len(gardens) != 2 || gardens["rosewood"] != nil || gardens["fairview"] == nil || *gardens["fairview"] != otherGardenID {
		t.Errorf("Sam's sessions are %v, want both still there, with the Rosewood one on no garden and the Fairview one where it was", gardens)
	}
}

func TestPeople_RemovingAMemberLeavesTheirCareEventsSignedWithTheirName(t *testing.T) {
	f := peopleGarden(t)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern')", peoplePlantID, moreGardenID)
	insertCareTypes(t, f.tx, moreGardenID, store.CareType{ID: peopleWaterID, Name: "Water", Slug: "water"})
	f.exec(t, `INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
		VALUES ($1, $2, $3, $4, $5, now(), true)`, peopleEventID, moreGardenID, peoplePlantID, peopleWaterID, otherUserID)

	f.member(t, http.MethodPost, f.handler.removeMember, "sam", removeMemberPath("sam"))

	var name string
	row := f.tx.QueryRow(t.Context(),
		"SELECT app_user.display_name FROM care_event JOIN app_user ON app_user.id = care_event.performed_by WHERE care_event.id = $1", peopleEventID)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("the event Sam logged is gone or unattributed: %v", err)
	}
	if name != "Sam" {
		t.Errorf("the event is signed %q, want %q", name, "Sam")
	}
}

func TestPeople_RemovingYourselfIsNotFound(t *testing.T) {
	f := peopleGarden(t)

	rec := f.member(t, http.MethodPost, f.handler.removeMember, "ellie", removeMemberPath("ellie"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; your own row offers no Remove", rec.Code, http.StatusNotFound)
	}
	if role := roleOf(t, f.moreFixture, moreUserID); role != "owner" {
		t.Errorf("the reader's membership is %q after removing themselves, and it should be untouched", role)
	}
}

func TestPeople_AReenrolmentLinkIsShownOnceAndOnlyItsHashIsStored(t *testing.T) {
	f := peopleGarden(t)

	page := f.memberPage(t, http.MethodPost, f.handler.reenrolMember, "sam", reenrolMemberPath("sam"))

	link := secretValue(t, page)
	if !strings.HasPrefix(link, "example.com/invite/") {
		t.Fatalf("the link reads %q, want the host this request arrived at and the path that redeems the token", link)
	}
	token := strings.TrimPrefix(link, "example.com/invite/")

	var stored string
	row := f.tx.QueryRow(t.Context(), "SELECT token_hash FROM invite WHERE user_id = $1", otherUserID)
	if err := row.Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token {
		t.Error("the invite row holds the link itself, and only its hash may be stored")
	}
	if stored != auth.HashToken(token) {
		t.Error("the stored hash is not the hash of the link that was shown")
	}
	if strings.Contains(f.page(t, f.handler.show, PeoplePath), token) {
		t.Error("the link is on the page again on the next request, and it is shown exactly once")
	}
}

func TestPeople_AReenrolmentLinkIsNotListedUnderPendingInvites(t *testing.T) {
	f := peopleGarden(t)
	before := invitesShown(f.page(t, f.handler.show, PeoplePath))

	f.memberPage(t, http.MethodPost, f.handler.reenrolMember, "sam", reenrolMemberPath("sam"))

	after := invitesShown(f.page(t, f.handler.show, PeoplePath))
	if !slices.Equal(before, after) {
		t.Errorf("Pending invites reads %v after a re-enrolment and %v before, and Sam is already a member", after, before)
	}
}

func TestPeople_ASecondReenrolmentLinkReplacesTheFirst(t *testing.T) {
	f := peopleGarden(t)

	first := secretValue(t, f.memberPage(t, http.MethodPost, f.handler.reenrolMember, "sam", reenrolMemberPath("sam")))
	second := secretValue(t, f.memberPage(t, http.MethodPost, f.handler.reenrolMember, "sam", reenrolMemberPath("sam")))

	var hashes []string
	rows, err := f.tx.Query(t.Context(), "SELECT token_hash FROM invite WHERE user_id = $1", otherUserID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	token := strings.TrimPrefix(second, "example.com/invite/")
	if want := []string{auth.HashToken(token)}; !slices.Equal(hashes, want) {
		t.Errorf("Sam has %d re-enrolment links, want only the one that was just shown", len(hashes))
	}
	if first == second {
		t.Error("the second press showed the first link again")
	}
}

func TestPeople_RevokingAnInviteTakesItOutOfTheList(t *testing.T) {
	f := peopleGarden(t)

	rec := f.remove(t, f.handler.revokeInvite, "invite", pendingInviteID, revokeInvitePath(pendingInviteID))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	got := invitesShown(f.page(t, f.handler.show, PeoplePath))
	if want := []string{"Member Sent 26 Aug · expired"}; !slices.Equal(got, want) {
		t.Errorf("Pending invites reads %v, want %v", got, want)
	}
}

func TestPeople_RevokingAnInviteThatIsAlreadyRedeemedIsNotFound(t *testing.T) {
	f := peopleGarden(t)

	rec := f.remove(t, f.handler.revokeInvite, "invite", peopleRedeemedID, revokeInvitePath(peopleRedeemedID))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; Revoke is only offered on rows nobody has opened", rec.Code, http.StatusNotFound)
	}
}

// member calls one of the handlers that name a member by handle in the URL.
// These tests call the handler rather than the mux, so nothing else sets the
// path value.
func (f *peopleFixture) member(t *testing.T, method string, handler http.HandlerFunc, handle, path string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	req.SetPathValue("member", handle)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// memberPage calls one of those handlers and fails on any status but 200.
func (f *peopleFixture) memberPage(t *testing.T, method string, handler http.HandlerFunc, handle, path string) string {
	t.Helper()

	rec := f.member(t, method, handler, handle, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	return rec.Body.String()
}

func roleOf(t *testing.T, f *moreFixture, userID uuid.UUID) string {
	t.Helper()

	var role string
	row := f.tx.QueryRow(t.Context(), "SELECT role FROM membership WHERE garden_id = $1 AND user_id = $2", moreGardenID, userID)
	if err := row.Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

// shownMember is one row of the Members list.
type shownMember struct {
	// name is the first text in the row. On a member's row that is their
	// display name. On the row asking "Remove Sam?" it is the question.
	name string
	// says is the row's text without its select, buttons and links: the name,
	// any handle, and the role or the line under the name.
	says string
	// role is the option the select has chosen, and roles every option it
	// offers. Both are empty on a row with no select.
	role  string
	roles []string
	// until is what the end date field holds, empty on a row with no field.
	until string
	// acts is the text of the row's buttons and links, in page order.
	acts []string
}

// membersShown reads the Members list in page order. A row is a list item in
// the form that saves the list.
func membersShown(page string) []shownMember {
	var out []shownMember
	for _, item := range formTo(page, PeoplePath).all(isTag("li")) {
		row := shownMember{name: firstText(item), says: textOutside(item, "select", "button", "a")}
		if roles := item.first(isTag("select")); roles != nil {
			row.role = selectedValue(roles)
			for _, option := range roles.all(isTag("option")) {
				row.roles = append(row.roles, option.attr("value"))
			}
		}
		row.until = item.first(isTag("input"), attrIs("type", "date")).attr("value")
		for _, control := range item.all(func(e *element) bool { return e.tag == "button" || e.tag == "a" }) {
			row.acts = append(row.acts, control.text())
		}
		out = append(out, row)
	}
	return out
}

// memberNamed returns the row for the person with this display name.
func memberNamed(t *testing.T, page, name string) shownMember {
	t.Helper()

	for _, row := range membersShown(page) {
		if row.name == name {
			return row
		}
	}
	t.Fatalf("no row for %q in:\n%s", name, page)
	return shownMember{}
}

// memberAsking returns the one row asking "Remove Ellie?". It is the row that
// offers Cancel.
func memberAsking(t *testing.T, page string) shownMember {
	t.Helper()

	for _, row := range membersShown(page) {
		if slices.Contains(row.acts, "Cancel") {
			return row
		}
	}
	t.Fatalf("no row is asking on:\n%s", page)
	return shownMember{}
}

func memberNames(page string) []string {
	rows := membersShown(page)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.name)
	}
	return out
}

// invitesShown reads the Pending invites list, one string per row. A row is a
// list item holding a Revoke button, and the string is its text without the
// button.
func invitesShown(page string) []string {
	var out []string
	for _, item := range readHTML(page).all(isTag("li")) {
		if item.first(isTag("button"), textIs("Revoke")) != nil {
			out = append(out, textOutside(item, "button"))
		}
	}
	return out
}

// secretValue returns the value a page shows once, such as a new token or an
// invite link.
func secretValue(t *testing.T, page string) string {
	t.Helper()

	value := readHTML(page).byID("secret-value")
	if value == nil {
		t.Fatalf("no value in a secret box on:\n%s", page)
	}
	return value.text()
}

// ariaLabels returns the accessible name of every named control on the page,
// keyed by the name it posts under.
func ariaLabels(page string) map[string]string {
	out := map[string]string{}
	for _, control := range readHTML(page).all(hasAttr("name"), hasAttr("aria-label")) {
		out[control.attr("name")] = control.attr("aria-label")
	}
	return out
}

// firstText returns the first run of text in e that is not only whitespace,
// trimmed.
func firstText(e *element) string {
	if e == nil {
		return ""
	}
	for _, c := range e.children {
		switch c := c.(type) {
		case string:
			if s := strings.TrimSpace(c); s != "" {
				return s
			}
		case *element:
			if s := firstText(c); s != "" {
				return s
			}
		}
	}
	return ""
}

// textOutside returns the text of e without the text of the elements below it
// that have one of the tags given.
func textOutside(e *element, tags ...string) string {
	return withoutTags(e, tags).text()
}

// withoutTags returns a copy of e that leaves out every element below it with
// one of the tags given.
func withoutTags(e *element, tags []string) *element {
	if e == nil {
		return nil
	}
	kept := &element{tag: e.tag, attrs: e.attrs}
	for _, c := range e.children {
		child, ok := c.(*element)
		switch {
		case !ok:
			kept.children = append(kept.children, c)
		case !slices.Contains(tags, child.tag):
			kept.children = append(kept.children, withoutTags(child, tags))
		}
	}
	return kept
}

func TestPeople_AnEndDatePostedUnchangedIsNotWritten(t *testing.T) {
	f := peopleGarden(t)
	woken := 0
	f.handler.wake = countingWake(&woken)
	shown := memberNamed(t, f.page(t, f.handler.show, PeoplePath), "Jo").until
	if shown == "" {
		t.Fatal("Jo's row has no access-ends date field")
	}

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"until.jo": {shown}})

	var ends time.Time
	if err := f.tx.QueryRow(t.Context(), "SELECT expires_at FROM membership WHERE user_id = $1", peopleJoID).Scan(&ends); err != nil {
		t.Fatalf("reading Jo's end date: %v", err)
	}
	if !ends.Equal(thursday.AddDate(0, 0, 8)) {
		t.Errorf("Jo's access ends at %s after posting the date shown, want it left at %s", ends, thursday.AddDate(0, 0, 8))
	}
	if woken != 0 {
		t.Errorf("an unchanged end date woke the jobs %d times, want 0", woken)
	}
}

func TestPeople_ARemoveSentAsASwapGetsThePageUnderTheBarWithoutThatMember(t *testing.T) {
	f := peopleGarden(t)

	rec := f.swap(t, f.handler.removeMember, removeMemberPath("sam"), peopleID, "member", "sam", url.Values{})

	body := fragment(t, rec, peopleID)
	page := textWithoutAnnouncement(body)
	if strings.Contains(page, "Sam") {
		t.Errorf("Sam is still on the page after the remove:\n%s", page)
	}
	if readHTML(body).first(isTag("a"), attrIs("href", removeMemberPath("sam"))) != nil {
		t.Errorf("Sam's Remove is still on the page after the remove:\n%s", text(body))
	}
}

func TestPeople_ARevokeSentAsASwapGetsThePageUnderTheBarWithoutThatInvite(t *testing.T) {
	f := peopleGarden(t)

	rec := f.swap(t, f.handler.revokeInvite, revokeInvitePath(pendingInviteID), peopleID, "invite", pendingInviteID.String(), url.Values{})

	body := fragment(t, rec, peopleID)
	if formTo(body, revokeInvitePath(pendingInviteID)) != nil {
		t.Errorf("the revoked invite is still listed:\n%s", text(body))
	}
	if formTo(body, revokeInvitePath(peopleExpiredID)) == nil {
		t.Errorf("the invite that ran out is no longer listed:\n%s", text(body))
	}
}

func TestPeople_AMemberWhoseRoleChangedIsToldTheNewRole(t *testing.T) {
	f := peopleGarden(t)
	got := captureUserNotifications(&f.handler.notify)

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"role.sam": {"sitter"}, "role.jo": {"sitter"}})

	// Jo is a sitter already, so their row changed nothing and they are not
	// told. Sam is not the owner, so the garden is named after Ellie.
	if len(*got) != 1 || (*got)[0].user.ID != otherUserID || (*got)[0].n != roleChangedNotification("Ellie’s Rosewood", "sitter") {
		t.Errorf("notified %+v, want Sam alone, told he is now a sitter in Ellie’s Rosewood", *got)
	}
}

func TestPeople_ANewEndDateWakesTheJobsAndARoleChangeDoesNot(t *testing.T) {
	f := peopleGarden(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"role.sam": {"sitter"}})
	if woken != 0 {
		t.Errorf("a role change woke the jobs %d times, want 0: no instant moved", woken)
	}

	f.do(t, f.handler.saveMembers, PeoplePath, url.Values{"until.jo": {"2026-10-01"}})
	if woken != 1 {
		t.Errorf("an end date woke the jobs %d times, want 1", woken)
	}
}

func TestPeople_ARemovedMemberIsToldAndTheNotificationOpensNothing(t *testing.T) {
	f := peopleGarden(t)
	got := captureUserNotifications(&f.handler.notify)

	f.member(t, http.MethodPost, f.handler.removeMember, "sam", removeMemberPath("sam"))

	if len(*got) != 1 || (*got)[0].user.ID != otherUserID || (*got)[0].n != membershipRemovedNotification("Ellie’s Rosewood") {
		t.Errorf("notified %+v, want Sam told he was removed from Ellie’s Rosewood", *got)
	}
}
