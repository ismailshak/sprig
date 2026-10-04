package auth

import "testing"

func TestIssueSetupLink_TheRowHoldsTheTokensHashUnusedAndExpiresAfterAWeek(t *testing.T) {
	q, _ := twoAccountsOnTx(t)

	made, err := IssueSetupLink(t.Context(), q, issuedAt)
	if err != nil {
		t.Fatalf("IssueSetupLink: %v", err)
	}

	link := made.Link
	if link.TokenHash != HashToken(made.Token) || link.UsedAt != nil || !link.ExpiresAt.Equal(issuedAt.Add(InviteLifetime)) {
		t.Errorf("the row holds hash %q, used %v, expires %v; want the token's hash, unused, expiring in a week", link.TokenHash, link.UsedAt, link.ExpiresAt)
	}
}
