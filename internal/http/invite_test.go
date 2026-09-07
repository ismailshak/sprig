package http

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/auth"
)

func TestInvite_TheChipsOfferTheTwoRolesAndSaySoInTheWordsAMembersRowUses(t *testing.T) {
	f := peopleGarden(t)

	page := f.page(t, f.handler.invite, invitePath+"?role=member")

	if got := chips(page); !slices.Equal(got, []string{"Member", "Sitter"}) {
		t.Errorf("the chips read %v, want Member and Sitter", got)
	}
	if pressed := chipPressed(page); pressed != "Member" {
		t.Errorf("%q is pressed, want Member", pressed)
	}
	if !strings.Contains(page, roleWhat["member"]) {
		t.Errorf("the sentence under the chips is not the one a member's row uses:\n%s", page)
	}
}

func TestInvite_TheChipsStartOnSitterAndKeepTheEndDateWhenOneIsPressed(t *testing.T) {
	f := peopleGarden(t)

	page := f.page(t, f.handler.invite, invitePath)
	if pressed := chipPressed(page); pressed != "Sitter" {
		t.Errorf("%q is pressed on a first visit, want Sitter", pressed)
	}

	page = f.page(t, f.handler.invite, invitePath+"?role=member&until=2026-09-14")
	if got := endDateField(t, page); got != "2026-09-14" {
		t.Errorf("the end date reads %q after a chip was pressed, want %q", got, "2026-09-14")
	}
}

