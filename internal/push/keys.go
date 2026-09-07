package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Keys is the VAPID key pair push messages are signed with, and the contact
// address sent with them. All three are read from the environment at startup.
type Keys struct {
	// Public is the P-256 public key, base64url encoded. The browser is given
	// it to subscribe with.
	Public string
	// Private is the 32-byte P-256 private key as base64url.
	Private string
	// Subject is a mailto: address or an https URL. It goes to the push
	// service with every message so the service can contact whoever runs this
	// server.
	Subject string
}

// GenerateKeys returns a new P-256 key pair with no subject.
func GenerateKeys() (Keys, error) {
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return Keys{}, fmt.Errorf("generating a VAPID key: %w", err)
	}
	return Keys{
		Public:  base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		Private: base64.RawURLEncoding.EncodeToString(private.Bytes()),
	}, nil
}

// Validate checks that Public and Private are one P-256 pair and that Subject
// is a mailto: address or an https URL. A mismatched pair is otherwise only
// noticed when the push service refuses every message, because the browser
// subscribes with whatever public key it is given.
func (k Keys) Validate() error {
	private, err := decodeKey(k.Private)
	if err != nil {
		return fmt.Errorf("the private key is not base64url: %w", err)
	}
	public, err := decodeKey(k.Public)
	if err != nil {
		return fmt.Errorf("the public key is not base64url: %w", err)
	}
	key, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		return errors.New("the private key is not a 32-byte P-256 key")
	}
	given, err := ecdh.P256().NewPublicKey(public)
	if err != nil {
		return errors.New("the public key is not a point on P-256")
	}
	if !key.PublicKey().Equal(given) {
		return errors.New("the public key does not belong to the private key")
	}
	if !strings.HasPrefix(k.Subject, "mailto:") && !strings.HasPrefix(k.Subject, "https://") {
		return errors.New("the subject must be a mailto: address or an https URL")
	}
	return nil
}

// decodeKey decodes a base64url key, with or without padding, because other
// tools print the padded form.
func decodeKey(v string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(v, "="))
}
