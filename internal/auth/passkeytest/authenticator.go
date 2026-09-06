// Package passkeytest is a passkey in software. A test uses it to answer a
// registration or a sign-in challenge the way a browser and a device would, so
// the whole ceremony can run against the server with no browser in the loop.
// Nothing outside a test imports it.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
)

// The bits of the flags byte in authenticator data.
const (
	flagUserPresent    = 0x01
	flagUserVerified   = 0x04
	flagBackupEligible = 0x08
	flagBackedUp       = 0x10
	flagAttested       = 0x40
)

// credentialIDLength is how many random bytes name a credential.
const credentialIDLength = 16

// Authenticator holds one P-256 key and answers challenges for it. The fields
// are what a test changes to model a different kind of device or a
// tampered answer.
type Authenticator struct {
	// RPID is the relying party id the answers are bound to.
	RPID string
	// Origin is the page origin written into the client data. The server
	// refuses an answer whose origin is not the one it serves.
	Origin string
	// Verified sets the user-verified flag on every answer. A device with no
	// screen lock, or one that was locked, leaves it clear.
	Verified bool
	// BackupEligible sets the backup-eligible and backed-up flags. A synced
	// passkey sets both and a hardware key sets neither. The server
	// refuses a sign-in whose backup-eligible flag differs from the
	// registration's.
	BackupEligible bool
	// Stores is what the credProps extension reports at registration: whether
	// the device kept the credential. nil leaves the extension unanswered,
	// which is what a browser without it does.
	Stores *bool
	// Counter is the signature counter the next answer sends. A synced
	// passkey leaves it at zero. A hardware key sets it higher on every use.
	Counter uint32
	// UserHandle is the account the credential was registered to, sent back
	// with every sign-in answer. Register sets it. A test changes it to answer
	// as another account.
	UserHandle []byte

	key          *ecdsa.PrivateKey
	credentialID []byte
}

// New returns an authenticator for the relying party at origin, set up like a
// phone with a screen lock: it verifies the person, keeps the credential, and
// keeps no counter.
func New(rpID, origin string) *Authenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	credentialID := make([]byte, credentialIDLength)
	if _, err := rand.Read(credentialID); err != nil {
		panic(err)
	}
	stores := true
	return &Authenticator{
		RPID:         rpID,
		Origin:       origin,
		Verified:     true,
		Stores:       &stores,
		key:          key,
		credentialID: credentialID,
	}
}

// Clone returns a second authenticator holding the same key and credential
// id. It is what the signature counter exists to catch.
func (a *Authenticator) Clone() *Authenticator {
	copied := *a
	return &copied
}

// CredentialID returns the id the server stores the credential under, base64url
// encoded.
func (a *Authenticator) CredentialID() string {
	return base64.RawURLEncoding.EncodeToString(a.credentialID)
}

// Register answers a registration challenge. It returns the JSON the page
// would post, in the shape PublicKeyCredential.toJSON produces.
func (a *Authenticator) Register(creation *protocol.CredentialCreation) string {
	a.UserHandle = userHandle(creation.Response.User.ID)

	clientData := a.clientData("webauthn.create", creation.Response.Challenge)
	authData := a.authData(flagAttested)
	authData = append(authData, make([]byte, 16)...) // The AAGUID. All zero means the device does not say what it is.
	authData = binary.BigEndian.AppendUint16(authData, credentialIDLength)
	authData = append(authData, a.credentialID...)
	authData = append(authData, a.coseKey()...)

	// The attestation object is a CBOR map with the format "none", an empty
	// statement, and the authenticator data. Keys are in the order CTAP2's
	// canonical form puts them: shorter first.
	attestation := cborMap(3)
	attestation = append(attestation, cborText("fmt")...)
	attestation = append(attestation, cborText("none")...)
	attestation = append(attestation, cborText("attStmt")...)
	attestation = append(attestation, cborMap(0)...)
	attestation = append(attestation, cborText("authData")...)
	attestation = append(attestation, cborBytes(authData)...)

	answer := creationAnswer{
		ID:    a.CredentialID(),
		RawID: a.CredentialID(),
		Type:  "public-key",
		Response: attestationResponse{
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientData),
			AttestationObject: base64.RawURLEncoding.EncodeToString(attestation),
			Transports:        []string{"internal"},
		},
	}
	if a.Stores != nil {
		answer.ClientExtensionResults = &extensionResults{CredProps: &credProps{RK: *a.Stores}}
	}
	return marshal(answer)
}

// Assert answers a sign-in challenge. It returns the JSON the page would post.
func (a *Authenticator) Assert(assertion *protocol.CredentialAssertion) string {
	clientData := a.clientData("webauthn.get", assertion.Response.Challenge)
	authData := a.authData(0)

	// The signature covers the authenticator data and the hash of the client
	// data. That is what puts the challenge and the origin under the signature.
	clientHash := sha256.Sum256(clientData)
	signed := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		panic(err)
	}

	return marshal(assertionAnswer{
		ID:    a.CredentialID(),
		RawID: a.CredentialID(),
		Type:  "public-key",
		Response: assertionResponse{
			ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientData),
			AuthenticatorData: base64.RawURLEncoding.EncodeToString(authData),
			Signature:         base64.RawURLEncoding.EncodeToString(signature),
			UserHandle:        base64.RawURLEncoding.EncodeToString(a.UserHandle),
		},
	})
}

