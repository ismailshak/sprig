package push

import (
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	return loc
}

func utc(year int, month time.Month, day, hour, minute, second int) time.Time {
	return time.Date(year, month, day, hour, minute, second, 0, time.UTC)
}

func TestInstantOf_IsTheHourOnTheClockInTheMembersTimezone(t *testing.T) {
	cases := []struct {
		name string
		zone string
		day  int
		hour int
		want time.Time
	}{
		{"a London morning in summer", "Europe/London", 3, 8, utc(2026, time.September, 3, 7, 0, 0)},
		{"a New York morning", "America/New_York", 3, 8, utc(2026, time.September, 3, 12, 0, 0)},
		{"midnight in Auckland is the afternoon before in UTC", "Pacific/Auckland", 3, 0, utc(2026, time.September, 2, 12, 0, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := instantOf(2026, time.September, c.day, c.hour, zone(t, c.zone))
			if !got.Equal(c.want) {
				t.Errorf("instantOf = %s, want %s", got.UTC(), c.want)
			}
		})
	}
}

func TestInstantOf_AnHourTheClocksSkipIsTheInstantTheyJump(t *testing.T) {
	// London goes forward at 01:00 UTC on 29 March 2026, so 01:00 on the
	// clock never happens. The next thing the clock reads is 02:00 BST.
	got := instantOf(2026, time.March, 29, 1, zone(t, "Europe/London"))
	if want := utc(2026, time.March, 29, 1, 0, 0); !got.Equal(want) {
		t.Errorf("London 1am on the day the clocks go forward = %s, want %s", got.UTC(), want)
	}

	// Lord Howe Island goes forward by half an hour, from 02:00 to 02:30, so
	// the first instant after the jump is not on the hour.
	got = instantOf(2026, time.October, 4, 2, zone(t, "Australia/Lord_Howe"))
	if want := utc(2026, time.October, 3, 15, 30, 0); !got.Equal(want) {
		t.Errorf("Lord Howe 2am on the day the clocks go forward = %s, want %s", got.UTC(), want)
	}
}

func TestInstantOf_AnHourTheClocksRepeatIsTheFirstOfTheTwo(t *testing.T) {
	// London goes back at 01:00 UTC on 25 October 2026, so the clock reads
	// 01:00 at 00:00 UTC and again at 01:00 UTC.
	got := instantOf(2026, time.October, 25, 1, zone(t, "Europe/London"))
	if want := utc(2026, time.October, 25, 0, 0, 0); !got.Equal(want) {
		t.Errorf("London 1am on the day the clocks go back = %s, want %s", got.UTC(), want)
	}
}

func TestNextDigest_IsTodaysHourUntilAnHourAfterIt(t *testing.T) {
	london := zone(t, "Europe/London")
	eight := utc(2026, time.September, 3, 7, 0, 0)
	cases := []struct {
		name string
		now  time.Time
	}{
		{"before the hour", eight.Add(-time.Hour)},
		{"on the hour", eight},
		{"half an hour after it", eight.Add(30 * time.Minute)},
		{"a second short of an hour after it", eight.Add(time.Hour - time.Second)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, key, skipped := nextDigest(8, london, "", c.now)
			if !at.Equal(eight) || key != "2026-09-03" || !skipped.IsZero() {
				t.Errorf("nextDigest = %s %q skipped %s, want %s 2026-09-03 and nothing skipped", at.UTC(), key, skipped, eight)
			}
		})
	}
}

func TestNextDigest_AnHourOrMorePastTheHourTodayIsPassedOver(t *testing.T) {
	london := zone(t, "Europe/London")
	eight := utc(2026, time.September, 3, 7, 0, 0)

	at, key, skipped := nextDigest(8, london, "", eight.Add(time.Hour))

	if want := eight.AddDate(0, 0, 1); !at.Equal(want) || key != "2026-09-04" {
		t.Errorf("nextDigest = %s %q, want tomorrow %s 2026-09-04", at.UTC(), key, want)
	}
	if !skipped.Equal(eight) {
		t.Errorf("skipped = %s, want today's %s", skipped, eight)
	}
}

func TestNextDigest_ADayInTheLedgerIsNotSentForAgain(t *testing.T) {
	london := zone(t, "Europe/London")

	at, key, skipped := nextDigest(8, london, "2026-09-03", utc(2026, time.September, 3, 6, 0, 0))

	if want := utc(2026, time.September, 4, 7, 0, 0); !at.Equal(want) || key != "2026-09-04" || !skipped.IsZero() {
		t.Errorf("nextDigest = %s %q skipped %s, want %s 2026-09-04 and nothing skipped", at.UTC(), key, skipped, want)
	}
}

func TestNextDigest_ATimezoneMovedWestContinuesAfterTheLastDaySent(t *testing.T) {
	// A digest went out for 3 September in Tokyo. The account then moved to
	// Honolulu, where it is still 2 September. The next digest is for the
	// 4th, not a second one for the 3rd.
	honolulu := zone(t, "Pacific/Honolulu")

	at, key, _ := nextDigest(8, honolulu, "2026-09-03", utc(2026, time.September, 3, 6, 0, 0))

	if want := utc(2026, time.September, 4, 18, 0, 0); !at.Equal(want) || key != "2026-09-04" {
		t.Errorf("nextDigest = %s %q, want %s 2026-09-04", at.UTC(), key, want)
	}
}

func TestNextDigest_LateEveningIsTomorrowsMidnightInTheMembersDay(t *testing.T) {
	// 22:30 UTC is 23:30 in London. Midnight is half an hour off, and it is
	// the 4th's midnight, not the 3rd's.
	london := zone(t, "Europe/London")

	at, key, _ := nextDigest(0, london, "", utc(2026, time.September, 3, 22, 30, 0))

	if want := utc(2026, time.September, 3, 23, 0, 0); !at.Equal(want) || key != "2026-09-04" {
		t.Errorf("nextDigest = %s %q, want %s 2026-09-04", at.UTC(), key, want)
	}
}

func TestNextDigest_TheSecondOfARepeatedHourIsNotASecondDigest(t *testing.T) {
	// The digest for 25 October went out at the first 01:00 in London. A
	// second later the clock is still short of the second 01:00, and the next
	// digest is the 26th's.
	london := zone(t, "Europe/London")

	at, key, _ := nextDigest(1, london, "2026-10-25", utc(2026, time.October, 25, 0, 0, 1))

	if want := utc(2026, time.October, 26, 1, 0, 0); !at.Equal(want) || key != "2026-10-26" {
		t.Errorf("nextDigest = %s %q, want %s 2026-10-26", at.UTC(), key, want)
	}
}
