-- Runs on every authenticated request, so the join reads all three rows in one
-- round trip.
-- name: GetMembershipWithUserAndGarden :one
SELECT sqlc.embed(membership), sqlc.embed(app_user), sqlc.embed(garden)
FROM membership
JOIN app_user ON app_user.id = membership.user_id
JOIN garden ON garden.id = membership.garden_id
WHERE membership.garden_id = @garden_id AND membership.user_id = @user_id;

-- A new session starts on the oldest row. The read takes no garden_id because
-- it is how a garden is found, and every row it returns belongs to @user_id.
-- name: ListMembershipsForUser :many
SELECT * FROM membership
WHERE user_id = @user_id
ORDER BY created_at, id;
