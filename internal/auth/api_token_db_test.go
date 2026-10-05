package auth

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// apiTokensOnTx returns APITokens over a transaction holding Rosewood and
// Fairview, with one token in each. Both were made ten days before signedInAt
// and stop working thirty days after it.
func apiTokensOnTx(t *testing.T) (tokens *APITokens, tx pgx.Tx, rosewood, fairview string) {
	t.Helper()

	_, tx = resolverOnTx(t, nil)
	rosewood = insertAPIToken(t, tx, testGardenID, testUserID, "The kitchen display")
	fairview = insertAPIToken(t, tx, otherGardenID, otherUserID, "The hallway display")
	return NewAPITokens(store.New(tx)), tx, rosewood, fairview
}

func insertAPIToken(t *testing.T, tx pgx.Tx, gardenID, userID uuid.UUID, name string) string {
	t.Helper()

	token, prefix := NewAPIToken()
	_, err := tx.Exec(t.Context(), `INSERT INTO api_token (garden_id, name, token_hash, prefix, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		gardenID, name, HashToken(token), prefix, userID, signedInAt.AddDate(0, 0, -10), signedInAt.AddDate(0, 0, 30))
	if err != nil {
		t.Fatalf("seeding %s: %v", name, err)
	}
	return token
}

func lastUsed(t *testing.T, tx pgx.Tx, token string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := tx.QueryRow(t.Context(), "SELECT last_used_at FROM api_token WHERE token_hash = $1", HashToken(token)).Scan(&at); err != nil {
		t.Fatalf("reading last_used_at: %v", err)
	}
	return at
}

func TestAPITokens_ALiveTokenResolvesToItsGardenWithNoCapabilities(t *testing.T) {
	tokens, tx, rosewood, _ := apiTokensOnTx(t)

	principal, err := tokens.Resolve(t.Context(), signedInAt, rosewood)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if principal.Garden.Name != "Rosewood" || principal.APIToken == nil || principal.APIToken.Name != "The kitchen display" {
		t.Errorf("resolved to %s with token %+v, want Rosewood with the kitchen display", principal.Garden.Name, principal.APIToken)
	}
	if principal.User != (store.AppUser{}) || principal.Session != (store.Session{}) || principal.Membership.Role != "" {
		t.Errorf("the principal holds a user, session or membership, and a token is none of those: %+v", principal)
	}
	for _, c := range allCapabilities {
		if principal.Can(c) {
			t.Errorf("a token can %s", c)
		}
	}
	if at := lastUsed(t, tx, rosewood); at == nil || !at.Equal(signedInAt) {
		t.Errorf("last_used_at = %v after the request, want %s", at, signedInAt)
	}
}

func TestAPITokens_ATokenResolvesToTheGardenThatIssuedItAndNotTheOtherGarden(t *testing.T) {
	tokens, _, rosewood, fairview := apiTokensOnTx(t)

	first, err := tokens.Resolve(t.Context(), signedInAt, rosewood)
	if err != nil {
		t.Fatalf("Resolve on Rosewood's token: %v", err)
	}
	second, err := tokens.Resolve(t.Context(), signedInAt, fairview)
	if err != nil {
		t.Fatalf("Resolve on Fairview's token: %v", err)
	}

	if first.Garden.ID != testGardenID || first.Garden.Name != "Rosewood" {
		t.Errorf("Rosewood's token resolved to %s, want Rosewood", first.Garden.Name)
	}
	if second.Garden.ID != otherGardenID || second.Garden.Name != "Fairview" {
		t.Errorf("Fairview's token resolved to %s, want Fairview", second.Garden.Name)
	}
}

func TestAPITokens_ATokenStopsAtTheInstantItExpiresWithNoJobHavingRun(t *testing.T) {
	tokens, tx, rosewood, _ := apiTokensOnTx(t)
	expiry := signedInAt.AddDate(0, 0, 30)

	if _, err := tokens.Resolve(t.Context(), expiry.Add(-time.Second), rosewood); err != nil {
		t.Errorf("a second before its expiry the token is refused: %v", err)
	}
	_, err := tokens.Resolve(t.Context(), expiry, rosewood)
	if !errors.Is(err, ErrNoAPIToken) {
		t.Errorf("at its expiry the token resolved with %v, want ErrNoAPIToken", err)
	}
	if at := lastUsed(t, tx, rosewood); at == nil || !at.Equal(expiry.Add(-time.Second)) {
		t.Errorf("last_used_at = %v, and a refused request records no use", at)
	}
	var revoked *time.Time
	if err := tx.QueryRow(t.Context(), "SELECT revoked_at FROM api_token WHERE token_hash = $1", HashToken(rosewood)).Scan(&revoked); err != nil {
		t.Fatalf("the row is gone, and an expired token stays until somebody removes it: %v", err)
	}
	if revoked != nil {
		t.Error("expiring set revoked_at, and only Revoke and Remove do that")
	}
}

func TestAPITokens_ARevokedTokenIsRefused(t *testing.T) {
	tokens, tx, rosewood, _ := apiTokensOnTx(t)
	if _, err := tx.Exec(t.Context(), "UPDATE api_token SET revoked_at = $1 WHERE token_hash = $2", signedInAt, HashToken(rosewood)); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	_, err := tokens.Resolve(t.Context(), signedInAt.Add(time.Minute), rosewood)

	if !errors.Is(err, ErrNoAPIToken) {
		t.Errorf("Resolve = %v, want ErrNoAPIToken", err)
	}
}

// The table stores the hash and never the token, so a copy of the table leaks
// hashes. Presenting one has to fail like any unknown string.
func TestAPITokens_TheStoredHashIsRefusedLikeAnyUnknownString(t *testing.T) {
	tokens, _, rosewood, _ := apiTokensOnTx(t)

	for _, presented := range []string{HashToken(rosewood), "sprg_0000_" + rosewood[len(rosewood)-32:], ""} {
		if _, err := tokens.Resolve(t.Context(), signedInAt, presented); !errors.Is(err, ErrNoAPIToken) {
			t.Errorf("Resolve(%q) = %v, want ErrNoAPIToken", presented, err)
		}
	}
}

func TestAPITokens_ATokenIsRefusedOnceItsCreatorIsRemovedFromTheGarden(t *testing.T) {
	tokens, tx, rosewood, _ := apiTokensOnTx(t)
	if _, err := tx.Exec(t.Context(), "DELETE FROM membership WHERE garden_id = $1 AND user_id = $2", testGardenID, testUserID); err != nil {
		t.Fatalf("removing the creator: %v", err)
	}

	_, err := tokens.Resolve(t.Context(), signedInAt, rosewood)

	if !errors.Is(err, ErrNoAPIToken) {
		t.Errorf("Resolve = %v, want ErrNoAPIToken", err)
	}
	if at := lastUsed(t, tx, rosewood); at != nil {
		t.Errorf("last_used_at = %v, and a refused request records no use", at)
	}
}

func TestAPITokens_ATokenStopsAtTheInstantItsCreatorsMembershipEnds(t *testing.T) {
	tokens, tx, rosewood, _ := apiTokensOnTx(t)
	endsAt := signedInAt.Add(time.Hour)
	if _, err := tx.Exec(t.Context(), "UPDATE membership SET expires_at = $1 WHERE garden_id = $2 AND user_id = $3", endsAt, testGardenID, testUserID); err != nil {
		t.Fatalf("setting the creator's end date: %v", err)
	}

	if _, err := tokens.Resolve(t.Context(), endsAt.Add(-time.Second), rosewood); err != nil {
		t.Errorf("a second before the creator's membership ends the token is refused: %v", err)
	}
	if _, err := tokens.Resolve(t.Context(), endsAt, rosewood); !errors.Is(err, ErrNoAPIToken) {
		t.Errorf("when the creator's membership ends Resolve = %v, want ErrNoAPIToken", err)
	}
}

func TestAPITokens_ATokenIsRefusedWhenItsCreatorsRoleLacksTokenManage(t *testing.T) {
	cases := []struct {
		role string
		live bool
	}{
		{"member", true},
		{"sitter", false},
	}
	for _, c := range cases {
		t.Run(c.role, func(t *testing.T) {
			tokens, tx, rosewood, _ := apiTokensOnTx(t)
			if _, err := tx.Exec(t.Context(), "UPDATE membership SET role = $1 WHERE garden_id = $2 AND user_id = $3", c.role, testGardenID, testUserID); err != nil {
				t.Fatalf("changing the creator's role: %v", err)
			}

			_, err := tokens.Resolve(t.Context(), signedInAt, rosewood)

			if c.live && err != nil {
				t.Errorf("with the creator a %s Resolve = %v, want the token to work", c.role, err)
			}
			if !c.live && !errors.Is(err, ErrNoAPIToken) {
				t.Errorf("with the creator a %s Resolve = %v, want ErrNoAPIToken", c.role, err)
			}
		})
	}
}

// Noor owns Fairview and is a sitter in Rosewood. Owning Fairview grants token.manage
// there and nowhere else.
func TestAPITokens_ACreatorsRoleInAnotherGardenDoesNotKeepTheTokenWorking(t *testing.T) {
	tokens, tx, _, _ := apiTokensOnTx(t)
	porch := insertAPIToken(t, tx, testGardenID, otherUserID, "The porch display")

	_, err := tokens.Resolve(t.Context(), signedInAt, porch)

	if !errors.Is(err, ErrNoAPIToken) {
		t.Errorf("Resolve = %v, want ErrNoAPIToken", err)
	}
}
