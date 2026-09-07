package auth

import (
	"crypto/rand"
	"strings"
)

// RecoveryBatchSize is how many codes the Create codes button makes at once.
// The Recover an account page says "one of the ten you were shown", so
// changing this means changing that sentence too.
const RecoveryBatchSize = 10

// recoveryAlphabet is the 32 characters a recovery code is drawn from. The
// digits 0 and 1 and the letters i and o are left out because they are misread
// for each other on paper.
const recoveryAlphabet = "abcdefghjklmnpqrstuvwxyz23456789"

const (
	// recoveryGroups and recoveryGroupLength give a code the shape
	// k4rt-9wme-3xqd: twelve characters, so 60 bits of randomness.
	recoveryGroups      = 3
	recoveryGroupLength = 4
)

// NewRecoveryCode returns one recovery code in the form it is shown and
// stored: three groups of four lowercase characters joined by hyphens.
func NewRecoveryCode() string {
	raw := make([]byte, recoveryGroups*recoveryGroupLength)
	// rand.Read cannot return an error since Go 1.24. It panics instead, so
	// there is nothing here to handle.
	_, _ = rand.Read(raw)
	groups := make([]string, 0, recoveryGroups)
	for g := range recoveryGroups {
		var group strings.Builder
		for _, b := range raw[g*recoveryGroupLength : (g+1)*recoveryGroupLength] {
			// The alphabet has 32 characters, so the low five bits of a byte
			// pick one without bias.
			group.WriteByte(recoveryAlphabet[b&31])
		}
		groups = append(groups, group.String())
	}
	return strings.Join(groups, "-")
}

// CanonicalRecoveryCode rewrites a typed code into the form NewRecoveryCode
// produces, so its hash can be looked up. Capitals, spaces and missing or
// misplaced hyphens are accepted, because a code is typed in from paper. The
// bool is false when what is left is not twelve characters of the alphabet.
func CanonicalRecoveryCode(typed string) (string, bool) {
	var symbols strings.Builder
	for _, r := range strings.ToLower(typed) {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r':
			continue
		case strings.ContainsRune(recoveryAlphabet, r):
			symbols.WriteRune(r)
		default:
			return "", false
		}
	}
	flat := symbols.String()
	if len(flat) != recoveryGroups*recoveryGroupLength {
		return "", false
	}
	groups := make([]string, 0, recoveryGroups)
	for g := range recoveryGroups {
		groups = append(groups, flat[g*recoveryGroupLength:(g+1)*recoveryGroupLength])
	}
	return strings.Join(groups, "-"), true
}
