package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// passkeysOnTx returns Passkeys over the same garden, account and membership
// the session tests seed, inside a transaction rolled back when the test ends.
func passkeysOnTx(t *testing.T) (*Passkeys, pgx.Tx) {
	t.Helper()

	_, tx := sessionsOnTx(t)
	passkeys, err := NewPasskeys(store.New(tx), "localhost", "sprig", "http://localhost:8080", testCookie)
	if err != nil {
		t.Fatalf("building the passkeys: %v", err)
	}
	return passkeys, tx
}

// answering returns a request with the ceremony cookie on it. That cookie is
// what says which challenge the request is answering.
func answering(t *testing.T, cookie *http.Cookie) *http.Request {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/more/passkeys", nil)
	r.AddCookie(cookie)
	return r
}

// notACredential is what the credential field holds when nothing valid was
// posted. FinishRegistration reads it only after the ceremony is found and
// checked, so a test about the ceremony never has to build a real one.
func notACredential() *strings.Reader { return strings.NewReader("{}") }

// testAccount is the row sessionsOnTx seeds.
var testAccount = store.AppUser{ID: testUserID, DisplayName: "Emma", Handle: "emma"}

func TestPasskeys_AChallengeCanOnlyBeAnsweredOnce(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	_, cookie, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}

	// The first answer gets as far as the credential it posted, so the
	// challenge was found. The second no longer finds one.
	_, err = passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), testAccount, notACredential(), "iPhone")
	if !errors.Is(err, ErrBadCredential) {
		t.Fatalf("the first answer to the challenge returned %v, want %v", err, ErrBadCredential)
	}
	_, err = passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), testAccount, notACredential(), "iPhone")
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("the second answer to the challenge returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_AChallengeLeftOpenPastItsLifeIsGone(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	// The row expires ceremonyLife after the ceremony started, so starting one
	// that long ago is the same as nobody answering in time.
	stale := time.Now().Add(-ceremonyLife - time.Minute)
	_, cookie, err := passkeys.BeginRegistration(ctx, stale, testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}

	_, err = passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), testAccount, notACredential(), "iPhone")
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("answering a challenge from %s ago returned %v, want %v", ceremonyLife, err, ErrCeremonyGone)
	}
}

func TestPasskeys_AChallengeIsStillAnswerableASecondBeforeItExpires(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	started := time.Now()
	_, cookie, err := passkeys.BeginRegistration(ctx, started, testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}

	answeredAt := started.Add(ceremonyLife - time.Second)
	_, err = passkeys.FinishRegistration(ctx, answeredAt, answering(t, cookie), testAccount, notACredential(), "iPhone")
	if !errors.Is(err, ErrBadCredential) {
		t.Errorf("answering a challenge a second before it expires returned %v, want %v", err, ErrBadCredential)
	}
}

func TestPasskeys_StartingACeremonyDeletesTheOnesNobodyAnswered(t *testing.T) {
	ctx := t.Context()
	passkeys, tx := passkeysOnTx(t)

	stale := time.Now().Add(-ceremonyLife - time.Minute)
	if _, _, err := passkeys.BeginAssertion(ctx, stale); err != nil {
		t.Fatalf("BeginAssertion returned an error: %v", err)
	}
	if _, _, err := passkeys.BeginAssertion(ctx, time.Now()); err != nil {
		t.Fatalf("BeginAssertion returned an error: %v", err)
	}

	var rows int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM webauthn_ceremony").Scan(&rows); err != nil {
		t.Fatalf("counting the ceremonies: %v", err)
	}
	if rows != 1 {
		t.Errorf("webauthn_ceremony holds %d rows, want 1, so an abandoned prompt does not leave one behind", rows)
	}
}

