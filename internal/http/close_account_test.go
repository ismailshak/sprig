package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/store"
)

var closeEventID = uuid.MustParse("00000000-0000-7000-8000-000000000340")

// closableAccount gives Ellie one of everything closing deletes: a care event,
// a recovery code, a session and a re-enrolment invite. It makes Sam a second
// owner of Rosewood, because the close is refused while Ellie is the only
// owner.
func closableAccount(t *testing.T) *moreFixture {
	t.Helper()

	f := moreGarden(t)
	f.handler.sessions = testSessions()
	f.exec(t, "INSERT INTO care_type (garden_id, name, slug) VALUES ($1, 'Water', 'water')", moreGardenID)
	f.exec(t, "INSERT INTO plant (id, garden_id, nickname) VALUES ($1, $2, 'Fern')", morePlantID, moreGardenID)
	f.exec(t, `INSERT INTO care_event (id, garden_id, plant_id, care_type_id, performed_by, performed_at, done)
		SELECT $1, $2, $3, id, $4, now(), true FROM care_type WHERE garden_id = $2`, closeEventID, moreGardenID, morePlantID, moreUserID)
	f.exec(t, "INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, 'code')", moreUserID)
	f.exec(t, "INSERT INTO session (token_hash, user_id, garden_id, created_at, last_seen_at) VALUES ('ellie-phone', $1, $2, now(), now())", moreUserID, moreGardenID)
	f.exec(t, `INSERT INTO invite (garden_id, token_hash, role, created_by, user_id, expires_at)
		VALUES ($1, 'reenrol-ellie', 'owner', $2, $2, now() + interval '7 days')`, moreGardenID, moreUserID)
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", moreGardenID, otherUserID)
	return f
}

func (f *moreFixture) closeAccount(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, f.handler.closeAccount, closeAccountPath, map[string][]string{})
}

func TestCloseAccount_ThePageSaysWhatIsDeletedAndWhatIsKept(t *testing.T) {
	f := closableAccount(t)

	page := f.page(t, f.handler.confirmCloseAccount, closeAccountPath)

	for _, want := range []string{"deletes your passkeys", "Your name stays on everything you’ve logged", ">Close account</button>"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q:\n%s", want, text(page))
		}
	}
}

func TestCloseAccount_TheOnlyOwnerOfAGardenIsToldToDeleteItAndGetsNoButton(t *testing.T) {
	f := moreGarden(t)

	page := f.page(t, f.handler.confirmCloseAccount, closeAccountPath)

	if !strings.Contains(page, "You’re the only owner of Rosewood. Delete it before closing your account") {
		t.Errorf("the page does not name the garden:\n%s", text(page))
	}
	if strings.Contains(page, ">Close account</button>") {
		t.Error("the only owner of a garden is offered the button")
	}
}

func TestCloseAccount_TheOnlyOwnerOfTwoGardensIsToldBoth(t *testing.T) {
	f := moreGarden(t)
	f.exec(t, "INSERT INTO garden (id, name) VALUES ($1, 'Allotment')", uuid.MustParse("00000000-0000-7000-8000-000000000341"))
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", uuid.MustParse("00000000-0000-7000-8000-000000000341"), moreUserID)

	page := f.page(t, f.handler.confirmCloseAccount, closeAccountPath)

	if !strings.Contains(page, "only owner of Allotment and Rosewood. Delete them before") {
		t.Errorf("the page does not name both gardens:\n%s", text(page))
	}
}

func TestCloseAccount_PostingAsTheOnlyOwnerIsRefusedAndNothingIsDeleted(t *testing.T) {
	f := moreGarden(t)
	f.handler.sessions = testSessions()

	rec := f.closeAccount(t)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if n := countRows(t, f.tx, "passkey_credential"); n != 3 {
		t.Errorf("%d passkeys remain, want all 3", n)
	}
	var closed *time.Time
	if err := f.tx.QueryRow(t.Context(), "SELECT closed_at FROM app_user WHERE id = $1", moreUserID).Scan(&closed); err != nil || closed != nil {
		t.Errorf("the account was closed: %v %v", closed, err)
	}
}

