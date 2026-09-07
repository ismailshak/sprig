package push

import (
	"strings"
	"testing"
)

func TestGenerateKeys_ReturnsAPairThatValidates(t *testing.T) {
	keys, err := GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys: %v", err)
	}
	keys.Subject = "mailto:sprig@example.com"

	if err := keys.Validate(); err != nil {
		t.Errorf("a generated pair does not validate: %v", err)
	}
}

func TestKeys_APublicKeyFromAnotherPairIsRefused(t *testing.T) {
	first, _ := GenerateKeys()
	second, _ := GenerateKeys()
	keys := Keys{Public: first.Public, Private: second.Private, Subject: "mailto:sprig@example.com"}

	err := keys.Validate()

	if err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("a mismatched pair validated with %v", err)
	}
}

func TestKeys_APaddedKeyIsAccepted(t *testing.T) {
	keys, _ := GenerateKeys()
	keys.Public += "="
	keys.Subject = "https://sprig.example.com"

	if err := keys.Validate(); err != nil {
		t.Errorf("a key with base64 padding was refused: %v", err)
	}
}

func TestKeys_ASubjectThatIsNeitherMailtoNorHTTPSIsRefused(t *testing.T) {
	keys, _ := GenerateKeys()

	for _, subject := range []string{"", "sprig@example.com", "http://sprig.example.com"} {
		keys.Subject = subject
		if err := keys.Validate(); err == nil || !strings.Contains(err.Error(), "subject") {
			t.Errorf("subject %q validated with %v", subject, err)
		}
	}
}

func TestKeys_AKeyThatIsNotBase64urlIsRefused(t *testing.T) {
	keys, _ := GenerateKeys()
	keys.Private = "not/base64url"
	keys.Subject = "mailto:sprig@example.com"

	if err := keys.Validate(); err == nil || !strings.Contains(err.Error(), "private key") {
		t.Errorf("a private key with a slash in it validated with %v", err)
	}
}
