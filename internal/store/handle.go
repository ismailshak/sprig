package store

import (
	"crypto/rand"
	"strings"
	"unicode"
)

// Four base32 characters is a million suffixes.
const handleSuffixLength = 4

const maxHandleLength = 32

// handleFor returns the attempt-th candidate handle, tried in turn against the
// unique index until one is free. The suffix is random rather than a count, so a
// handle does not say how many people share a name.
func handleFor(displayName string, attempt int) string {
	limit := maxHandleLength
	if attempt > 1 {
		// The suffix and its underscore have to fit too.
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

// slugify returns the empty string for a name holding neither a letter nor a
// digit.
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
	// The cut can land on an underscore the loop put there.
	return strings.TrimRight(string(runes), "_")
}
