-- Runs on every authenticated request, so one join fetches all three rows in a
-- single round trip.
-- name: GetMembershipWithUserAndGarden :one
SELECT sqlc.embed(membership), sqlc.embed(app_user), sqlc.embed(garden)
FROM membership
JOIN app_user ON app_user.id = membership.user_id
JOIN garden ON garden.id = membership.garden_id
WHERE membership.garden_id = @garden_id AND membership.user_id = @user_id;

-- The garden a session is put on is the first row here that has not ended.
-- Rows come back with the garden the account last switched to first, then the
-- oldest membership. This takes no garden_id because it is how the garden is
-- found. Every row returned belongs to @user_id.
-- name: ListMembershipsForUser :many
SELECT membership.* FROM membership
JOIN app_user ON app_user.id = membership.user_id
WHERE membership.user_id = @user_id
ORDER BY membership.garden_id = app_user.last_garden_id DESC NULLS LAST, membership.created_at, membership.id;

-- The hour the digest arrives, which is per membership: a person may want one
-- for their own garden and nothing from a garden they are sitting.
-- name: SetDigestHour :exec
UPDATE membership SET digest_hour = @digest_hour
WHERE garden_id = @garden_id AND user_id = @user_id;

-- The People page lists everybody in the garden, oldest membership first, so
-- the person who created it is at the top.
-- name: ListMembers :many
SELECT sqlc.embed(membership), sqlc.embed(app_user)
FROM membership
JOIN app_user ON app_user.id = membership.user_id
WHERE membership.garden_id = @garden_id
ORDER BY membership.created_at, membership.id;

-- People names a member by handle in its URLs. A handle belonging to somebody
-- outside the garden returns no row, so the route returns 404 for it.
-- name: GetMemberByHandle :one
SELECT sqlc.embed(membership), sqlc.embed(app_user)
FROM membership
JOIN app_user ON app_user.id = membership.user_id
WHERE membership.garden_id = @garden_id AND app_user.handle = @handle;

-- name: SetMemberRole :execrows
UPDATE membership SET role = @role
WHERE garden_id = @garden_id AND user_id = @user_id;

-- name: SetMembershipEnd :execrows
UPDATE membership SET expires_at = @expires_at
WHERE garden_id = @garden_id AND user_id = @user_id;

-- Removing a member deletes the membership. The foreign key sets garden_id to
-- NULL on their sessions for this garden, so they stay signed in. Every care
-- event they logged stays where it is, because an event points at the account
-- and not at the membership.
-- name: DeleteMembership :execrows
DELETE FROM membership
WHERE garden_id = @garden_id AND user_id = @user_id;

-- Deletes the account's memberships in every garden. There is no garden id
-- because the query spans them all.
-- name: DeleteUserMemberships :exec
DELETE FROM membership WHERE user_id = @user_id;

-- CreateMembership inserts the membership row and returns it. The caller also
-- writes the two notification_preference rows every membership has.
-- name: CreateMembership :one
INSERT INTO membership (garden_id, user_id, role, invited_by, expires_at, digest_hour)
VALUES (@garden_id, @user_id, @role, @invited_by, @expires_at, @digest_hour)
RETURNING *;

-- Every garden the account is a member of, oldest membership first, with the
-- owner's display name. The owner is the oldest owner membership: the person
-- who created the garden, unless they have left it. owner_name is empty and
-- reader_owns is false when no owner is left. This takes no garden_id because
-- it is how the account's other gardens are found. Every row belongs to
-- @user_id.
-- name: ListMembershipsWithGardensForUser :many
SELECT sqlc.embed(membership), sqlc.embed(garden),
    coalesce(owner.display_name, '')::text AS owner_name,
    coalesce(owner.id = membership.user_id, false)::boolean AS reader_owns
FROM membership
JOIN garden ON garden.id = membership.garden_id
LEFT JOIN LATERAL (
    SELECT app_user.id, app_user.display_name
    FROM membership o
    JOIN app_user ON app_user.id = o.user_id
    WHERE o.garden_id = garden.id AND o.role = 'owner'
    ORDER BY o.created_at, o.id
    LIMIT 1
) AS owner ON true
WHERE membership.user_id = @user_id
ORDER BY membership.created_at, membership.id;

-- Accepting an invite as an account whose membership of the garden has ended
-- updates that row rather than inserting one, because membership is unique on
-- (garden_id, user_id). The row keeps its digest hour and its notification
-- preferences.
-- name: RenewMembership :one
UPDATE membership
SET role = @role, invited_by = @invited_by, expires_at = @expires_at
WHERE garden_id = @garden_id AND user_id = @user_id
RETURNING *;

-- For every membership with an end date after @since: the sitter, and the
-- person who invited them where that person is still a member at @now. One row
-- per recipient, with is_sitter telling the two apart. A recipient with no
-- browser subscribed is left out, so the job never claims a ledger row and then
-- sends nothing. owner_name is the display name of the garden's owner, empty
-- when the garden has no owner. recipient_owns is true when the recipient is
-- that owner. There is no @garden_id because the job runs across every garden.
-- name: ListSittingDeadlines :many
SELECT sitting.id AS sitting_id, sitting.expires_at::timestamptz AS ends_at,
    sitter.display_name AS sitter_name, garden.name AS garden_name,
    recipient.id AS membership_id, recipient.user_id, app_user.handle,
    (recipient.id = sitting.id)::boolean AS is_sitter,
    coalesce(owner.display_name, '')::text AS owner_name,
    coalesce(owner.id = recipient.user_id, false)::boolean AS recipient_owns
FROM membership sitting
JOIN app_user sitter ON sitter.id = sitting.user_id
JOIN garden ON garden.id = sitting.garden_id
JOIN membership recipient ON recipient.garden_id = sitting.garden_id
    AND (recipient.id = sitting.id
        OR (recipient.user_id = sitting.invited_by AND (recipient.expires_at IS NULL OR recipient.expires_at > @now::timestamptz)))
JOIN app_user ON app_user.id = recipient.user_id
LEFT JOIN LATERAL (
    SELECT app_user.id, app_user.display_name
    FROM membership o
    JOIN app_user ON app_user.id = o.user_id
    WHERE o.garden_id = garden.id AND o.role = 'owner'
    ORDER BY o.created_at, o.id
    LIMIT 1
) AS owner ON true
WHERE sitting.expires_at IS NOT NULL AND sitting.expires_at > @since::timestamptz
  AND EXISTS (SELECT 1 FROM push_subscription WHERE push_subscription.user_id = recipient.user_id)
ORDER BY sitting.expires_at, sitting.id, recipient.id = sitting.id DESC;

-- The members of the garden whose role grants @capability and whose membership
-- has not ended at @now, oldest membership first.
-- name: ListMembersWithCapability :many
SELECT sqlc.embed(membership), sqlc.embed(app_user)
FROM membership
JOIN app_user ON app_user.id = membership.user_id
JOIN role_capability ON role_capability.role = membership.role AND role_capability.capability = @capability
WHERE membership.garden_id = @garden_id
  AND (membership.expires_at IS NULL OR membership.expires_at > @now::timestamptz)
ORDER BY membership.created_at, membership.id;
