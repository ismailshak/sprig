package store

import (
	"regexp"
	"strings"
	"testing"
)

func TestHandleCandidate_ANameBecomesItsLowerCaseSlug(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		attempt     int
		want        string
	}{
		{"a first name", "Emma", 1, "emma"},
		{"two words", "Emma Fletcher", 1, "emma_fletcher"},
		{"punctuation separates", "O'Brien-Smith", 1, "o_brien_smith"},
		{"surrounding space is not a separator", "  Emma  ", 1, "emma"},
		{"a run of separators is one underscore", "Emma   ~ Fletcher", 1, "emma_fletcher"},
		{"digits are kept", "Plant Room 2", 1, "plant_room_2"},
		{"a name outside ASCII keeps its letters", "Élodie", 1, "élodie"},
		{"a name with no letters or digits", "🌱", 1, "gardener"},
		{"an empty name", "", 1, "gardener"},
		{"a long name is cut", strings.Repeat("a", 40), 1, strings.Repeat("a", 32)},
		{"a cut mid-word keeps the words before it", strings.Repeat("a", 20) + " " + strings.Repeat("b", 40), 1, strings.Repeat("a", 20) + "_" + strings.Repeat("b", 11)},
		{"a cut landing on a separator drops it", strings.Repeat("a", 31) + " " + strings.Repeat("b", 40), 1, strings.Repeat("a", 31)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := handleCandidate(tt.displayName, tt.attempt); got != tt.want {
				t.Errorf("handleCandidate(%q, %d) = %q, want %q", tt.displayName, tt.attempt, got, tt.want)
			}
		})
	}
}

func TestHandleCandidate_ACollisionGetsARandomSuffixNotACounter(t *testing.T) {
	shape := regexp.MustCompile(`^emma_[a-z2-7]{4}$`)

	seen := map[string]bool{}
	for attempt := 2; attempt < 12; attempt++ {
		got := handleCandidate("Emma", attempt)
		if !shape.MatchString(got) {
			t.Fatalf("handleCandidate(\"Emma\", %d) = %q, want the slug and a random suffix", attempt, got)
		}
		seen[got] = true
	}
	if len(seen) < 10 {
		t.Errorf("ten candidates produced %d distinct handles, want ten", len(seen))
	}
}

func TestHandleCandidate_ASuffixedCandidateStaysWithinTheLengthLimit(t *testing.T) {
	got := handleCandidate(strings.Repeat("a", 40), 2)
	if len(got) != maxHandleLength {
		t.Errorf("handleCandidate on a forty-character name = %q, %d characters, want %d",
			got, len(got), maxHandleLength)
	}
}

// A generated handle has to normalise to itself. Otherwise opening the Account
// page and pressing Save would rewrite a handle nobody edited.
func TestNormaliseHandle_AGeneratedHandleIsUnchanged(t *testing.T) {
	for _, displayName := range []string{"Emma", "Emma Fletcher", "O'Brien-Smith", "Élodie", "🌱", "", strings.Repeat("a", 40)} {
		for _, attempt := range []int{1, 2} {
			handle := handleCandidate(displayName, attempt)
			if got := NormaliseHandle(handle); got != handle {
				t.Errorf("handleCandidate(%q, %d) = %q and NormaliseHandle rewrites it to %q", displayName, attempt, handle, got)
			}
		}
	}
}
