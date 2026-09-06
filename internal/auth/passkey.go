package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
	"uuid"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/store"
)

// ceremonyLife is how long a challenge can still be answered after it was
// issued. Five minutes covers somebody interrupted between pressing the button
// and finishing a fingerprint or a PIN. It is also how long a stolen ceremony
// cookie is usable.
const ceremonyLife = 5 * time.Minute

// ErrCeremonyGone is returned when a challenge is answered and no row is left
// for it. The cookie was never issued, the challenge was already answered, or
// the prompt sat open past ceremonyLife.
var ErrCeremonyGone = errors.New("no ceremony in progress")

// ErrNotVerified is returned when a registration or an assertion completed
// without the device checking who was using it. sprig has no password and no
// second factor, so that check is all an account is protected by.
var ErrNotVerified = errors.New("the device did not check who was using it")

// ErrNotDiscoverable is returned when the device would not store the
// credential itself. Sign-in names no credential and has no username field, so
// a credential only the server holds could never be used to sign in.
var ErrNotDiscoverable = errors.New("the device would not store the passkey")

// ErrUnknownCredential is returned when an assertion names a credential no
// account has.
var ErrUnknownCredential = errors.New("no account has that passkey")

// ErrAlreadyRegistered is returned when a registration names a credential id
// some account already has. A browser refuses the prompt itself when the
// challenge excludes the credential, so this is a client that ignored the
// exclusions.
var ErrAlreadyRegistered = errors.New("that passkey is already registered")

// ErrBadCredential is returned when the posted field does not parse as a
// credential. The page always posts what PublicKeyCredential.toJSON produced,
// so this is a hand-made post.
var ErrBadCredential = errors.New("that is not a credential")

// ErrClonedCredential is returned for an assertion whose signature counter did
// not advance on an authenticator that keeps one. Two copies of the same
// private key are in use. The error names the passkey row and the account, for
// the log line.
var ErrClonedCredential = errors.New("the passkey looks like a copy")

// ErrFailedVerification is returned when the browser's answer parsed as a
// credential and go-webauthn refused it: a challenge or an origin other than
// the one issued, a signature that does not check, a backup-eligible flag that
// changed since registration. The reason is wrapped for the log. It is the
// client's fault and not the server's, so a handler does not report it as a
// server error.
var ErrFailedVerification = errors.New("the passkey could not be checked")

// Passkeys runs the two WebAuthn ceremonies, registering a device and signing
// in. It writes the webauthn_ceremony row that holds a challenge and the
// passkey_credential row a finished registration produces.
type Passkeys struct {
	webauthn   *webauthn.WebAuthn
	queries    *store.Queries
	cookieName string
	secure     bool
}

// NewPasskeys returns Passkeys for a deployment served at origin under the
// relying party id rpID. displayName is what a browser's prompt calls this
// site. cookie is the session cookie's settings. The ceremony cookie takes its
// Secure flag from them.
func NewPasskeys(queries *store.Queries, rpID, displayName, origin string, cookie CookieSettings) (*Passkeys, error) {
	w, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: displayName,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, fmt.Errorf("configure webauthn: %w", err)
	}
	return &Passkeys{
		webauthn:   w,
		queries:    queries,
		cookieName: ceremonyCookieName(cookie.Secure),
		secure:     cookie.Secure,
	}, nil
}

// ceremonyCookieName is the name of the ceremony cookie. A __Host- name is only
// valid on a Secure cookie, so the name follows the Secure flag rather than
// being configured on its own.
func ceremonyCookieName(secure bool) string {
	if secure {
		return "__Host-sprig_ceremony"
	}
	return "sprig_ceremony"
}

// BeginRegistration starts enrolling a new device for user. It returns the
// options to hand to navigator.credentials.create and the cookie that tells
// FinishRegistration which challenge an answer is for.
func (p *Passkeys) BeginRegistration(ctx context.Context, now time.Time, user store.AppUser, held []store.PasskeyCredential) (*protocol.CredentialCreation, *http.Cookie, error) {
	account := webauthnUser{user: user, held: held}
	creation, session, err := p.webauthn.BeginRegistration(account,
		// A device that already holds one of these credentials refuses the
		// prompt instead of registering a second one. go-webauthn does not read
		// the account's credentials during a registration, so they are listed
		// here.
		webauthn.WithExclusions(descriptorsOf(account.WebAuthnCredentials())),
		// Both are Required rather than Preferred. Sign-in names no credential
		// to the browser, so a credential the device did not store could never
		// sign in. User verification is the only check on who is at the device.
		// Both are checked again on the response, because the request only
		// asks. RequireResidentKey is the older name for ResidentKey, set for a
		// browser that reads only that one.
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}),
		// credProps is the only way the browser reports whether the credential
		// was stored on the device. Nothing in the signed data says so.
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("begin the registration: %w", err)
	}

	cookie, err := p.startCeremony(ctx, now, &user.ID, session)
	if err != nil {
		return nil, nil, err
	}
	return creation, cookie, nil
}

