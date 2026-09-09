package auth

import (
	"errors"
	"testing"
	"time"
)

var issuedAt = signedInAt

func TestIssueReenrolment_TheLinkIsForTheAccountOnTheGardenAndRoleItIsAMemberOf(t *testing.T) {
	q, _ := twoAccountsOnTx(t)

	made, err := IssueReenrolment(t.Context(), q, issuedAt, "emma")
	if err != nil {
		t.Fatalf("IssueReenrolment: %v", err)
	}

	invite := made.Invite
	if invite.UserID == nil || *invite.UserID != testUserID || invite.CreatedBy != testUserID {
		t.Errorf("the invite is for %v by %v, want Emma's account on both", invite.UserID, invite.CreatedBy)
	}
	if invite.GardenID != testGardenID || invite.Role != "owner" {
		t.Errorf("the invite is on %v as %s, want Rosewood as owner, the membership Emma has", invite.GardenID, invite.Role)
	}
	if invite.TokenHash != HashToken(made.Token) || invite.RedeemedAt != nil || !invite.ExpiresAt.Equal(issuedAt.Add(InviteLifetime)) {
		t.Errorf("the row holds hash %q, redeemed %v, expires %v; want the token's hash, unredeemed, expiring in a week", invite.TokenHash, invite.RedeemedAt, invite.ExpiresAt)
	}
	if made.User.ID != testUserID {
		t.Errorf("the link is reported as for %v, want Emma", made.User.ID)
	}
}

func TestIssueReenrolment_IssuingWritesNoAccountAndNoMembership(t *testing.T) {
	q, tx := twoAccountsOnTx(t)
	ctx := t.Context()
	var users, memberships int
	count := func() {
		t.Helper()
		if err := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM app_user), (SELECT count(*) FROM membership)").Scan(&users, &memberships); err != nil {
			t.Fatalf("counting: %v", err)
		}
	}
	count()
	usersBefore, membershipsBefore := users, memberships

	if _, err := IssueReenrolment(ctx, q, issuedAt, "emma"); err != nil {
		t.Fatalf("IssueReenrolment: %v", err)
	}

	count()
	if users != usersBefore || memberships != membershipsBefore {
		t.Errorf("issuing wrote %d accounts and %d memberships", users-usersBefore, memberships-membershipsBefore)
	}
}

func TestIssueReenrolment_TheLinkIsOnTheGardenTheAccountLastUsed(t *testing.T) {
	q, tx := twoAccountsOnTx(t)
	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v\n%s", err, sql)
		}
	}
	// Emma is a member of both gardens and last switched to Fairview. Rosewood
	// is the older membership. Only last_garden_id puts the link on Fairview.
	exec("INSERT INTO garden (id, name) VALUES ($1, 'Fairview')", otherGardenID)
	exec("INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'member', 8)", otherGardenID, testUserID)
	exec("UPDATE app_user SET last_garden_id = $1 WHERE id = $2", otherGardenID, testUserID)

	made, err := IssueReenrolment(ctx, q, issuedAt, "emma")
	if err != nil {
		t.Fatalf("IssueReenrolment: %v", err)
	}

	if made.Invite.GardenID != otherGardenID || made.Invite.Role != "member" {
		t.Errorf("the invite is on %v as %s, want Fairview as member, the garden Emma last used", made.Invite.GardenID, made.Invite.Role)
	}
}

func TestIssueReenrolment_AnUnknownHandleIsRefusedAndNothingIsWritten(t *testing.T) {
	q, tx := twoAccountsOnTx(t)

	_, err := IssueReenrolment(t.Context(), q, issuedAt, "nobody")

	if !errors.Is(err, ErrUnknownHandle) {
		t.Errorf("err = %v, want ErrUnknownHandle", err)
	}
	if left := hashes(t, tx, "invite", "token_hash"); len(left) != 0 {
		t.Errorf("the invites on the table are %v, want none", left)
	}
}

func TestIssueReenrolment_AnAccountWhoseEveryMembershipEndedIsRefused(t *testing.T) {
	q, _ := twoAccountsOnTx(t)

	_, err := IssueReenrolment(t.Context(), q, issuedAt, "sam")

	if !errors.Is(err, ErrNoLiveMembership) {
		t.Errorf("err = %v, want ErrNoLiveMembership: the link would be refused when opened", err)
	}
}

func TestIssueReenrolment_ASecondLinkReplacesTheFirst(t *testing.T) {
	q, tx := twoAccountsOnTx(t)
	ctx := t.Context()
	if _, err := IssueReenrolment(ctx, q, issuedAt, "emma"); err != nil {
		t.Fatalf("the first link: %v", err)
	}

	second, err := IssueReenrolment(ctx, q, issuedAt.Add(time.Minute), "emma")
	if err != nil {
		t.Fatalf("the second link: %v", err)
	}

	left := hashes(t, tx, "invite", "token_hash")
	if len(left) != 1 || left[0] != second.Invite.TokenHash {
		t.Errorf("the invites on the table are %v, want only the second link", left)
	}
}
