package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth/passkeytest"
	"github.com/ismailshak/sprig/internal/store"
)

// These tests run whole ceremonies, registration then sign-in, with a passkey
// in software answering the challenges.

const testOrigin = "http://localhost:8080"

// aPhone is a passkey on a device with a screen lock, synced by the platform:
// verified, stored, backup eligible, no counter.
func aPhone() *passkeytest.Authenticator {
	device := passkeytest.New("localhost", testOrigin)
	device.BackupEligible = true
	return device
}

// aHardwareKey is a passkey on a security key with a PIN: verified, stored,
// not backed up anywhere, with a counter that advances on every use.
func aHardwareKey() *passkeytest.Authenticator {
	device := passkeytest.New("localhost", testOrigin)
	device.Counter = 1
	return device
}

// enrol registers device on the seeded account, and fails the test if the
// registration is refused.
func enrol(t *testing.T, passkeys *Passkeys, device *passkeytest.Authenticator) store.PasskeyCredential {
	t.Helper()

	saved, err := register(t, passkeys, device)
	if err != nil {
		t.Fatalf("registering the device: %v", err)
	}
	return saved
}

// register runs one registration ceremony with device answering.
func register(t *testing.T, passkeys *Passkeys, device *passkeytest.Authenticator) (store.PasskeyCredential, error) {
	t.Helper()

	ctx := t.Context()
	creation, cookie, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}
	answer := strings.NewReader(device.Register(creation))
	return passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), testAccount, answer, "iPhone")
}

// signInWith runs one sign-in ceremony with device answering.
func signInWith(t *testing.T, passkeys *Passkeys, device *passkeytest.Authenticator) (store.AppUser, error) {
	t.Helper()

	ctx := t.Context()
	assertion, cookie, err := passkeys.BeginAssertion(ctx, time.Now())
	if err != nil {
		t.Fatalf("BeginAssertion returned an error: %v", err)
	}
	answer := strings.NewReader(device.Assert(assertion))
	return passkeys.FinishAssertion(ctx, time.Now(), answering(t, cookie), answer)
}

// mustSignIn is signInWith for a sign-in the test expects to be accepted.
func mustSignIn(t *testing.T, passkeys *Passkeys, device *passkeytest.Authenticator) {
	t.Helper()

	user, err := signInWith(t, passkeys, device)
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	if user.ID != testUserID {
		t.Fatalf("the sign-in proved account %v, want %v", user.ID, testUserID)
	}
}

// passkeyRow reads the counter and the last-used date of the one credential
// the seeded account has.
func passkeyRow(t *testing.T, tx pgx.Tx) (signCount int64, lastUsedAt *time.Time) {
	t.Helper()

	err := tx.QueryRow(t.Context(), "SELECT sign_count, last_used_at FROM passkey_credential WHERE user_id = $1", testUserID).Scan(&signCount, &lastUsedAt)
	if err != nil {
		t.Fatalf("reading the passkey row: %v", err)
	}
	return signCount, lastUsedAt
}

func TestPasskeys_ASyncedPasskeySignsInAndTheRowRecordsTheUse(t *testing.T) {
	passkeys, tx := passkeysOnTx(t)
	phone := aPhone()

	saved := enrol(t, passkeys, phone)
	if saved.CredentialID != phone.CredentialID() {
		t.Errorf("the row holds credential %q, want %q", saved.CredentialID, phone.CredentialID())
	}
	if _, lastUsed := passkeyRow(t, tx); lastUsed != nil {
		t.Errorf("a passkey that has not signed in was last used at %v, want never", lastUsed)
	}

	// A synced passkey returns a zero counter on every sign-in, so the second
	// one looks exactly like the first and both are accepted.
	mustSignIn(t, passkeys, phone)
	mustSignIn(t, passkeys, phone)

	count, lastUsed := passkeyRow(t, tx)
	if count != 0 {
		t.Errorf("the stored counter is %d, want 0", count)
	}
	if lastUsed == nil {
		t.Error("the passkey signed in and its row says it was never used")
	}
}