// FinishRegistration verifies the browser's answer to a registration challenge
// and saves the credential. body is the credential JSON the page posted. name
// is the label the Passkeys page shows the credential under. user is the
// account the request is signed in as. A ceremony started by any other account
// is refused, because the ceremony cookie outlives a sign-out and the next
// person at the same browser would otherwise enrol their device on the previous
// account. ErrCeremonyGone, ErrNotVerified, ErrNotDiscoverable,
// ErrAlreadyRegistered and ErrFailedVerification are the refusals the Passkeys
// page has a message for.
func (p *Passkeys) FinishRegistration(ctx context.Context, now time.Time, r *http.Request, user store.AppUser, body io.Reader, name string) (store.PasskeyCredential, error) {
	// The ceremony is taken before the body is parsed, so a prompt left open
	// past its life is reported as expired rather than as a bad credential.
	row, session, err := p.takeCeremony(ctx, now, r)
	if err != nil {
		return store.PasskeyCredential{}, err
	}
	if row.UserID == nil || *row.UserID != user.ID {
		return store.PasskeyCredential{}, ErrCeremonyGone
	}
	response, err := protocol.ParseCredentialCreationResponseBody(body)
	if err != nil {
		return store.PasskeyCredential{}, ErrBadCredential
	}

	// The flag is checked here rather than left to CreateCredential so the
	// refusal is ErrNotVerified rather than ErrFailedVerification.
	if !response.Response.AttestationObject.AuthData.Flags.UserVerified() {
		return store.PasskeyCredential{}, ErrNotVerified
	}

	// The account's other credentials are not needed here. go-webauthn reads
	// only the user handle during a registration, to match it against the one
	// the challenge named.
	credential, err := p.webauthn.CreateCredential(webauthnUser{user: user}, session, response)
	if err != nil {
		return store.PasskeyCredential{}, verificationFailure("verify the registration", err)
	}
	// RK is nil when the browser did not answer the credProps extension. Only
	// an explicit false says the device refused to store the credential.
	if credential.Extensions.RK != nil && !*credential.Extensions.RK {
		return store.PasskeyCredential{}, ErrNotDiscoverable
	}

	saved, err := p.queries.CreatePasskey(ctx, store.CreatePasskeyParams{
		UserID:       user.ID,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
		Name:         name,
		PublicKey:    credential.PublicKey,
		SignCount:    int64(credential.Authenticator.SignCount),
		Flags:        int16(credential.Flags.ProtocolValue()),
		Transports:   transportNames(credential.Transport),
	})
	if store.CredentialTaken(err) {
		return store.PasskeyCredential{}, ErrAlreadyRegistered
	}
	if err != nil {
		return store.PasskeyCredential{}, fmt.Errorf("save the passkey: %w", err)
	}
	return saved, nil
}

// BeginAssertion starts a sign-in. It returns the options to hand to
// navigator.credentials.get and the cookie that tells FinishAssertion which
// challenge an answer is for. No account is named, because the browser offers
// the passkeys it holds for this site and its answer is what says who this is.
func (p *Passkeys) BeginAssertion(ctx context.Context, now time.Time) (*protocol.CredentialAssertion, *http.Cookie, error) {
	assertion, session, err := p.webauthn.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("begin the assertion: %w", err)
	}
	cookie, err := p.startCeremony(ctx, now, nil, session)
	if err != nil {
		return nil, nil, err
	}
	return assertion, cookie, nil
}

