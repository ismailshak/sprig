package store

import (
	"regexp"
	"strings"
	"testing"
)

func TestHandleFor(t *testing.T) {
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
			if got := handleFor(tt.displayName, tt.attempt); got != tt.want {
				t.Errorf("handleFor(%q, %d) = %q, want %q", tt.displayName, tt.attempt, got, tt.want)
			}
		})
	}
}

func TestHandleFor_ACollisionTakesARandomSuffixRatherThanTheNextNumber(t *testing.T) {
	shape := regexp.MustCompile(`^emma_[a-z2-7]{4}$`)

	seen := map[string]bool{}
	for attempt := 2; attempt < 12; attempt++ {
		got := handleFor("Emma", attempt)
		if !shape.MatchString(got) {
			t.Fatalf("handleFor(\"Emma\", %d) = %q, want the slug and a random suffix", attempt, got)
		}
		seen[got] = true
	}
	if len(seen) < 10 {
		t.Errorf("ten candidates produced %d distinct handles, want ten", len(seen))
	}
}

func TestHandleFor_ASuffixedCandidateStillFits(t *testing.T) {
	got := handleFor(strings.Repeat("a", 40), 2)
	if len(got) != maxHandleLength {
		t.Errorf("handleFor on a forty-character name = %q, %d characters, want %d",
			got, len(got), maxHandleLength)
	}
}
