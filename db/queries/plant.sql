-- name: GetPlant :one
SELECT * FROM plant
WHERE garden_id = @garden_id AND id = @plant_id;

-- The order is the roster's, which groups by room and puts a plant with no
-- room last. Two plants can agree on room and name, so id ends the ordering.
-- name: ListPlants :many
SELECT * FROM plant
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY location NULLS LAST, coalesce(nickname, common_name, botanical_name), id;

-- Today needs the number rather than the rows, so it can tell a garden with no
-- plants from one with nothing due.
-- name: CountPlants :one
SELECT count(*) FROM plant
WHERE garden_id = @garden_id AND archived_at IS NULL;