func TestPasskeys_AnAnswerWithNoCeremonyCookieIsRefused(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/more/passkeys", nil)
	_, err := passkeys.FinishRegistration(ctx, time.Now(), r, testAccount, notACredential(), "iPhone")
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("an answer with no ceremony cookie returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_ARegistrationStartedByAnotherAccountIsRefused(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	// The ceremony cookie outlives a sign-out, so the next person at the same
	// browser must not finish the previous one and enrol their device on that
	// account.
	_, cookie, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}

	_, err = passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), store.AppUser{ID: otherUserID}, notACredential(), "iPhone")
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("a registration finished by a second account returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_ASignInChallengeNamesNoCredential(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	// A discoverable credential is what lets the sign-in page have no username
	// field: the browser offers the passkeys it holds rather than being told
	// which ones to accept.
	assertion, _, err := passkeys.BeginAssertion(ctx, time.Now())
	if err != nil {
		t.Fatalf("BeginAssertion returned an error: %v", err)
	}
	if got := assertion.Response.AllowedCredentials; len(got) != 0 {
		t.Errorf("the challenge allows %d named credentials, want none", len(got))
	}
}

func TestPasskeys_ARegistrationChallengeNamesTheAccountAddingTheDevice(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	creation, _, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}

	handle, ok := creation.Response.User.ID.(protocol.URLEncodedBase64)
	if !ok {
		t.Fatalf("the challenge names the account as %T, want the raw user handle", creation.Response.User.ID)
	}
	if want := testUserID[:]; string(handle) != string(want) {
		t.Errorf("the challenge names account %x, want %x", handle, want)
	}
	if creation.Response.User.Name != "emma" {
		t.Errorf("the challenge calls the account %q, want %q", creation.Response.User.Name, "emma")
	}
}

// aNewAccount is an account Set up your garden is about to create: an id, a
// name and a zone, with no row in the database yet.
func aNewAccount() store.AppUser {
	return store.AppUser{ID: uuid.NewV7(), DisplayName: "Robin", Handle: "robin", Timezone: "Europe/London"}
}

func TestPasskeys_ASetupChallengeNamesTheAccountIdAndHandleAndItsCeremonyRowNamesNoAccount(t *testing.T) {
	ctx := t.Context()
	passkeys, tx := passkeysOnTx(t)
	account := aNewAccount()

	creation, cookie, err := passkeys.BeginSetup(ctx, time.Now(), account)
	if err != nil {
		t.Fatalf("BeginSetup returned an error: %v", err)
	}

	handle, ok := creation.Response.User.ID.(protocol.URLEncodedBase64)
	if !ok {
		t.Fatalf("the challenge names the account as %T, want the raw user handle", creation.Response.User.ID)
	}
	if string(handle) != string(account.ID[:]) {
		t.Errorf("the challenge names account %x, want %x", handle, account.ID[:])
	}
	if creation.Response.User.Name != "robin" || creation.Response.User.DisplayName != "Robin" {
		t.Errorf("the challenge calls the account %q and %q, want robin and Robin", creation.Response.User.Name, creation.Response.User.DisplayName)
	}
	// The row cannot name an account that does not exist, so it names none.
	var named int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM webauthn_ceremony WHERE user_id IS NOT NULL").Scan(&named); err != nil {
		t.Fatalf("counting the ceremonies: %v", err)
	}
	if named != 0 {
		t.Errorf("%d ceremony rows name an account, want none", named)
	}

	ceremony, err := passkeys.TakeSetup(ctx, time.Now(), answering(t, cookie))
	if err != nil {
		t.Fatalf("TakeSetup returned an error: %v", err)
	}
	if ceremony.AccountID != account.ID {
		t.Errorf("TakeSetup returned account %s, want %s", ceremony.AccountID, account.ID)
	}
}

