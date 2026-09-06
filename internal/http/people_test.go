package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
)

var (
	peopleSamID      = uuid.MustParse("00000000-0000-7000-8000-000000000330")
	peopleJoID       = uuid.MustParse("00000000-0000-7000-8000-000000000331")
	peopleClareID    = uuid.MustParse("00000000-0000-7000-8000-000000000332")
	peopleSecondID   = uuid.MustParse("00000000-0000-7000-8000-000000000333")
	peoplePlantID    = uuid.MustParse("00000000-0000-7000-8000-000000000334")
	peopleWaterID    = uuid.MustParse("00000000-0000-7000-8000-000000000335")
	peopleEventID    = uuid.MustParse("00000000-0000-7000-8000-000000000336")
	peopleWaitingID  = uuid.MustParse("00000000-0000-7000-8000-000000000337")
	peopleExpiredID  = uuid.MustParse("00000000-0000-7000-8000-000000000338")
	peopleRedeemedID = uuid.MustParse("00000000-0000-7000-8000-000000000340")
	peopleFinnID     = uuid.MustParse("00000000-0000-7000-8000-000000000341")
)

// peopleGarden gives Rosewood the four rows the People page has to tell apart:
// Ellie who is reading it, Sam whose membership has no end date, Jo whose
// access ends in eight days, and Clare whose access ended twelve days ago.
// Rosewood's invites are replaced so this page's list is known. Fairview's is
// left where the More fixture put it, so another garden's row is in reach of
// every query the page makes.
func peopleGarden(t *testing.T) *moreFixture {
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
		($1, $2, 'waiting', 'sitter', $3, $4, $5, NULL),
		($6, $2, 'ran-out', 'member', $3, $7, $8, NULL),
		($9, $2, 'redeemed', 'sitter', $3, $4, $5, $4)`,
		peopleWaitingID, moreGardenID, moreUserID, thursday.AddDate(0, 0, -2), thursday.AddDate(0, 0, 5),
		peopleExpiredID, thursday.AddDate(0, 0, -8), thursday.AddDate(0, 0, -1), peopleRedeemedID)
	return f
}

func TestPeople_TheReadersOwnRowHasNoRoleSelectAndNoRemove(t *testing.T) {
	f := peopleGarden(t)

	row := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Ellie")

	if row.role != "" {
		t.Errorf("your own row offers the role %q, and an owner demoting themselves leaves a garden nobody can administer", row.role)
	}
	if row.note != "Owner" {
		t.Errorf("your own row says %q at its right-hand end, want %q", row.note, "Owner")
	}
	if len(row.acts) != 0 {
		t.Errorf("your own row offers %v, and it should offer nothing", row.acts)
	}
	if row.meta != "" {
		t.Errorf("your own row says %q under the name, and there is no choice for it to explain", row.meta)
	}
}

func TestPeople_APermanentMemberHasBothRolesTheirOwnSelectedAndNoEndDate(t *testing.T) {
	f := peopleGarden(t)

	row := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Sam")

	if row.role != "member" {
		t.Errorf("Sam's row has %q selected, want %q", row.role, "member")
	}
	if !slices.Equal(row.roles, []string{"member", "sitter"}) {
		t.Errorf("the select offers %v, want member and sitter", row.roles)
	}
	if row.meta != roleWhat["member"] {
		t.Errorf("the sentence under Sam reads %q, want %q", row.meta, roleWhat["member"])
	}
	if row.until != "" {
		t.Errorf("a permanent membership offers the end date %q; a date arrives with the invite", row.until)
	}
	if !slices.Equal(row.acts, []string{"Re-enrol", "Remove"}) {
		t.Errorf("Sam's row offers %v, want Re-enrol and Remove", row.acts)
	}
}

func TestPeople_ASitterWhoseAccessEndsShowsTheDayAndKeepsTheRoleSelect(t *testing.T) {
	f := peopleGarden(t)

	row := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Jo")

	if want := "Sitter · until 11 Sep"; row.meta != want {
		t.Errorf("Jo's second line reads %q, want %q", row.meta, want)
	}
	if row.until != "2026-09-11" {
		t.Errorf("the end date field holds %q, want %q", row.until, "2026-09-11")
	}
	if row.role != "sitter" {
		t.Errorf("a sitter whose access has not ended has %q selected, want %q", row.role, "sitter")
	}
	if row.off {
		t.Error("Jo's access has not ended, and the row is greyed")
	}
}

func TestPeople_AMembershipThatHasEndedIsGreyKeepsItsPlaceAndHasNoRoleSelect(t *testing.T) {
	f := peopleGarden(t)
	page := f.page(t, f.handler.people, peoplePath)

	row := memberNamed(t, page, "Clare")

	if !row.off {
		t.Error("a membership that has ended is not greyed")
	}
	if row.role != "" {
		t.Errorf("a membership that has ended offers the role %q, and there is no role to choose for access that has ended", row.role)
	}
	if want := "Sitter · Access ended 22 Aug"; row.meta != want {
		t.Errorf("Clare's second line reads %q, want %q", row.meta, want)
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
	if handle := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Sam").handle; handle != "" {
		t.Fatalf("Sam's row shows the handle %q, and no other member is called Sam", handle)
	}

	f.exec(t, "INSERT INTO app_user (id, display_name, handle, timezone) VALUES ($1, 'Ellie', 'ellie2', 'Europe/London')", peopleSecondID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'sitter', 8)", moreGardenID, peopleSecondID)
	page := f.page(t, f.handler.people, peoplePath)

	for _, want := range []string{"ellie", "ellie2"} {
		if !strings.Contains(page, `<span class="row__handle">`+want+`</span>`) {
			t.Errorf("no row shows the handle %q, and two members are called Ellie", want)
		}
	}
	if label := ariaLabels(page)["role.ellie2"]; label != "What Ellie (ellie2) can do" {
		t.Errorf("the select is named %q, and a name alone names both Ellies", label)
	}
}

func TestPeople_TheInvitedSectionIsAbsentWhenNoInviteIsWaiting(t *testing.T) {
	f := peopleGarden(t)

	if got := invitesShown(f.page(t, f.handler.people, peoplePath)); len(got) != 2 {
		t.Fatalf("the Invited section holds %v, want the waiting invite and the one that ran out", got)
	}

	f.exec(t, "DELETE FROM invite WHERE garden_id = $1", moreGardenID)
	page := f.page(t, f.handler.people, peoplePath)

	if strings.Contains(page, "Invited") {
		t.Error("the Invited section is on the page with nothing in it")
	}
}

func TestPeople_AnInviteSaysWhenItWasSentAndWhetherItHasRunOut(t *testing.T) {
	f := peopleGarden(t)

	got := invitesShown(f.page(t, f.handler.people, peoplePath))

	want := []string{"Sitter Sent Tuesday · expires in 5 days", "Member Sent 26 Aug · expired"}
	if !slices.Equal(got, want) {
		t.Errorf("the Invited section reads %v, want %v", got, want)
	}
}

func TestPeople_AnInviteThatRunsOutLaterTodayStillReadsAsWaiting(t *testing.T) {
	f := peopleGarden(t)
	f.exec(t, "DELETE FROM invite WHERE garden_id = $1", moreGardenID)
	f.exec(t, `INSERT INTO invite (id, garden_id, token_hash, role, created_by, created_at, expires_at)
		VALUES ($1, $2, 'later-today', 'sitter', $3, $4, $5)`,
		peopleWaitingID, moreGardenID, moreUserID, thursday.AddDate(0, 0, -7), thursday.Add(3*time.Hour))

	got := invitesShown(f.page(t, f.handler.people, peoplePath))

	if want := []string{"Sitter Sent 27 Aug · expires in 1 day"}; !slices.Equal(got, want) {
		t.Errorf("the Invited section reads %v, want %v; the link works until this afternoon", got, want)
	}
	if note := noteOn(t, f.page(t, f.handler.show, morePath), "People"); note != "1 invite waiting" {
		t.Errorf("More says %q, and the row it is counting reads as waiting", note)
	}
}

func TestPeople_ARoleSavedOnAMemberIsTheOneTheirRowShowsAfterwards(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.saveMembers, peoplePath, url.Values{"role.sam": {"sitter"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	row := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Sam")
	if row.role != "sitter" {
		t.Errorf("Sam's row has %q selected after the save, want %q", row.role, "sitter")
	}
	if row.meta != roleWhat["sitter"] {
		t.Errorf("the sentence under Sam reads %q, want %q", row.meta, roleWhat["sitter"])
	}
}

func TestPeople_ARoleTheSelectDoesNotOfferIsRefusedAndTheMemberIsUnchanged(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.saveMembers, peoplePath, url.Values{"role.sam": {"owner"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if role := roleOf(t, f, otherUserID); role != "member" {
		t.Errorf("Sam's role is %q, and the select never offered owner", role)
	}
}

func TestPeople_ARoleTheSelectDoesNotOfferLeavesTheRowsAboveItUnchanged(t *testing.T) {
	f := peopleGarden(t)

	// Sam's row is above Jo's, so Sam is written first if the save writes as
	// it reads.
	rec := f.do(t, f.handler.saveMembers, peoplePath, url.Values{"role.sam": {"sitter"}, "role.jo": {"owner"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if role := roleOf(t, f, otherUserID); role != "member" {
		t.Errorf("Sam's role is %q, and the post that carried it was refused", role)
	}
}

func TestPeople_ARolePostedForTheReadersOwnRowIsIgnored(t *testing.T) {
	f := peopleGarden(t)

	f.do(t, f.handler.saveMembers, peoplePath, url.Values{"role.ellie": {"sitter"}})

	if role := roleOf(t, f, moreUserID); role != "owner" {
		t.Errorf("the reader's role is %q, and their own row has no select to post from", role)
	}
}

func TestPeople_ANewDateOnAMembershipThatEndedGivesTheRowItsRoleSelectBack(t *testing.T) {
	f := peopleGarden(t)

	f.do(t, f.handler.saveMembers, peoplePath, url.Values{"until.clare": {"2026-10-01"}})

	row := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Clare")
	if row.off {
		t.Error("Clare's access runs to October and the row is still greyed")
	}
	if row.role != "sitter" {
		t.Errorf("Clare's row has %q selected, want %q", row.role, "sitter")
	}
	if want := "Sitter · until 1 Oct"; row.meta != want {
		t.Errorf("Clare's second line reads %q, want %q", row.meta, want)
	}
}

func TestPeople_AnEndDatePostedForAPermanentMembershipIsNotWritten(t *testing.T) {
	f := peopleGarden(t)

	f.do(t, f.handler.saveMembers, peoplePath, url.Values{"until.sam": {"2026-10-01"}})

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

	row := memberNamed(t, f.page(t, f.handler.people, peoplePath), "Finn")
	if want := "Sitter · until 13 Sep"; row.meta != want {
		t.Errorf("Finn's second line reads %q, want %q; the day is the one where they are", row.meta, want)
	}
	if row.until != "2026-09-13" {
		t.Errorf("the end date field holds %q, want %q", row.until, "2026-09-13")
	}

	f.do(t, f.handler.saveMembers, peoplePath, url.Values{"until.finn": {"2026-09-20"}})

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
	if row.ask != "Remove Sam?" {
		t.Errorf("the row asks %q, want %q", row.ask, "Remove Sam?")
	}
	if !strings.Contains(row.why, "their name stays on every watering") {
		t.Errorf("the question reads %q, and it has to say what removing them keeps", row.why)
	}
}

func TestPeople_RemovingAMemberEndsTheirSessionsOnThatGardenOnly(t *testing.T) {
	f := peopleGarden(t)
	f.exec(t, `INSERT INTO session (token_hash, user_id, garden_id) VALUES
		('rosewood', $1, $2), ('fairview', $1, $3)`, otherUserID, moreGardenID, otherGardenID)

	rec := f.member(t, http.MethodPost, f.handler.removeMember, "sam", removeMemberPath("sam"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	var live []string
	rows, err := f.tx.Query(t.Context(), "SELECT token_hash FROM session WHERE user_id = $1", otherUserID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			t.Fatal(err)
		}
		live = append(live, hash)
	}
	if !slices.Equal(live, []string{"fairview"}) {
		t.Errorf("Sam's sessions are %v, want only the one on the garden they are still in", live)
	}
}

func TestPeople_RemovingAMemberLeavesTheirCareEventsSignedWithTheirName(t *testing.T) {
	f := peopleGarden(t)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern')", peoplePlantID, moreGardenID)
	f.exec(t, "INSERT INTO care_type (id, garden_id, name, slug) VALUES ($1, $2, 'Water', 'water')", peopleWaterID, moreGardenID)
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
	if role := roleOf(t, f, moreUserID); role != "owner" {
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
	if strings.Contains(f.page(t, f.handler.people, peoplePath), token) {
		t.Error("the link is on the page again on the next request, and it is shown exactly once")
	}
}

func TestPeople_AReenrolmentLinkIsNotCountedAmongTheInvitesWaiting(t *testing.T) {
	f := peopleGarden(t)
	before := invitesShown(f.page(t, f.handler.people, peoplePath))

	f.memberPage(t, http.MethodPost, f.handler.reenrolMember, "sam", reenrolMemberPath("sam"))

	after := invitesShown(f.page(t, f.handler.people, peoplePath))
	if !slices.Equal(before, after) {
		t.Errorf("the Invited section reads %v after a re-enrolment and %v before, and Sam is already a member", after, before)
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

	rec := f.remove(t, f.handler.revokeInvite, "invite", peopleWaitingID, revokeInvitePath(peopleWaitingID))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	got := invitesShown(f.page(t, f.handler.people, peoplePath))
	if want := []string{"Member Sent 26 Aug · expired"}; !slices.Equal(got, want) {
		t.Errorf("the Invited section reads %v, want %v", got, want)
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
func (f *moreFixture) member(t *testing.T, method string, handler http.HandlerFunc, handle, path string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := context.WithValue(t.Context(), principalKey, f.principal)
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	req.SetPathValue("member", handle)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// memberPage calls one of those handlers and fails on any status but 200.
func (f *moreFixture) memberPage(t *testing.T, method string, handler http.HandlerFunc, handle, path string) string {
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

var (
	// A member's row has one of two class combinations, one for the controls
	// and one for "Remove Ellie?". An invite row has neither, so this pattern
	// skips the Invited section.
	memberRowElement = regexp.MustCompile(`(?s)<li class="row row--setting row--stack (row--people[^"]*|row--editing row--asking)">(.*?)</li>`)
	memberName       = regexp.MustCompile(`(?s)<span class="row__name">(.*?)</span>`)
	memberHandle     = regexp.MustCompile(`<span class="row__handle">([^<]*)</span>`)
	memberPart       = regexp.MustCompile(`(?s)<span class="row__part[^"]*"[^>]*>(.*?)</span>`)
	memberDate       = regexp.MustCompile(`<input class="input input--unit" type="date"[^>]*value="([^"]*)"`)
	memberAct        = regexp.MustCompile(`(?s)<(?:button|a) class="row__drop"[^>]*>(.*?)</(?:button|a)>`)
	memberAsk        = regexp.MustCompile(`(?s)<span class="row__ask">(.*?)</span>`)
	memberWhy        = regexp.MustCompile(`(?s)<p class="row__why">(.*?)</p>`)
	memberNote       = regexp.MustCompile(`(?s)<span class="row__note">(.*?)</span>`)
	inviteRowElement = regexp.MustCompile(`(?s)<li class="row row--setting row--stack">(.*?)</li>`)
	secretElement    = regexp.MustCompile(`(?s)<code class="secret__value">(.*?)</code>`)
	ariaLabelled     = regexp.MustCompile(`name="([^"]+)" aria-label="([^"]+)"`)
)