func TestInvite_ALinkIsShownOnceAndOnlyItsHashIsStored(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.createInviteLink, invitePath, url.Values{"role": {"sitter"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	link := secretValue(t, rec.Body.String())
	token := strings.TrimPrefix(link, "example.com/invite/")
	if token == link {
		t.Fatalf("the link reads %q, want the host and the path that redeems the token", link)
	}

	var stored string
	var expires time.Time
	row := f.tx.QueryRow(t.Context(), "SELECT token_hash, expires_at FROM invite WHERE token_hash = $1", auth.HashToken(token))
	if err := row.Scan(&stored, &expires); err != nil {
		t.Fatalf("no invite holds the hash of the link that was shown: %v", err)
	}
	if want := thursday.Add(auth.InviteLifetime); !expires.Equal(want) {
		t.Errorf("the link runs out at %s, want %s", expires, want)
	}
	if strings.Contains(f.page(t, f.handler.invite, invitePath), token) {
		t.Error("the link is on the page again on the next request, and it is shown exactly once")
	}
}

func TestInvite_TheEndDateLandsOnTheMembershipTheLinkWillCreate(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.createInviteLink, invitePath, url.Values{"role": {"sitter"}, "until": {"2026-09-14"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Their access ends on 14 Sep") {
		t.Errorf("the paragraph under the link does not say when the access ends:\n%s", rec.Body.String())
	}

	token := strings.TrimPrefix(secretValue(t, rec.Body.String()), "example.com/invite/")
	var ends *time.Time
	row := f.tx.QueryRow(t.Context(), "SELECT membership_expires_at FROM invite WHERE token_hash = $1", auth.HashToken(token))
	if err := row.Scan(&ends); err != nil {
		t.Fatal(err)
	}
	if ends == nil {
		t.Fatal("the invite carries no end date, so the membership it creates would be permanent")
	}
	// The reader is in Europe/London, where 14 September begins an hour before
	// midnight UTC.
	if want := time.Date(2026, time.September, 13, 23, 0, 0, 0, time.UTC); !ends.Equal(want) {
		t.Errorf("the access ends at %s, want %s", ends.UTC(), want)
	}
}

func TestInvite_ARoleTheChipsDoNotOfferIsRefusedAndNoLinkIsMade(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.createInviteLink, invitePath, url.Values{"role": {"owner"}})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if invites := invitesInGarden(t, f); invites != 3 {
		t.Errorf("the garden holds %d invites, and a refused role writes none", invites)
	}
}

func TestInvite_ADateTheFieldCannotHoldIsRefusedWithTheFormStillOnThePage(t *testing.T) {
	f := peopleGarden(t)

	rec := f.do(t, f.handler.createInviteLink, invitePath, url.Values{"role": {"member"}, "until": {"next Tuesday"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	page := rec.Body.String()
	if !strings.Contains(page, untilUnusable) {
		t.Errorf("the message under the end date is missing:\n%s", page)
	}
	if pressed := chipPressed(page); pressed != "Member" {
		t.Errorf("%q is pressed after the refusal, want the role that was chosen", pressed)
	}
	if invites := invitesInGarden(t, f); invites != 3 {
		t.Errorf("the garden holds %d invites, and a refused date writes none", invites)
	}
}

func invitesInGarden(t *testing.T, f *moreFixture) int {
	t.Helper()

	var count int
	row := f.tx.QueryRow(t.Context(), "SELECT count(*) FROM invite WHERE garden_id = $1", moreGardenID)
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

var untilElement = regexp.MustCompile(`<input class="input input--narrow" id="until" name="until" type="date" min="[^"]*" value="([^"]*)">`)

// chipPressed returns the word on the one chip that is pressed.
func chipPressed(page string) string {
	if m := pressedButton.FindStringSubmatch(page); m != nil {
		return text(m[1])
	}
	return ""
}

func endDateField(t *testing.T, page string) string {
	t.Helper()

	m := untilElement.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no end date field on:\n%s", page)
	}
	return m[1]
}

func TestInvite_TheEndDateFieldOffersNothingBeforeTomorrowInTheInvitersZone(t *testing.T) {
	f := peopleGarden(t)
	// The fixture's clock is 08:00 UTC on Thursday 3 September. In Honolulu
	// that is still Wednesday evening, so tomorrow there is the 3rd.
	f.principal.User.Timezone = "Pacific/Honolulu"

	page := f.page(t, f.handler.invite, invitePath)

	if !strings.Contains(page, `type="date" min="2026-09-03"`) {
		t.Errorf("the end date field does not start on tomorrow in the inviter's zone:\n%s", page)
	}
}

func TestInvite_AnEndDateThatHasAlreadyBegunIsRefusedAndTomorrowIsNot(t *testing.T) {
	cases := []struct {
		zone    string
		posted  string
		refused bool
	}{
		// 08:00 UTC on the 3rd is the 3rd in London, so the 3rd has begun
		// there and the 4th has not.
		{"Europe/London", "2026-09-03", true},
		{"Europe/London", "2026-09-02", true},
		{"Europe/London", "2026-09-04", false},
		// In Honolulu it is still the 2nd, so the 3rd is tomorrow.
		{"Pacific/Honolulu", "2026-09-03", false},
		{"Pacific/Honolulu", "2026-09-02", true},
	}
	for _, c := range cases {
		t.Run(c.zone+" "+c.posted, func(t *testing.T) {
			f := peopleGarden(t)
			f.principal.User.Timezone = c.zone
			before := invitesInGarden(t, f)

			rec := f.do(t, f.handler.createInviteLink, invitePath, url.Values{"role": {"sitter"}, "until": {c.posted}})

			if !c.refused {
				if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/invite/") {
					t.Fatalf("status = %d, want %d with the link on the page:\n%s", rec.Code, http.StatusOK, text(rec.Body.String()))
				}
				return
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d:\n%s", rec.Code, http.StatusUnprocessableEntity, text(rec.Body.String()))
			}
			page := rec.Body.String()
			if !strings.Contains(page, untilPassed) {
				t.Errorf("the message under the end date is missing:\n%s", text(page))
			}
			if !strings.Contains(page, `value="`+c.posted+`"`) {
				t.Errorf("the field lost the date that was typed:\n%s", page)
			}
			if invitesInGarden(t, f) != before {
				t.Error("a refused date wrote an invite")
			}
		})
	}
}
