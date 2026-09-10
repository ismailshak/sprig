-- name: RenameGarden :exec
UPDATE garden SET name = @name WHERE id = @garden_id;

-- name: CreateGarden :one
INSERT INTO garden (name)
VALUES (@name)
RETURNING *;

-- name: GetGarden :one
SELECT * FROM garden WHERE id = @garden_id;

-- The garden's owner: the oldest owner membership, the person who created the
-- garden unless they have left it. No row when no owner is left. A
-- notification names another person's garden after its owner, "Ellie's
-- Rosewood", and the owner's own garden by its name alone.
-- name: GetGardenOwner :one
SELECT app_user.id, app_user.display_name
FROM membership
JOIN app_user ON app_user.id = membership.user_id
WHERE membership.garden_id = @garden_id AND membership.role = 'owner'
ORDER BY membership.created_at, membership.id
LIMIT 1;

-- Deletes the garden's care events before the garden itself, because
-- care_event references care_type with ON DELETE RESTRICT and Postgres refuses
-- the cascade while any event is left. Deleting the garden then cascades to
-- its memberships, plants, care types, schedules, photos, invites and tokens.
-- name: DeleteGardenCareEvents :exec
DELETE FROM care_event WHERE garden_id = @garden_id;

-- name: DeleteGarden :execrows
DELETE FROM garden WHERE id = @garden_id;
