package store

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"uuid"
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

func TestCreateAccountWithHandle_AHandleAnotherAccountHoldsIsRefused(t *testing.T) {
	tx := sharedTx(t)
	q := New(tx)
	if _, err := q.CreateAccountWithHandle(t.Context(), uuid.NewV7(), "Wren", "wren_hale", "Europe/London"); err != nil {
		t.Fatalf("the first account: %v", err)
	}

	_, err := q.CreateAccountWithHandle(t.Context(), uuid.NewV7(), "Wren Hale", "wren_hale", "Europe/London")

	if !errors.Is(err, ErrHandleTaken) {
		t.Errorf("the second account was refused with %v, want ErrHandleTaken", err)
	}
	var accounts int
	if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM app_user WHERE handle = $1", "wren_hale").Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 {
		t.Errorf("%d accounts hold the handle, want the first alone", accounts)
	}
}

func TestCreateAccountWithHandle_TheHandleIsWrittenAsGivenAndNoSuffixIsAdded(t *testing.T) {
	q := New(sharedTx(t))

	user, err := q.CreateAccountWithHandle(t.Context(), uuid.NewV7(), "Wren Hale", "wren", "Europe/London")

	if err != nil {
		t.Fatal(err)
	}
	if user.Handle != "wren" {
		t.Errorf("the account's handle is %q, want wren", user.Handle)
	}
}

func TestFreeHandle_ANameNoAccountHoldsGetsItsSlug(t *testing.T) {
	q := New(sharedTx(t))

	handle, err := q.FreeHandle(t.Context(), "Wren Hale")

	if err != nil {
		t.Fatal(err)
	}
	if handle != "wren_hale" {
		t.Errorf("the handle is %q, want wren_hale", handle)
	}
}

func TestFreeHandle_ANameAnotherAccountHoldsGetsASuffixNoAccountHolds(t *testing.T) {
	q := New(sharedTx(t))
	if _, err := q.CreateAccountWithHandle(t.Context(), uuid.NewV7(), "Wren", "wren", "Europe/London"); err != nil {
		t.Fatal(err)
	}

	handle, err := q.FreeHandle(t.Context(), "Wren")

	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^wren_[a-z2-7]{4}$`).MatchString(handle) {
		t.Errorf("the handle is %q, want wren and a four character suffix", handle)
	}
	taken, err := q.HandleExists(t.Context(), handle)
	if err != nil {
		t.Fatal(err)
	}
	if taken {
		t.Errorf("%q is held by an account", handle)
	}
}
