package push

import (
	"time"

	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/store"
)

// tokenNamed returns the token's name and prefix as the Tokens list shows them.
func tokenNamed(token store.APIToken) string {
	return token.Name + " (" + token.Prefix + "…)"
}

// tokenExpiringNotification reads "The kitchen display (sprg_7c1f…) in
// Ellie’s Rosewood expires on 10 Sep." The date is in loc, the recipient's
// timezone.
func tokenExpiringNotification(garden string, token store.APIToken, loc *time.Location, tokensURL string) Notification {
	return Notification{Title: "Token expires soon", Body: tokenNamed(token) + " in " + garden + " expires on " + token.ExpiresAt.In(loc).Format("2 Jan") + ".", URL: tokensURL}
}

// tokenExpiredNotification reads "The kitchen display (sprg_7c1f…) in Ellie’s
// Rosewood has expired."
func tokenExpiredNotification(garden string, token store.APIToken, tokensURL string) Notification {
	return Notification{Title: "Token expired", Body: tokenNamed(token) + " in " + garden + " has expired.", URL: tokensURL}
}

// sittingEndedNotification is sent to the sitter and to the person who invited
// them when the sitter's access to a garden ends. The sitter's has no URL,
// because the sitter can no longer open the garden's pages. The inviter's reads
// "Sam no longer has access to Rosewood." to avoid two possessives in a row, as
// in "Sam’s access to Ellie’s Rosewood".
func sittingEndedNotification(row store.ListSittingDeadlinesRow, peopleURL string) Notification {
	garden := GardenNamed(row.GardenName, row.OwnerName, row.RecipientOwns)
	if row.IsSitter {
		return Notification{Title: "Access ended", Body: "Your access to " + garden + " has ended."}
	}
	return Notification{Title: "Access ended", Body: row.SitterName + " no longer has access to " + garden + ".", URL: peopleURL}
}

// InviteAcceptedNotification is sent to the person who created an invite when
// it is accepted. It reads "Robin joined Ellie’s Rosewood as a sitter."
func InviteAcceptedNotification(garden string, joined store.AppUser, role, peoplePath string) Notification {
	return Notification{Title: "Invite accepted", Body: joined.DisplayName + " joined " + garden + " as a " + role + ".", URL: peoplePath}
}

// RoleChangedNotification is sent to a member whose role is changed on People.
// It reads "You’re now a sitter in Ellie’s Rosewood."
func RoleChangedNotification(garden, role, todayPath string) Notification {
	return Notification{Title: "Role changed", Body: "You’re now a " + role + " in " + garden + ".", URL: todayPath}
}

// MemberRemovedNotification is sent to a member removed on People. It has no
// URL, because the member can no longer open the garden's pages.
func MemberRemovedNotification(garden string) Notification {
	return Notification{Title: "Removed from a garden", Body: "You’ve been removed from " + garden + "."}
}

// StorageNotification is sent when a photo upload brings the garden's photo
// storage to nearly full. It reads "920 MB of 1 GB used in Rosewood. Delete
// photos to make room."
func StorageNotification(garden string, usage photo.Usage, photosPath string) Notification {
	return Notification{
		Title: "Photo storage nearly full",
		Body:  photo.FormatSize(usage.Used) + " of " + photo.FormatSize(usage.Quota) + " used in " + garden + ". Delete photos to make room.",
		URL:   photosPath,
	}
}

// TestNotification is sent to one browser by the Notifications page's
// Send test notification button.
func TestNotification(notificationsPath string) Notification {
	return Notification{Title: "Test notification", Body: "Notifications are working.", URL: notificationsPath}
}
