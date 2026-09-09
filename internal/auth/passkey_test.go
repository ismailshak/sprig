package auth

import (
	"testing"
	"uuid"
)

func TestCloned_ACounterIsACopyOnlyWhenItFailsToAdvanceOnADeviceThatKeepsOne(t *testing.T) {
	cases := []struct {
		name             string
		stored, returned uint32
		want             bool
	}{
		{"a counter that advanced by one", 4, 5, false},
		// A hardware key keeps one counter for every site it is used on.
		{"a counter that jumped ahead", 4, 40, false},
		{"the same counter twice", 4, 4, true},
		{"a counter that went backwards", 4, 3, true},
		{"a first assertion from a key that has a counter", 0, 1, false},
		{"a key that had a counter and stopped returning one", 4, 0, true},
		// iCloud Keychain, the Android provider and the password managers all
		// hold one credential on several devices with no counter between them,
		// and every assertion returns zero. Refusing those would lock out most
		// of the devices sprig is used from.
		{"a device that keeps no counter", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Cloned(c.stored, c.returned); got != c.want {
				t.Errorf("Cloned(%d, %d) = %t, want %t", c.stored, c.returned, got, c.want)
			}
		})
	}
}

func TestStoredCount_ACounterOutsideTheRangeWebAuthnCountsInRefusesEveryAssertion(t *testing.T) {
	// The column is a bigint and the counter is 32 bits, so a value outside
	// that range is one no ceremony wrote. Reading it as the largest counter
	// there is makes Cloned refuse rather than let a wrapped number through.
	for _, count := range []int64{-1, 1 << 32} {
		if got := storedCount(count); !Cloned(got, 4294967295) {
			t.Errorf("a stored counter of %d reads as %d, which accepted an assertion", count, got)
		}
	}
}

func TestAaguidOf_AnAllZeroOrMalformedAAGUIDIsNil(t *testing.T) {
	if got := aaguidOf(make([]byte, 16)); got != nil {
		t.Errorf("the all-zero AAGUID is %v, want nil, because it names no provider", got)
	}
	if got := aaguidOf([]byte{1, 2, 3}); got != nil {
		t.Errorf("a 3-byte AAGUID is %v, want nil", got)
	}
	if got := aaguidOf(nil); got != nil {
		t.Errorf("a missing AAGUID is %v, want nil", got)
	}
}

func TestAaguidOf_ASixteenByteAAGUIDIsKeptAsAUUID(t *testing.T) {
	want := uuid.MustParse("bada5566-a7aa-401f-bd96-45619a55120d")

	got := aaguidOf(want[:])

	if got == nil || *got != want {
		t.Errorf("aaguidOf = %v, want %s", got, want)
	}
}