func TestPasskeys_ACounterThatDoesNotAdvanceIsRefusedAsACopy(t *testing.T) {
	passkeys, tx := passkeysOnTx(t)
	key := aHardwareKey()
	enrol(t, passkeys, key)

	key.Counter = 5
	mustSignIn(t, passkeys, key)
	if count, _ := passkeyRow(t, tx); count != 5 {
		t.Fatalf("the stored counter is %d, want 5", count)
	}

	cases := []struct {
		name    string
		counter uint32
	}{
		{"the same counter again", 5},
		{"a counter that went backwards", 3},
		{"a key that stopped returning a counter", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key.Counter = c.counter
			_, err := signInWith(t, passkeys, key)
			if !errors.Is(err, ErrClonedCredential) {
				t.Errorf("a sign-in returning counter %d after 5 returned %v, want %v", c.counter, err, ErrClonedCredential)
			}
		})
	}

	// The refusals wrote nothing, so the key itself still signs in.
	key.Counter = 6
	mustSignIn(t, passkeys, key)
}

func TestPasskeys_ACopyOfAHardwareKeyIsRefusedOnceTheOriginalHasBeenUsed(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	key := aHardwareKey()
	enrol(t, passkeys, key)

	// The copy is taken at counter 1. The original then signs in twice and
	// leaves the row at 3.
	copied := key.Clone()
	key.Counter = 2
	mustSignIn(t, passkeys, key)
	key.Counter = 3
	mustSignIn(t, passkeys, key)

	copied.Counter = 2
	_, err := signInWith(t, passkeys, copied)
	if !errors.Is(err, ErrClonedCredential) {
		t.Errorf("the copy signed in with counter 2 after the original reached 3: %v, want %v", err, ErrClonedCredential)
	}
}

func TestPasskeys_ARegistrationWithoutUserVerificationIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	device := aPhone()
	device.Verified = false

	_, err := register(t, passkeys, device)
	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("a registration from a device that did not check who was using it returned %v, want %v", err, ErrNotVerified)
	}
}

func TestPasskeys_ARegistrationTheDeviceWouldNotStoreIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	device := aPhone()
	stores := false
	device.Stores = &stores

	_, err := register(t, passkeys, device)
	if !errors.Is(err, ErrNotDiscoverable) {
		t.Errorf("a registration the device would not store returned %v, want %v", err, ErrNotDiscoverable)
	}
}

func TestPasskeys_ARegistrationFromABrowserThatDoesNotSayWhetherItStoredIsAccepted(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	device := aPhone()
	// The browser did not answer the credProps extension. Only an explicit
	// refusal is one.
	device.Stores = nil

	if _, err := register(t, passkeys, device); err != nil {
		t.Errorf("a registration with no credProps answer returned %v, want it accepted", err)
	}
}

func TestPasskeys_ASignInWithoutUserVerificationIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)

	phone.Verified = false
	_, err := signInWith(t, passkeys, phone)
	if !errors.Is(err, ErrNotVerified) {
		t.Errorf("a sign-in from a device that did not check who was using it returned %v, want %v", err, ErrNotVerified)
	}
}

func TestPasskeys_ASignInNamingAnotherAccountIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)

	// The credential is Emma's and the answer says it is Noor's.
	phone.UserHandle = otherUserID[:]
	_, err := signInWith(t, passkeys, phone)
	if !errors.Is(err, ErrFailedVerification) {
		t.Errorf("a sign-in naming another account returned %v, want %v", err, ErrFailedVerification)
	}
}

func TestPasskeys_AnAnswerFromAnotherOriginIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)

	elsewhere := aPhone()
	elsewhere.Origin = "https://sprig.example"
	if _, err := register(t, passkeys, elsewhere); !errors.Is(err, ErrFailedVerification) {
		t.Errorf("a registration answered from another origin returned %v, want %v", err, ErrFailedVerification)
	}

	phone.Origin = "https://sprig.example"
	if _, err := signInWith(t, passkeys, phone); !errors.Is(err, ErrFailedVerification) {
		t.Errorf("a sign-in answered from another origin returned %v, want %v", err, ErrFailedVerification)
	}
}

