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
