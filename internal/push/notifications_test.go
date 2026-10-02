package push

import (
	"testing"
	"time"

	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

var kitchenToken = store.APIToken{ID: kitchenTokenID, Name: "The kitchen display", Prefix: "sprg_7c1f", ExpiresAt: utc(2026, time.September, 10, 12, 0, 0)}

func TestTokenExpiringNotification_NamesTheTokenByNameAndPrefixWithTheDateInTheRecipientsZone(t *testing.T) {
	// Noon UTC on 10 Sep is midnight on 11 Sep in Auckland.
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatal(err)
	}

	got := tokenExpiringNotification("Ellie’s Rosewood", kitchenToken, auckland, "https://sprig.example.com/more/tokens")

	want := Notification{Title: "Token expires soon", Body: "The kitchen display (sprg_7c1f…) in Ellie’s Rosewood expires on 11 Sep.", URL: "https://sprig.example.com/more/tokens"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestTokenExpiredNotification_SaysTheTokenHasExpiredAndOpensTokens(t *testing.T) {
	got := tokenExpiredNotification("Rosewood", kitchenToken, "https://sprig.example.com/more/tokens")

	want := Notification{Title: "Token expired", Body: "The kitchen display (sprg_7c1f…) in Rosewood has expired.", URL: "https://sprig.example.com/more/tokens"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestSittingEndedNotification_OpensPeopleForTheInviterAndNothingForTheSitter(t *testing.T) {
	// The inviter is Ellie, the owner, so only the sitter's text says Ellie’s Rosewood.
	row := store.ListSittingDeadlinesRow{SitterName: "Sam", GardenName: "Rosewood", OwnerName: "Ellie", IsSitter: true}
	people := "https://sprig.example.com/more/people"

	sitter := sittingEndedNotification(row, people)
	row.IsSitter, row.RecipientOwns = false, true
	inviter := sittingEndedNotification(row, people)

	if want := (Notification{Title: "Access ended", Body: "Your access to Ellie’s Rosewood has ended."}); sitter != want {
		t.Errorf("the sitter's notification is %+v, want %+v", sitter, want)
	}
	if want := (Notification{Title: "Access ended", Body: "Sam no longer has access to Rosewood.", URL: people}); inviter != want {
		t.Errorf("the inviter's notification is %+v, want %+v", inviter, want)
	}
}

func TestInviteAcceptedNotification_NamesWhoJoinedAndTheirRole(t *testing.T) {
	got := InviteAcceptedNotification("Ellie’s Rosewood", store.AppUser{DisplayName: "Robin"}, "sitter", "/more/people")

	want := Notification{Title: "Invite accepted", Body: "Robin joined Ellie’s Rosewood as a sitter.", URL: "/more/people"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestRoleChangedNotification_NamesTheGardenAndTheNewRole(t *testing.T) {
	got := RoleChangedNotification("Ellie’s Rosewood", "sitter", "/")

	want := Notification{Title: "Role changed", Body: "You’re now a sitter in Ellie’s Rosewood.", URL: "/"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestMemberRemovedNotification_NamesTheGardenAndOpensNothing(t *testing.T) {
	got := MemberRemovedNotification("Ellie’s Rosewood")

	want := Notification{Title: "Removed from a garden", Body: "You’ve been removed from Ellie’s Rosewood."}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}

func TestStorageNotification_SaysHowMuchOfTheQuotaIsUsed(t *testing.T) {
	usage := photo.Usage{Used: 920_000_000, Quota: 1_000_000_000}

	got := StorageNotification("Rosewood", usage, "/plants/doris/photos")

	want := Notification{Title: "Photo storage nearly full", Body: "920 MB of 1 GB used in Rosewood. Delete photos to make room.", URL: "/plants/doris/photos"}
	if got != want {
		t.Errorf("the notification is %+v, want %+v", got, want)
	}
}
