-- Runs on every authenticated request, so one join fetches all three rows in a
-- single round trip.
-- name: GetMembershipWithUserAndGarden :one
SELECT sqlc.embed(membership), sqlc.embed(app_user), sqlc.embed(garden)
FROM membership
JOIN app_user ON app_user.id = membership.user_id
JOIN garden ON garden.id = membership.garden_id
WHERE membership.garden_id = @garden_id AND membership.user_id = @user_id;

-- A new session starts on the oldest membership. This takes no garden_id
-- because it is how the garden is found. Every row returned belongs to
-- @user_id.
-- name: ListMembershipsForUser :many
SELECT * FROM membership
WHERE user_id = @user_id
ORDER BY created_at, id;

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

-- Removing a member deletes the membership. Their sessions on this garden go
-- with it through the foreign key, so they lose access at once rather than on
-- the next page they load. Every care event they logged stays where it is,
-- because an event points at the account and not at the membership.
-- name: DeleteMembership :execrows
DELETE FROM membership
WHERE garden_id = @garden_id AND user_id = @user_id;