func TestPasskeys_ABackupEligibleFlagThatChangedSinceRegistrationIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)
	// A synced passkey sets the flag on every sign-in, and the row has to
	// hold it for those to be accepted at all.
	mustSignIn(t, passkeys, phone)

	phone.BackupEligible = false
	_, err := signInWith(t, passkeys, phone)
	if !errors.Is(err, ErrFailedVerification) {
		t.Errorf("a sign-in whose backup-eligible flag changed returned %v, want %v", err, ErrFailedVerification)
	}
}

func TestPasskeys_ASignInFromAPasskeyNoAccountHasIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	stranger := aPhone()
	stranger.UserHandle = testUserID[:]

	_, err := signInWith(t, passkeys, stranger)
	if !errors.Is(err, ErrUnknownCredential) {
		t.Errorf("a sign-in with a passkey never registered returned %v, want %v", err, ErrUnknownCredential)
	}
}

func TestPasskeys_ASignInAnswerCannotBeReplayed(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)

	assertion, cookie, err := passkeys.BeginAssertion(ctx, time.Now())
	if err != nil {
		t.Fatalf("BeginAssertion returned an error: %v", err)
	}
	answer := phone.Assert(assertion)
	if _, err := passkeys.FinishAssertion(ctx, time.Now(), answering(t, cookie), strings.NewReader(answer)); err != nil {
		t.Fatalf("the first answer was refused: %v", err)
	}

	_, err = passkeys.FinishAssertion(ctx, time.Now(), answering(t, cookie), strings.NewReader(answer))
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("the same answer a second time returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_ACeremonyStartedForOnePurposeDoesNotServeTheOther(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)

	t.Run("a registration challenge answered as a sign-in", func(t *testing.T) {
		creation, cookie, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, nil)
		if err != nil {
			t.Fatalf("BeginRegistration returned an error: %v", err)
		}
		asSignIn := &protocol.CredentialAssertion{Response: protocol.PublicKeyCredentialRequestOptions{Challenge: creation.Response.Challenge}}
		_, err = passkeys.FinishAssertion(ctx, time.Now(), answering(t, cookie), strings.NewReader(phone.Assert(asSignIn)))
		if !errors.Is(err, ErrFailedVerification) {
			t.Errorf("returned %v, want %v", err, ErrFailedVerification)
		}
	})

	t.Run("a sign-in challenge answered as a registration", func(t *testing.T) {
		assertion, cookie, err := passkeys.BeginAssertion(ctx, time.Now())
		if err != nil {
			t.Fatalf("BeginAssertion returned an error: %v", err)
		}
		asRegistration := &protocol.CredentialCreation{Response: protocol.PublicKeyCredentialCreationOptions{
			Challenge: assertion.Response.Challenge,
			User:      protocol.UserEntity{ID: protocol.URLEncodedBase64(testUserID[:])},
		}}
		_, err = passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), testAccount, strings.NewReader(aPhone().Register(asRegistration)), "iPhone")
		if !errors.Is(err, ErrCeremonyGone) {
			t.Errorf("returned %v, want %v", err, ErrCeremonyGone)
		}
	})
}

func TestPasskeys_ADeviceRegisteredASecondTimeIsRefused(t *testing.T) {
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	enrol(t, passkeys, phone)

	// register starts a challenge that excludes nothing, so the device answers
	// again with the same credential id. A browser would have refused the
	// prompt. This is the client that did not.
	_, err := register(t, passkeys, phone)
	if !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("registering the same device again returned %v, want %v", err, ErrAlreadyRegistered)
	}
}

func TestPasskeys_ARegistrationChallengeExcludesTheDevicesAlreadyEnrolled(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)
	phone := aPhone()
	saved := enrol(t, passkeys, phone)

	creation, _, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, []store.PasskeyCredential{saved})
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}
	excluded := creation.Response.CredentialExcludeList
	if len(excluded) != 1 || excluded[0].CredentialID.String() != phone.CredentialID() {
		t.Errorf("the challenge excludes %v, want the one credential %q", excluded, phone.CredentialID())
	}
}
