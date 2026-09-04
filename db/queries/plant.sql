-- name: GetPlant :one
SELECT * FROM plant
WHERE garden_id = @garden_id AND id = @plant_id;

-- The order is the roster's, which groups by room and puts a plant with no
-- room last. Two plants can agree on room and name, so id ends the ordering
-- and fixes their order.
-- name: ListPlants :many
SELECT * FROM plant
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY location NULLS LAST, coalesce(nickname, common_name, botanical_name), id;