// userHandle is the account a registration challenge names. It is raw bytes
// when the challenge came straight from BeginRegistration and a base64url
// string when it was read back from the JSON the server sent.
func userHandle(id any) []byte {
	switch id := id.(type) {
	case protocol.URLEncodedBase64:
		return id
	case []byte:
		return id
	case string:
		handle, err := base64.RawURLEncoding.DecodeString(id)
		if err != nil {
			panic(fmt.Sprintf("the challenge names the account as %q. That is not base64url", id))
		}
		return handle
	default:
		panic(fmt.Sprintf("the challenge names the account as %T, want bytes or a base64url string", id))
	}
}

// clientData is the JSON a browser builds for the device to sign: the
// ceremony, the challenge and the origin.
func (a *Authenticator) clientData(ceremony string, challenge protocol.URLEncodedBase64) []byte {
	return []byte(marshal(map[string]string{
		"type":      ceremony,
		"challenge": challenge.String(),
		"origin":    a.Origin,
	}))
}

// authData is the fixed front of the authenticator data: the hash of the
// relying party id, the flags byte, and the counter. extra is OR'd into the
// flags.
func (a *Authenticator) authData(extra byte) []byte {
	rpIDHash := sha256.Sum256([]byte(a.RPID))
	flags := byte(flagUserPresent) | extra
	if a.Verified {
		flags |= flagUserVerified
	}
	if a.BackupEligible {
		flags |= flagBackupEligible | flagBackedUp
	}
	data := append([]byte{}, rpIDHash[:]...)
	data = append(data, flags)
	return binary.BigEndian.AppendUint32(data, a.Counter)
}

// coseKey is the public key as COSE encodes an EC2 key: a CBOR map of the key
// type, the algorithm, the curve and the two coordinates.
func (a *Authenticator) coseKey() []byte {
	// Bytes is the uncompressed point: one byte of 0x04, then x, then y.
	point, err := a.key.PublicKey.Bytes()
	if err != nil {
		panic(err)
	}
	x, y := point[1:33], point[33:65]
	key := cborMap(5)
	key = append(key, cborUint(1), cborUint(2))     // kty: EC2
	key = append(key, cborUint(3), cborNegative(7)) // alg: ES256
	key = append(key, cborNegative(1), cborUint(1)) // crv: P-256
	key = append(key, cborNegative(2))              // x
	key = append(key, cborBytes(x)...)
	key = append(key, cborNegative(3)) // y
	key = append(key, cborBytes(y)...)
	return key
}

func cborMap(entries int) []byte { return cborHead(5, entries) }

func cborText(s string) []byte { return append(cborHead(3, len(s)), s...) }

func cborBytes(b []byte) []byte { return append(cborHead(2, len(b)), b...) }

// cborUint is an unsigned integer under 24. It fits in the head byte.
func cborUint(n byte) byte { return n }

// cborNegative is -n for n from 1 to 24. CBOR stores -1-n under major type 1.
func cborNegative(n byte) byte { return 0x20 | (n - 1) }

// cborHead is the first bytes of an item: the major type and the count or
// length that follows it.
func cborHead(major byte, n int) []byte {
	if n < 0 {
		panic("a CBOR item cannot have a negative length")
	}
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 1<<8:
		return []byte{major<<5 | 24, byte(n)}
	case n < 1<<16:
		return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
	default:
		panic("a CBOR item longer than the encoder here handles")
	}
}

// creationAnswer is what PublicKeyCredential.toJSON produces for a
// registration.
type creationAnswer struct {
	ID                     string              `json:"id"`
	RawID                  string              `json:"rawId"`
	Type                   string              `json:"type"`
	Response               attestationResponse `json:"response"`
	ClientExtensionResults *extensionResults   `json:"clientExtensionResults,omitempty"`
}

type attestationResponse struct {
	ClientDataJSON    string   `json:"clientDataJSON"`
	AttestationObject string   `json:"attestationObject"`
	Transports        []string `json:"transports"`
}

type extensionResults struct {
	CredProps *credProps `json:"credProps,omitempty"`
}

type credProps struct {
	RK bool `json:"rk"`
}

// assertionAnswer is what PublicKeyCredential.toJSON produces for a sign-in.
type assertionAnswer struct {
	ID       string            `json:"id"`
	RawID    string            `json:"rawId"`
	Type     string            `json:"type"`
	Response assertionResponse `json:"response"`
}

type assertionResponse struct {
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"userHandle"`
}

func marshal(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(out)
}
