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

-- The three names, the six reference fields and the acquired pair are all
-- nullable here because the form decides which of them are required.
-- name: CreatePlant :one
INSERT INTO plant (garden_id, nickname, common_name, botanical_name, location,
                   sun, water_needs, feed_needs, soil, climate, pot, notes,
                   acquired_year, acquired_month)
VALUES (@garden_id, @nickname, @common_name, @botanical_name, @location,
        @sun, @water_needs, @feed_needs, @soil, @climate, @pot, @notes,
        @acquired_year, @acquired_month)
RETURNING *;

-- Every column the form carries is written because a field emptied on the form
-- is emptied on the row. An archived plant matches nothing here because it is
-- not editable.
-- name: UpdatePlant :one
UPDATE plant
SET nickname = @nickname, common_name = @common_name, botanical_name = @botanical_name,
    location = @location, sun = @sun, water_needs = @water_needs, feed_needs = @feed_needs,
    soil = @soil, climate = @climate, pot = @pot, notes = @notes,
    acquired_year = @acquired_year, acquired_month = @acquired_month
WHERE garden_id = @garden_id AND id = @plant_id AND archived_at IS NULL
RETURNING *;

-- Archiving twice matches nothing, which is the answer for a plant the garden
-- does not have as well.
-- name: ArchivePlant :one
UPDATE plant SET archived_at = now()
WHERE garden_id = @garden_id AND id = @plant_id AND archived_at IS NULL
RETURNING *;