// shownMember is one row of the Members list as the page renders it.
type shownMember struct {
	name string
	// handle is the handle shown beside the name, empty on a row that shows
	// none.
	handle string
	// meta is the second line, its parts joined with the separator the page
	// puts between them.
	meta string
	// note is what the row says at its right-hand end in place of controls.
	note string
	// off is true when the row is greyed.
	off bool
	// role is the option the select has chosen, and roles every option it
	// offers. Both are empty on a row with no select.
	role  string
	roles []string
	// until is what the end date field holds, empty on a row with no field.
	until string
	// acts is the words on the row's own controls, in page order.
	acts []string
	// ask and why are the Remove question, empty on a row that is not asking.
	ask string
	why string
}

// membersShown reads the Members list in page order.
func membersShown(page string) []shownMember {
	var out []shownMember
	for _, m := range memberRowElement.FindAllStringSubmatch(page, -1) {
		row := shownMember{off: strings.Contains(m[1], "row--off")}
		if name := memberName.FindStringSubmatch(m[2]); name != nil {
			row.name = text(strings.SplitN(name[1], "<span", 2)[0])
			if handle := memberHandle.FindStringSubmatch(name[1]); handle != nil {
				row.handle = handle[1]
			}
		}
		var parts []string
		for _, part := range memberPart.FindAllStringSubmatch(m[2], -1) {
			parts = append(parts, text(part[1]))
		}
		row.meta = strings.Join(parts, " · ")
		if note := memberNote.FindStringSubmatch(m[2]); note != nil {
			row.note = text(note[1])
		}
		if selected := optionSelected.FindStringSubmatch(m[2]); selected != nil {
			row.role = selected[1]
		}
		for _, option := range optionElement.FindAllStringSubmatch(m[2], -1) {
			row.roles = append(row.roles, option[1])
		}
		if date := memberDate.FindStringSubmatch(m[2]); date != nil {
			row.until = date[1]
		}
		for _, act := range memberAct.FindAllStringSubmatch(m[2], -1) {
			row.acts = append(row.acts, text(act[1]))
		}
		if ask := memberAsk.FindStringSubmatch(m[2]); ask != nil {
			row.ask = text(ask[1])
		}
		if why := memberWhy.FindStringSubmatch(m[2]); why != nil {
			row.why = text(why[1])
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

// memberAsking returns the one row asking "Remove Ellie?". That row has no
// name, because the question replaces it.
func memberAsking(t *testing.T, page string) shownMember {
	t.Helper()

	for _, row := range membersShown(page) {
		if row.ask != "" {
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

// invitesShown reads the Invited section, one string per row.
func invitesShown(page string) []string {
	invited := strings.SplitN(page, `<h2 class="section__title">Invited</h2>`, 2)
	if len(invited) != 2 {
		return nil
	}
	var out []string
	for _, m := range inviteRowElement.FindAllStringSubmatch(invited[1], -1) {
		row := ""
		if name := memberName.FindStringSubmatch(m[1]); name != nil {
			row = text(name[1])
		}
		var parts []string
		for _, part := range memberPart.FindAllStringSubmatch(m[1], -1) {
			parts = append(parts, text(part[1]))
		}
		out = append(out, row+" "+strings.Join(parts, " · "))
	}
	return out
}

func secretValue(t *testing.T, page string) string {
	t.Helper()

	m := secretElement.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no value in a secret box on:\n%s", page)
	}
	return text(m[1])
}

// ariaLabels returns the accessible name of every named control on the page,
// keyed by the name it posts under.
func ariaLabels(page string) map[string]string {
	out := map[string]string{}
	for _, m := range ariaLabelled.FindAllStringSubmatch(page, -1) {
		out[m[1]] = m[2]
	}
	return out
}