// FinishAssertion verifies the browser's answer to a sign-in challenge and
// returns the account it proves. body is the credential JSON the page posted.
// The credential's signature counter and last-used date are written here, so
// the Passkeys page shows a device as used as soon as it is.
func (p *Passkeys) FinishAssertion(ctx context.Context, now time.Time, r *http.Request, body io.Reader) (store.AppUser, error) {
	_, session, err := p.takeCeremony(ctx, now, r)
	if err != nil {
		return store.AppUser{}, err
	}
	response, err := protocol.ParseCredentialRequestResponseBody(body)
	if err != nil {
		return store.AppUser{}, ErrBadCredential
	}

	if !response.Response.AuthenticatorData.Flags.UserVerified() {
		return store.AppUser{}, ErrNotVerified
	}

	// go-webauthn calls lookup with the credential id from the browser's
	// answer. The row is kept for the clone check below, which needs the
	// account and the stored counter. The lookup's error is kept too, because
	// go-webauthn wraps it as a verification failure and a database error must
	// not be reported as one.
	var (
		stored    store.GetPasskeyByCredentialIDRow
		lookupErr error
	)
	lookup := func(rawID, _ []byte) (webauthn.User, error) {
		stored, lookupErr = p.queries.GetPasskeyByCredentialID(ctx, base64.RawURLEncoding.EncodeToString(rawID))
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			lookupErr = ErrUnknownCredential
		} else if lookupErr != nil {
			lookupErr = fmt.Errorf("read the passkey: %w", lookupErr)
		}
		return webauthnUser{user: stored.AppUser, held: []store.PasskeyCredential{stored.PasskeyCredential}}, lookupErr
	}

	_, err = p.webauthn.ValidateDiscoverableLogin(lookup, session, response)
	if lookupErr != nil {
		return store.AppUser{}, lookupErr
	}
	if err != nil {
		return store.AppUser{}, verificationFailure("verify the assertion", err)
	}

	// The counter compared is the one the device sent. go-webauthn leaves its
	// own copy at the stored value when the counter did not advance.
	returned := response.Response.AuthenticatorData.Counter
	cloned := fmt.Errorf("%w: passkey %s on account %s", ErrClonedCredential, stored.PasskeyCredential.ID, stored.AppUser.ID)
	if Cloned(storedCount(stored.PasskeyCredential.SignCount), returned) {
		return store.AppUser{}, cloned
	}

	// The write repeats the comparison against the row as it is now. Two
	// sign-ins sending the same counter can both pass the check above, and
	// only the first of them updates the row.
	rows, err := p.queries.RecordPasskeyUse(ctx, store.RecordPasskeyUseParams{
		PasskeyID: stored.PasskeyCredential.ID,
		SignCount: int64(returned),
		UsedAt:    &now,
	})
	if err != nil {
		return store.AppUser{}, fmt.Errorf("record the passkey use: %w", err)
	}
	if rows == 0 {
		return store.AppUser{}, cloned
	}
	return stored.AppUser, nil
}

// verificationFailure wraps err in ErrFailedVerification when it is
// go-webauthn's protocol.Error, with that error's short and long reasons.
// Neither reason includes the credential. Any other err is returned wrapped
// with what.
func verificationFailure(what string, err error) error {
	var failure *protocol.Error
	if !errors.As(err, &failure) {
		return fmt.Errorf("%s: %w", what, err)
	}
	if failure.DevInfo == "" {
		return fmt.Errorf("%w: %s: %s", ErrFailedVerification, what, failure.Details)
	}
	return fmt.Errorf("%w: %s: %s: %s", ErrFailedVerification, what, failure.Details, failure.DevInfo)
}

// Cloned reports whether two copies of the same private key are in use. stored
// is the counter on the credential row and returned is the counter the device
// just sent.
//
// Both zero is an authenticator that keeps no counter, so there is nothing to
// compare. iCloud Keychain, the Android provider and the password managers all
// work that way, and refusing every non-increment would lock them out.
// Otherwise a counter that did not increase is a copy and is refused. That
// includes a credential that had a counter and stopped returning one.
func Cloned(stored, returned uint32) bool {
	if stored == 0 && returned == 0 {
		return false
	}
	return returned <= stored
}

// storedCount is a credential's signature counter as WebAuthn counts it. The
// column is a bigint and the counter is 32 bits, so a value outside that range
// is one no ceremony wrote. It reads as the largest counter there is, so Cloned
// refuses every assertion against that row instead of a wrapped number letting
// one through.
func storedCount(count int64) uint32 {
	if count < 0 || count > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(count)
}

// storedFlags is a credential's authenticator flags as WebAuthn counts them.
// The column is a smallint and the flags are one byte, so a value outside that
// range is one no registration wrote and reads as no flags set.
func storedFlags(flags int16) protocol.AuthenticatorFlags {
	if flags < 0 || flags > math.MaxUint8 {
		return 0
	}
	return protocol.AuthenticatorFlags(flags)
}

// ClearedCeremonyCookie returns a cookie that deletes the ceremony cookie from
// the browser. It is set on every answer, accepted or refused.
func (p *Passkeys) ClearedCeremonyCookie() *http.Cookie {
	cookie := p.ceremonyCookie("")
	cookie.MaxAge = -1
	return cookie
}

