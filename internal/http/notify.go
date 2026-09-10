package http

import (
	"context"
	"errors"
	"strconv"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/ismailshak/sprig/internal/auth"
	"github.com/ismailshak/sprig/internal/photo"
	"github.com/ismailshak/sprig/internal/push"
	"github.com/ismailshak/sprig/internal/store"
)

// storageKind is the kind column of the ledger row the storage notification
// claims before it is sent.
const storageKind = "storage_nearly_full"

// nearlyFull is the share of the quota at which the Garden page's storage line
// adds the sentence about deleting photos and an upload sends the storage
// notification. Below it there is nothing to act on.
const nearlyFull = 0.9

func nearlyFullReached(usage photo.Usage) bool {
	return float64(usage.Used) >= nearlyFull*float64(usage.Quota)
}

// storageKey is the send key of the storage notification: the share and the
// quota. A raised quota is a new key, so the next crossing of the new
// threshold is sent for.
func storageKey(usage photo.Usage) string {
	return strconv.FormatFloat(nearlyFull, 'f', -1, 64) + " " + strconv.FormatInt(usage.Quota, 10)
}

// gardenNamer reads the garden's owner once and returns the function that
// names the garden to one recipient: "Ellie’s Rosewood", or "Rosewood" when
// the recipient is Ellie. The owner is read rather than taken from the
// request, because the person making the request is not always the owner.
func gardenNamer(ctx context.Context, queries *store.Queries, garden store.Garden) (func(recipient uuid.UUID) string, error) {
	owner, err := queries.GetGardenOwner(ctx, garden.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return func(uuid.UUID) string { return garden.Name }, nil
	}
	if err != nil {
		return nil, err
	}
	return func(recipient uuid.UUID) string {
		return push.GardenNamed(garden.Name, owner.DisplayName, owner.ID == recipient)
	}, nil
}

// inviteAcceptedNotification is what the person who created an invite gets
// when it is accepted. It reads "Robin joined Ellie’s Rosewood as a sitter."
// and opens People. garden is the name gardenNamedFor gives it.
func inviteAcceptedNotification(garden string, joined store.AppUser, role string) push.Notification {
	return push.Notification{Title: "Invite accepted", Body: joined.DisplayName + " joined " + garden + " as a " + role + ".", URL: PeoplePath}
}

// roleChangedNotification is what a member gets when their role is changed
// on People. It reads "You’re now a sitter in Ellie’s Rosewood." and opens
// Today.
func roleChangedNotification(garden, role string) push.Notification {
	return push.Notification{Title: "Role changed", Body: "You’re now a " + role + " in " + garden + ".", URL: todayPath}
}

// membershipRemovedNotification is what a member gets when they are removed
// on People. It opens nothing, because they can no longer open the garden.
func membershipRemovedNotification(garden string) push.Notification {
	return push.Notification{Title: "Removed from a garden", Body: "You’ve been removed from " + garden + "."}
}

// storageNotification is what the people who can delete any photo get when
// an upload takes the garden to nearlyFull of the quota. It reads "920 MB of
// 1 GB used in Rosewood. Delete photos to make room." and opens the photos of
// the plant the upload was for.
func storageNotification(garden string, usage photo.Usage, plant store.Plant) push.Notification {
	return push.Notification{
		Title: "Photo storage nearly full",
		Body:  storageFigure(usage.Used) + " of " + storageFigure(usage.Quota) + " used in " + garden + ". Delete photos to make room.",
		URL:   photosPath(plant.ID),
	}
}

// notifyStorage runs after a photo is saved. When the garden's photos have
// reached nearlyFull of the quota it sends the storage notification to each
// member whose role can delete any photo. Each member's ledger row is claimed
// first, so a garden that stays over the share is told once. A database error
// is logged and nothing else happens, because the photo is saved and the
// response is not about the notification.
func (h *plants) notifyStorage(ctx context.Context, principal auth.Principal, plant store.Plant) {
	usage, err := h.photos.Usage(ctx, h.queries, principal.Garden.ID)
	if err != nil {
		h.logger.Error("storage not checked", "garden", principal.Garden.Name, "err", err)
		return
	}
	if !nearlyFullReached(usage) {
		return
	}
	members, err := h.queries.ListMembersWithCapability(ctx, store.ListMembersWithCapabilityParams{
		Capability: string(auth.PhotoDeleteAny),
		GardenID:   principal.Garden.ID,
		Now:        h.now(),
	})
	if err != nil {
		h.logger.Error("storage notification not sent", "garden", principal.Garden.Name, "err", err)
		return
	}
	gardenNamed, err := gardenNamer(ctx, h.queries, principal.Garden)
	if err != nil {
		h.logger.Error("storage notification not sent", "garden", principal.Garden.Name, "err", err)
		return
	}
	for _, member := range members {
		claimed, err := h.queries.ClaimNotificationSend(ctx, store.ClaimNotificationSendParams{
			MembershipID: member.Membership.ID,
			Kind:         storageKind,
			SendKey:      storageKey(usage),
		})
		if err != nil {
			h.logger.Error("storage notification not sent", "garden", principal.Garden.Name, "user", member.AppUser.Handle, "err", err)
			return
		}
		if claimed == 0 {
			continue
		}
		h.notify.call(ctx, member.AppUser, storageNotification(gardenNamed(member.AppUser.ID), usage, plant))
	}
}

// clearStorageNotification runs after a photo is deleted. Once the garden's
// photos are back under nearlyFull of the quota it deletes the ledger rows the
// storage notification claimed, so the next crossing is sent for again. A
// database error is logged and nothing else happens.
func (h *plants) clearStorageNotification(ctx context.Context, principal auth.Principal) {
	usage, err := h.photos.Usage(ctx, h.queries, principal.Garden.ID)
	if err != nil {
		h.logger.Error("storage not checked", "garden", principal.Garden.Name, "err", err)
		return
	}
	if nearlyFullReached(usage) {
		return
	}
	_, err = h.queries.DeleteNotificationSends(ctx, store.DeleteNotificationSendsParams{
		GardenID: principal.Garden.ID,
		Kind:     storageKind,
		SendKey:  storageKey(usage),
	})
	if err != nil {
		h.logger.Error("storage notification not cleared", "garden", principal.Garden.Name, "err", err)
	}
}