func TestPasskeys_TakeSetupRefusesAChallengeThePasskeysPageIssued(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	_, cookie, err := passkeys.BeginRegistration(ctx, time.Now(), testAccount, nil)
	if err != nil {
		t.Fatalf("BeginRegistration returned an error: %v", err)
	}

	_, err = passkeys.TakeSetup(ctx, time.Now(), answering(t, cookie))
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("TakeSetup on a Passkeys page challenge returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_TakeSetupRefusesASignInChallenge(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	// A sign-in ceremony names no account in its row, the same as a setup one.
	// The session data tells them apart: a setup holds the account's id there
	// and a sign-in holds nothing.
	_, cookie, err := passkeys.BeginAssertion(ctx, time.Now())
	if err != nil {
		t.Fatalf("BeginAssertion returned an error: %v", err)
	}

	_, err = passkeys.TakeSetup(ctx, time.Now(), answering(t, cookie))
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("TakeSetup on a sign-in challenge returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_ASetupChallengeDoesNotEnrolADeviceOnAnExistingAccount(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	_, cookie, err := passkeys.BeginSetup(ctx, time.Now(), aNewAccount())
	if err != nil {
		t.Fatalf("BeginSetup returned an error: %v", err)
	}

	_, err = passkeys.FinishRegistration(ctx, time.Now(), answering(t, cookie), testAccount, notACredential(), "iPhone")
	if !errors.Is(err, ErrCeremonyGone) {
		t.Errorf("FinishRegistration on a setup challenge returned %v, want %v", err, ErrCeremonyGone)
	}
}

func TestPasskeys_FinishSetupRefusesARowUnderAnotherId(t *testing.T) {
	ctx := t.Context()
	passkeys, _ := passkeysOnTx(t)

	_, cookie, err := passkeys.BeginSetup(ctx, time.Now(), aNewAccount())
	if err != nil {
		t.Fatalf("BeginSetup returned an error: %v", err)
	}
	ceremony, err := passkeys.TakeSetup(ctx, time.Now(), answering(t, cookie))
	if err != nil {
		t.Fatalf("TakeSetup returned an error: %v", err)
	}

	if _, err := passkeys.FinishSetup(ctx, ceremony, testAccount, notACredential(), "iPhone"); err == nil {
		t.Error("FinishSetup saved a credential for a row with an id the browser was never given")
	}
}

func TestPasskeys_ASetupAnsweredByAPhoneSavesTheCredentialAndThePhoneSignsIn(t *testing.T) {
	ctx := t.Context()
	passkeys, tx := passkeysOnTx(t)
	account := aNewAccount()
	phone := aPhone()

	creation, cookie, err := passkeys.BeginSetup(ctx, time.Now(), account)
	if err != nil {
		t.Fatalf("BeginSetup returned an error: %v", err)
	}
	ceremony, err := passkeys.TakeSetup(ctx, time.Now(), answering(t, cookie))
	if err != nil {
		t.Fatalf("TakeSetup returned an error: %v", err)
	}
	// The row is written between taking the ceremony and finishing it, under
	// the id the browser was given.
	user, err := store.New(tx).CreateAccount(ctx, ceremony.AccountID, account.DisplayName, account.Timezone)
	if err != nil {
		t.Fatalf("writing the account: %v", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO membership (garden_id, user_id, role, digest_hour) VALUES ($1, $2, 'owner', 8)", testGardenID, user.ID); err != nil {
		t.Fatalf("writing the membership: %v", err)
	}

	saved, err := passkeys.FinishSetup(ctx, ceremony, user, strings.NewReader(phone.Register(creation)), "iPhone")
	if err != nil {
		t.Fatalf("FinishSetup returned an error: %v", err)
	}
	if saved.UserID != user.ID {
		t.Errorf("the credential belongs to %s, want %s", saved.UserID, user.ID)
	}

	signedIn, err := signInWith(t, passkeys, phone)
	if err != nil {
		t.Fatalf("signing in with the phone: %v", err)
	}
	if signedIn.ID != user.ID {
		t.Errorf("the phone signed in %s, want %s", signedIn.ID, user.ID)
	}
}
