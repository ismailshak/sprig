package store

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// seedPasskey inserts one credential for the seeded account and returns the
// queries to read it back with.
func seedPasskey(t *testing.T, credentialID string) (*Queries, pgx.Tx) {
	t.Helper()

	tx := sharedTx(t)
	seedGardenAndUser(t, tx)
	insert := "INSERT INTO passkey_credential (user_id, credential_id, name, public_key) VALUES ($1, $2, 'iPhone', '\\x01')"
	if _, err := tx.Exec(t.Context(), insert, testUserID, credentialID); err != nil {
		t.Fatalf("seeding the passkey: %v", err)
	}
	return New(tx), tx
}

func TestGetPasskeyByCredentialID_ReturnsTheCredentialAndTheAccountItBelongsTo(t *testing.T) {
	queries, _ := seedPasskey(t, "aaaa")

	got, err := queries.GetPasskeyByCredentialID(t.Context(), "aaaa")
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID: %v", err)
	}
	if got.PasskeyCredential.Name != "iPhone" {
		t.Errorf("the credential is named %q, want %q", got.PasskeyCredential.Name, "iPhone")
	}
	if got.AppUser.ID != testUserID || got.AppUser.Handle != "emma" {
		t.Errorf("the credential belongs to %v (%q), want %v (%q)", got.AppUser.ID, got.AppUser.Handle, testUserID, "emma")
	}
}

func TestGetPasskeyByCredentialID_ACredentialNoAccountHasReturnsNoRow(t *testing.T) {
	queries, _ := seedPasskey(t, "aaaa")

	_, err := queries.GetPasskeyByCredentialID(t.Context(), "bbbb")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetPasskeyByCredentialID for an unknown credential returned %v, want %v", err, pgx.ErrNoRows)
	}
}

func TestRecordPasskeyUse_WritesTheCounterAndTheDateTheDeviceWasLastUsed(t *testing.T) {
	queries, _ := seedPasskey(t, "aaaa")

	before, err := queries.GetPasskeyByCredentialID(t.Context(), "aaaa")
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID: %v", err)
	}
	if before.PasskeyCredential.LastUsedAt != nil {
		t.Fatalf("the seeded credential was last used at %v, want never", before.PasskeyCredential.LastUsedAt)
	}

	used := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	rows, err := queries.RecordPasskeyUse(t.Context(), RecordPasskeyUseParams{
		PasskeyID: before.PasskeyCredential.ID,
		SignCount: 7,
		UsedAt:    &used,
	})
	if err != nil {
		t.Fatalf("RecordPasskeyUse: %v", err)
	}
	if rows != 1 {
		t.Fatalf("RecordPasskeyUse updated %d rows, want 1", rows)
	}

	after, err := queries.GetPasskeyByCredentialID(t.Context(), "aaaa")
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID: %v", err)
	}
	if after.PasskeyCredential.SignCount != 7 {
		t.Errorf("the stored counter is %d, want 7", after.PasskeyCredential.SignCount)
	}
	if after.PasskeyCredential.LastUsedAt == nil || !after.PasskeyCredential.LastUsedAt.Equal(used) {
		t.Errorf("the credential was last used at %v, want %v", after.PasskeyCredential.LastUsedAt, used)
	}
}

func TestRecordPasskeyUse_ACounterThatDoesNotAdvanceUpdatesNoRow(t *testing.T) {
	queries, _ := seedPasskey(t, "aaaa")
	row, err := queries.GetPasskeyByCredentialID(t.Context(), "aaaa")
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID: %v", err)
	}
	used := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)

	// Each step writes against the counter the step before left. The row
	// starts at zero.
	steps := []struct {
		name  string
		count int64
		rows  int64
	}{
		{"zero on a row at zero, which is a device with no counter", 0, 1},
		{"zero again on the same device", 0, 1},
		{"a counter that advanced from zero", 7, 1},
		{"the same counter again", 7, 0},
		{"a counter that went backwards", 3, 0},
		{"zero after a counter", 0, 0},
		{"a counter that advanced", 8, 1},
	}
	for _, step := range steps {
		rows, err := queries.RecordPasskeyUse(t.Context(), RecordPasskeyUseParams{
			PasskeyID: row.PasskeyCredential.ID,
			SignCount: step.count,
			UsedAt:    &used,
		})
		if err != nil {
			t.Fatalf("%s: RecordPasskeyUse: %v", step.name, err)
		}
		if rows != step.rows {
			t.Errorf("%s: updated %d rows, want %d", step.name, rows, step.rows)
		}
	}

	after, err := queries.GetPasskeyByCredentialID(t.Context(), "aaaa")
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID: %v", err)
	}
	if after.PasskeyCredential.SignCount != 8 {
		t.Errorf("the stored counter is %d, want 8", after.PasskeyCredential.SignCount)
	}
}

func TestCreatePasskey_KeepsTheAuthenticatorFlagsTheRegistrationReturned(t *testing.T) {
	// go-webauthn compares the backup-eligible flag on every assertion against
	// the one stored, so a synced passkey saved with zero flags can never sign
	// in again.
	tx := sharedTx(t)
	seedGardenAndUser(t, tx)
	queries := New(tx)

	const backupEligibleAndBackedUp = 0b0001_1000
	saved, err := queries.CreatePasskey(t.Context(), CreatePasskeyParams{
		UserID:       testUserID,
		CredentialID: "aaaa",
		Name:         "iPhone",
		PublicKey:    []byte{1},
		SignCount:    0,
		Flags:        backupEligibleAndBackedUp,
		Transports:   []string{"internal", "hybrid"},
	})
	if err != nil {
		t.Fatalf("CreatePasskey: %v", err)
	}
	if saved.Flags != backupEligibleAndBackedUp {
		t.Errorf("the saved flags are %08b, want %08b", saved.Flags, backupEligibleAndBackedUp)
	}

	read, err := queries.GetPasskeyByCredentialID(t.Context(), "aaaa")
	if err != nil {
		t.Fatalf("GetPasskeyByCredentialID: %v", err)
	}
	if read.PasskeyCredential.Flags != backupEligibleAndBackedUp {
		t.Errorf("the flags read back are %08b, want %08b", read.PasskeyCredential.Flags, backupEligibleAndBackedUp)
	}
}