// Sam's ownership of Rosewood ended yesterday. Ellie is still the only owner
// who can manage it.
func TestCloseAccount_AnOwnerWhoseMembershipEndedDoesNotCountAsAnotherOwner(t *testing.T) {
	f := closableAccount(t)
	f.exec(t, "UPDATE membership SET expires_at = $3 WHERE garden_id = $1 AND user_id = $2", moreGardenID, otherUserID, thursday.AddDate(0, 0, -1))

	if rec := f.closeAccount(t); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

// Ellie's own ownership of Rosewood ended yesterday. She can no longer reach
// the garden to delete it. A refusal would leave her unable to close the
// account at all.
func TestCloseAccount_AnOwnerWhoseOwnMembershipEndedCanClose(t *testing.T) {
	f := moreGarden(t)
	f.handler.sessions = testSessions()
	f.exec(t, "UPDATE membership SET expires_at = $3 WHERE garden_id = $1 AND user_id = $2", moreGardenID, moreUserID, thursday.AddDate(0, 0, -1))

	if rec := f.closeAccount(t); rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestCloseAccount_ClosingDeletesTheCredentialsSessionsAndMemberships(t *testing.T) {
	f := closableAccount(t)
	woken := 0
	f.handler.wake = countingWake(&woken)

	rec := f.closeAccount(t)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signInPath {
		t.Fatalf("closing returned %d to %q, want %d to sign in", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	for table, want := range map[string]int{
		"passkey_credential": 1, "push_subscription": 1, "recovery_code": 0, "session": 0,
		"membership": 2, "notification_preference": 0,
	} {
		if n := countRows(t, f.tx, table); n != want {
			t.Errorf("%s holds %d rows after the close, want %d, Sam's alone", table, n, want)
		}
	}
	if n, err := store.New(f.tx).CountPendingInvites(t.Context(), moreGardenID, thursday); err != nil || n != 1 {
		t.Errorf("%d invites are pending, want the sitter's alone: %v", n, err)
	}
	var reenrol int
	if err := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM invite WHERE user_id = $1", moreUserID).Scan(&reenrol); err != nil || reenrol != 0 {
		t.Errorf("%d re-enrolment links for the closed account remain, want none: %v", reenrol, err)
	}
	cookie := cookieNamed(t, rec, "__Host-sprig_session")
	if cookie == nil || cookie.MaxAge != -1 {
		t.Errorf("the response set %v, want the session cookie cleared", cookie)
	}
	if woken != 1 {
		t.Errorf("the digest job was woken %d times, want 1", woken)
	}
}

func TestCloseAccount_AClosedAccountsNameStaysOnItsCareEvents(t *testing.T) {
	f := closableAccount(t)

	if rec := f.closeAccount(t); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	queries := store.New(f.tx)
	user, err := queries.GetUser(t.Context(), moreUserID)
	if err != nil {
		t.Fatalf("the account's row is gone: %v", err)
	}
	if user.ClosedAt == nil || user.DisplayName != "Ellie" {
		t.Errorf("the row reads closed at %v as %q, want closed and still Ellie", user.ClosedAt, user.DisplayName)
	}
	events, err := queries.ListPlantCareEvents(t.Context(), store.ListPlantCareEventsParams{GardenID: moreGardenID, PlantID: morePlantID, Count: 5})
	if err != nil || len(events) != 1 || events[0].PerformedByName != "Ellie" {
		t.Errorf("the plant's care events are %v, want the one event still performed by Ellie: %v", events, err)
	}
}

func TestCloseAccount_AnAccountInNoGardenGetsThePageWithoutTheTabBar(t *testing.T) {
	f := closableAccount(t)
	f.principal = auth.Principal{User: f.principal.User}

	page := f.page(t, f.handler.confirmCloseAccount, closeAccountPath)

	if strings.Contains(page, `class="nav"`) {
		t.Error("an account in no garden got the tab bar")
	}
	if !strings.Contains(page, ">Close account</button>") {
		t.Errorf("an account in no garden is not offered the button:\n%s", text(page))
	}
}

func TestAccount_ThePageLinksToCloseAccount(t *testing.T) {
	f := moreGarden(t)

	if page := f.page(t, f.handler.account, accountPath); !strings.Contains(page, `href="`+closeAccountPath+`">Close account`) {
		t.Errorf("the Account page has no Close account link:\n%s", text(page))
	}
}
