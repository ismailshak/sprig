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
