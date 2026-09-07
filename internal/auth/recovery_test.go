package auth

import (
	"regexp"
	"testing"
)

var recoveryCodeShape = regexp.MustCompile(`^[abcdefghjklmnpqrstuvwxyz23456789]{4}-[abcdefghjklmnpqrstuvwxyz23456789]{4}-[abcdefghjklmnpqrstuvwxyz23456789]{4}$`)

func TestNewRecoveryCode_IsThreeGroupsOfFourFromTheAlphabetAndIsItsOwnCanonicalForm(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		code := NewRecoveryCode()
		if !recoveryCodeShape.MatchString(code) {
			t.Fatalf("the code %q is not three groups of four from the alphabet", code)
		}
		if canonical, ok := CanonicalRecoveryCode(code); !ok || canonical != code {
			t.Fatalf("the code %q reads back as %q, %v", code, canonical, ok)
		}
		if seen[code] {
			t.Fatalf("the code %q came up twice in a thousand", code)
		}
		seen[code] = true
	}
}

func TestCanonicalRecoveryCode_AcceptsCapitalsSpacesAndMissingHyphens(t *testing.T) {
	for _, typed := range []string{
		"k4rt-9wme-3xqd",
		"K4RT-9WME-3XQD",
		"k4rt9wme3xqd",
		" k4rt 9wme 3xqd\n",
		"k4r-t9w-me3-xqd",
	} {
		if got, ok := CanonicalRecoveryCode(typed); !ok || got != "k4rt-9wme-3xqd" {
			t.Errorf("CanonicalRecoveryCode(%q) = %q, %v, want k4rt-9wme-3xqd, true", typed, got, ok)
		}
	}
}

func TestCanonicalRecoveryCode_RefusesAStringThatIsNotTwelveCharactersOfTheAlphabet(t *testing.T) {
	for _, typed := range []string{
		"",
		"k4rt-9wme",
		"k4rt-9wme-3xqd-3xqd",
		"k4rt-9wme-3xq0",
		"k4rt-9wme-3xqi",
		"k4rt_9wme_3xqd",
		"k4rt-9wme-3xqé",
	} {
		if got, ok := CanonicalRecoveryCode(typed); ok {
			t.Errorf("CanonicalRecoveryCode(%q) = %q, true, want a refusal", typed, got)
		}
	}
}
