package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// Four base32 characters gives about a million possible suffixes.
const handleSuffixLength = 4

const maxHandleLength = 32

// HandleFor returns the handle an account is given when no other account holds
// it: displayName slugified. Set up your garden gives it to the browser before
// the account row exists, because the browser stores the handle with the
// passkey. CreateAccount may add a random suffix to the handle it writes, so
// the account's handle can differ from the one on the passkey. Nothing reads
// the one on the passkey.
func HandleFor(displayName string) string {
	return handleCandidate(displayName, 1)
}

// handleCandidate returns the attempt-th candidate handle for displayName. The
// caller tries candidates in turn until the unique index accepts one. The
// suffix is random rather than a count, so a handle does not reveal how many
// people share a name.
func handleCandidate(displayName string, attempt int) string {
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
// maxHandleLength characters. It applies the same rule as HandleFor does when
// an account is created, so "Emma Fletcher" is stored as "emma_fletcher"
// rather than rejected. It returns "" when there is no letter or digit.
func NormaliseHandle(handle string) string {
	return slugify(handle, maxHandleLength)
}

// ErrHandleTaken is returned by CreateAccountWithHandle when another account
// holds the handle.
var ErrHandleTaken = errors.New("another account holds the handle")

// CreateAccountWithHandle inserts an account under id with the handle typed on
// Set up your garden or an invite's join form. It tries that one handle and
// returns ErrHandleTaken when the unique index refuses it, because a person
// who chose a handle wants to be told rather than given one with a suffix.
func (q *Queries) CreateAccountWithHandle(ctx context.Context, id uuid.UUID, displayName, handle, timezone string) (AppUser, error) {
	user, err := q.CreateUser(ctx, CreateUserParams{ID: id, DisplayName: displayName, Handle: handle, Timezone: timezone})
	if errors.Is(err, pgx.ErrNoRows) {
		return AppUser{}, ErrHandleTaken
	}
	if err != nil {
		return AppUser{}, fmt.Errorf("writing the account: %w", err)
	}
	return user, nil
}

// handleAttempts is how many candidate handles CreateAccount tries before
// giving up. The first is the slug alone and the rest end in a random suffix,
// so running out means the random suffix collided nine times in a row.
const handleAttempts = 10

// CreateAccount inserts an account under id with a handle derived from
// displayName: the slug of the name, or the slug and a random suffix when
// another account already holds it. The unique index is what decides a
// candidate is free. A lookup first would let two accounts created at the
// same moment both be told the slug was free.
func (q *Queries) CreateAccount(ctx context.Context, id uuid.UUID, displayName, timezone string) (AppUser, error) {
	for attempt := 1; attempt <= handleAttempts; attempt++ {
		user, err := q.CreateUser(ctx, CreateUserParams{
			ID:          id,
			DisplayName: displayName,
			Handle:      handleCandidate(displayName, attempt),
			Timezone:    timezone,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return AppUser{}, fmt.Errorf("writing the account: %w", err)
		}
		return user, nil
	}
	return AppUser{}, fmt.Errorf("no free handle for %q in %d attempts", displayName, handleAttempts)
}

// FreeHandle returns a handle no account holds for displayName: the slug of
// the name, or the slug and a random suffix when another account holds it. It
// fills the Handle field on Set up your garden and an invite's join form
// before the account exists. Another account can take the handle between this
// and the write, so CreateAccountWithHandle still decides.
func (q *Queries) FreeHandle(ctx context.Context, displayName string) (string, error) {
	for attempt := 1; attempt <= handleAttempts; attempt++ {
		candidate := handleCandidate(displayName, attempt)
		taken, err := q.HandleExists(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("checking the handle: %w", err)
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free handle for %q in %d attempts", displayName, handleAttempts)
}