// startCeremony writes the challenge row and returns the cookie naming it.
// userID is the account registering a device. It is nil for a sign-in, where
// the browser's answer is what names the account.
func (p *Passkeys) startCeremony(ctx context.Context, now time.Time, userID *uuid.UUID, session *webauthn.SessionData) (*http.Cookie, error) {
	encoded, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("encode the ceremony: %w", err)
	}
	// Prompts nobody answers are the ordinary case, so the expired rows are
	// deleted here rather than on a timer of their own.
	if err := p.queries.DeleteExpiredCeremonies(ctx, now); err != nil {
		return nil, fmt.Errorf("delete the expired ceremonies: %w", err)
	}

	token := NewSessionToken()
	if _, err := p.queries.CreateCeremony(ctx, store.CreateCeremonyParams{
		TokenHash: HashToken(token),
		UserID:    userID,
		Session:   encoded,
		ExpiresAt: now.Add(ceremonyLife),
	}); err != nil {
		return nil, fmt.Errorf("create the ceremony: %w", err)
	}
	return p.ceremonyCookie(token), nil
}

// takeCeremony reads and deletes the ceremony the request's cookie names.
// Deleting it is what makes a challenge answerable once.
func (p *Passkeys) takeCeremony(ctx context.Context, now time.Time, r *http.Request) (store.WebauthnCeremony, webauthn.SessionData, error) {
	var session webauthn.SessionData
	cookie, err := r.Cookie(p.cookieName)
	if err != nil {
		return store.WebauthnCeremony{}, session, ErrCeremonyGone
	}
	row, err := p.queries.TakeCeremony(ctx, HashToken(cookie.Value), now)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.WebauthnCeremony{}, session, ErrCeremonyGone
	}
	if err != nil {
		return store.WebauthnCeremony{}, session, fmt.Errorf("read the ceremony: %w", err)
	}
	if err := json.Unmarshal(row.Session, &session); err != nil {
		return store.WebauthnCeremony{}, session, fmt.Errorf("decode the ceremony: %w", err)
	}
	return row, session, nil
}

// ceremonyCookie returns the ceremony cookie holding token. HttpOnly keeps
// script from reading it. SameSite=Lax keeps a cross-site post from sending
// it. It expires when the row does.
func (p *Passkeys) ceremonyCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     p.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ceremonyLife / time.Second),
		HttpOnly: true,
		Secure:   p.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// webauthnUser is the webauthn.User go-webauthn reads an account through. The
// WebAuthn user handle is the row's UUID rather than the handle a person
// picked, because the browser stores it with the passkey and a person can
// change their handle.
type webauthnUser struct {
	user store.AppUser
	held []store.PasskeyCredential
}

func (u webauthnUser) WebAuthnID() []byte { return u.user.ID[:] }

func (u webauthnUser) WebAuthnName() string { return u.user.Handle }

func (u webauthnUser) WebAuthnDisplayName() string { return u.user.DisplayName }

func (u webauthnUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, 0, len(u.held))
	for _, held := range u.held {
		credential, err := storedCredential(held)
		if err != nil {
			// The credential id is stored base64url encoded. A row that does
			// not decode cannot be matched by an assertion, so it is left out
			// rather than failing the whole ceremony.
			continue
		}
		credentials = append(credentials, credential)
	}
	return credentials
}

// descriptorsOf turns credentials into the descriptors a registration challenge
// lists as already enrolled.
func descriptorsOf(credentials []webauthn.Credential) []protocol.CredentialDescriptor {
	descriptors := make([]protocol.CredentialDescriptor, 0, len(credentials))
	for _, credential := range credentials {
		descriptors = append(descriptors, credential.Descriptor())
	}
	return descriptors
}

// storedCredential turns a passkey_credential row into the credential
// go-webauthn verifies against. The flags have to be included: go-webauthn
// refuses an assertion whose backup-eligible flag differs from the stored one,
// and a synced passkey returns that flag set on every assertion.
func storedCredential(row store.PasskeyCredential) (webauthn.Credential, error) {
	id, err := base64.RawURLEncoding.DecodeString(row.CredentialID)
	if err != nil {
		return webauthn.Credential{}, fmt.Errorf("decode the credential id: %w", err)
	}
	credential := webauthn.Credential{
		ID:        id,
		PublicKey: row.PublicKey,
		Flags:     webauthn.NewCredentialFlags(storedFlags(row.Flags)),
		Authenticator: webauthn.Authenticator{
			SignCount: storedCount(row.SignCount),
		},
	}
	for _, transport := range row.Transports {
		credential.Transport = append(credential.Transport, protocol.AuthenticatorTransport(transport))
	}
	return credential, nil
}

func transportNames(transports []protocol.AuthenticatorTransport) []string {
	names := make([]string, 0, len(transports))
	for _, transport := range transports {
		names = append(names, string(transport))
	}
	return names
}
