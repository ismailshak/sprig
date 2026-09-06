package store

import (
	"strings"
	"testing"
)

func TestCareTypeSlug(t *testing.T) {
	tests := []struct {
		name     string
		careType string
		want     string
	}{
		{"one word", "Water", "water"},
		{"two words", "Wipe leaves", "wipe_leaves"},
		{"surrounding space is not a separator", "  Feed  ", "feed"},
		{"punctuation separates", "Feed & mist", "feed_mist"},
		{"digits are kept", "Repot in 2 sizes", "repot_in_2_sizes"},
		{"a name outside ASCII keeps its letters", "Fütter", "fütter"},
		{"a name with no letter or digit has no slug", "!!!", ""},
		{"an empty name has no slug", "", ""},
		{"a long name is cut", strings.Repeat("a", 40), strings.Repeat("a", 32)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CareTypeSlug(tt.careType); got != tt.want {
				t.Errorf("CareTypeSlug(%q) = %q, want %q", tt.careType, got, tt.want)
			}
		})
	}
}
