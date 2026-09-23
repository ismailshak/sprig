package http

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
	"uuid"
)

var (
	thirdGardenID = uuid.MustParse("00000000-0000-7000-8000-000000000312")
	// elliesSession is the token hash of the session row the switch tests read
	// back. The fixture's principal has no session on it, so a test that posts
	// a switch calls startSession first.
	elliesSession = "ellies-session"
	// elliesOtherSession is the token hash of a second session on Ellie's
	// account, for a second device.
	elliesOtherSession = "ellies-other-session"
	// fairviewEnds is when Ellie's membership of Fairview ends. It is four days
	// after the fixture's clock, so the membership is live and the switch is
	// allowed.
	fairviewEnds = time.Date(2026, time.September, 7, 23, 30, 0, 0, time.UTC)
)

// joinFairview makes Ellie a sitter in Fairview until fairviewEnds, so she has
// two live memberships.
func (f *moreFixture) joinFairview(t *testing.T) {
	t.Helper()
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour, expires_at) VALUES ($1, $2, 'sitter', 8, $3)",
		otherGardenID, moreUserID, fairviewEnds)
}

// leaveThirdGarden gives Ellie a membership of a third garden, Allotment, that
// ended the day before the fixture's clock.
func (f *moreFixture) leaveThirdGarden(t *testing.T) {
	t.Helper()
	insertGarden(t, f.tx, thirdGardenID, "Allotment")
	f.exec(t, "INSERT INTO membership (garden_id, user_id, role, digest_hour, expires_at) VALUES ($1, $2, 'member', 8, $3)",
		thirdGardenID, moreUserID, thursday.AddDate(0, 0, -1))
}

// startSession inserts a session row for Ellie on Rosewood and sets its token
// hash on the fixture's principal. Authenticating a real request does the
// same.
func (f *moreFixture) startSession(t *testing.T) {
	t.Helper()
	f.exec(t, "INSERT INTO session (token_hash, user_id, garden_id) VALUES ($1, $2, $3)", elliesSession, moreUserID, moreGardenID)
	f.principal.Session.TokenHash = elliesSession
}

func (f *moreFixture) sessionGarden(t *testing.T, tokenHash string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT garden_id FROM session WHERE token_hash = $1", tokenHash).Scan(&id); err != nil {
		t.Fatalf("reading the session's garden: %v", err)
	}
	return id
}

func (f *moreFixture) switchTo(t *testing.T, garden string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, f.todayHandler.switchGarden, gardensPath, url.Values{gardenField: {garden}})
}

func TestGardens_SwitchingMovesTheSessionToThatGardenAndLandsOnToday(t *testing.T) {
	f := moreGarden(t)
	f.joinFairview(t)
	f.startSession(t)

	rec := f.switchTo(t, otherGardenID.String())

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("got %d to %q, want %d to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	if got := f.sessionGarden(t, elliesSession); got != otherGardenID {
		t.Errorf("the session is on %s, want Fairview", got)
	}
	var last *uuid.UUID
	if err := f.tx.QueryRow(t.Context(), "SELECT last_garden_id FROM app_user WHERE id = $1", moreUserID).Scan(&last); err != nil {
		t.Fatalf("reading the account's last garden: %v", err)
	}
	if last == nil || *last != otherGardenID {
		t.Errorf("the account's last garden is %v, want Fairview", last)
	}
}

func TestGardens_SwitchingLeavesTheAccountsOtherSessionsWhereTheyWere(t *testing.T) {
	f := moreGarden(t)
	f.joinFairview(t)
	f.startSession(t)
	f.exec(t, "INSERT INTO session (token_hash, user_id, garden_id) VALUES ($1, $2, $3)", elliesOtherSession, moreUserID, moreGardenID)

	f.switchTo(t, otherGardenID.String())

	if got := f.sessionGarden(t, elliesSession); got != otherGardenID {
		t.Fatalf("the session the switch was made from is on %s, want Fairview", got)
	}
	if got := f.sessionGarden(t, elliesOtherSession); got != moreGardenID {
		t.Errorf("the other session is on %s, want Rosewood", got)
	}
}

func TestGardens_SwitchingToTheGardenAlreadyOnLandsOnTodayAndLeavesTheSessionThere(t *testing.T) {
	f := moreGarden(t)
	f.startSession(t)

	rec := f.switchTo(t, moreGardenID.String())

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("got %d to %q, want %d to /", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	if got := f.sessionGarden(t, elliesSession); got != moreGardenID {
		t.Errorf("the session is on %s, want Rosewood", got)
	}
}

func TestGardens_AGardenTheAccountIsNotInIs404AndTheSessionStays(t *testing.T) {
	f := moreGarden(t)
	f.startSession(t)

	rec := f.switchTo(t, otherGardenID.String())

	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := f.sessionGarden(t, elliesSession); got != moreGardenID {
		t.Errorf("the session is on %s, want Rosewood", got)
	}
}

func TestGardens_AGardenWhoseMembershipHasEndedIs404(t *testing.T) {
	f := moreGarden(t)
	f.leaveThirdGarden(t)
	f.startSession(t)

	rec := f.switchTo(t, thirdGardenID.String())

	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := f.sessionGarden(t, elliesSession); got != moreGardenID {
		t.Errorf("the session is on %s, want Rosewood", got)
	}
}

func TestGardens_AValueThatIsNotAnIdIs404(t *testing.T) {
	f := moreGarden(t)
	f.startSession(t)

	rec := f.switchTo(t, "rosewood")

	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", rec.Code, http.StatusNotFound)
	}
}
