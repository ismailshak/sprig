package store

import (
	"crypto/rand"
	"strings"
	"unicode"
)

// Four base32 characters gives about a million possible suffixes.
const handleSuffixLength = 4

const maxHandleLength = 32

// handleFor returns the attempt-th candidate handle for displayName. The
// caller tries candidates in turn until the unique index accepts one. The
// suffix is random rather than a count, so a handle does not reveal how many
// people share a name.
func handleFor(displayName string, attempt int) string {
	limit := maxHandleLength
	if attempt > 1 {
		// Leave room for the underscore and suffix.
		limit -= handleSuffixLength + 1
	}
	slug := slugify(displayName, limit)
	if slug == "" {
		slug = "gardener"
	}
	if attempt <= 1 {
		return slug
	}
	return slug + "_" + strings.ToLower(rand.Text()[:handleSuffixLength])
}

// slugify lowercases name, replaces runs of other characters with one
// underscore, and cuts it to limit runes. It returns the empty string for a
// name with no letter or digit.
func slugify(name string, limit int) string {
	runes := make([]rune, 0, limit)
	separated := false
	for _, r := range strings.ToLower(name) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			separated = true
			continue
		}
		if separated && len(runes) > 0 {
			runes = append(runes, '_')
		}
		separated = false
		runes = append(runes, r)
	}
	if len(runes) > limit {
		runes = runes[:limit]
	}
	// The cut can leave a trailing underscore.
	return strings.TrimRight(string(runes), "_")
}

// NormaliseHandle returns the stored form of a handle typed on the Account
// page: lower case letters and digits, single underscores between them, cut to
// maxHandleLength characters. It applies the same rule as handleFor does when
// an account is created, so "Emma Fletcher" is stored as "emma_fletcher"
// rather than rejected. It returns "" when there is no letter or digit.
func NormaliseHandle(handle string) string {
	return slugify(handle, maxHandleLength)
}
