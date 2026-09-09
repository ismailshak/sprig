-- name: GetPlant :one
SELECT * FROM plant
WHERE garden_id = @garden_id AND id = @plant_id;

-- Ordered by room, then display name, with plants that have no room last. id
-- comes last so two plants with the same room and name keep a stable order.
-- name: ListPlants :many
SELECT * FROM plant
WHERE garden_id = @garden_id AND archived_at IS NULL
ORDER BY location NULLS LAST, coalesce(nickname, common_name, botanical_name), id;

-- The Today page needs only the count, to tell a garden with no plants from one
-- with nothing due.
-- name: CountPlants :one
SELECT count(*) FROM plant
WHERE garden_id = @garden_id AND archived_at IS NULL;

-- Every column except garden_id is nullable. The plant form decides which are
-- required.
-- name: CreatePlant :one
INSERT INTO plant (garden_id, nickname, common_name, botanical_name, location,
                   sun, water_needs, feed_needs, soil, climate, pot, notes,
                   acquired_year, acquired_month)
VALUES (@garden_id, @nickname, @common_name, @botanical_name, @location,
        @sun, @water_needs, @feed_needs, @soil, @climate, @pot, @notes,
        @acquired_year, @acquired_month)
RETURNING *;

-- Every form field is written, so a field cleared on the form is cleared on the
-- row. An archived plant matches nothing because it cannot be edited.
-- name: UpdatePlant :one
UPDATE plant
SET nickname = @nickname, common_name = @common_name, botanical_name = @botanical_name,
    location = @location, sun = @sun, water_needs = @water_needs, feed_needs = @feed_needs,
    soil = @soil, climate = @climate, pot = @pot, notes = @notes,
    acquired_year = @acquired_year, acquired_month = @acquired_month
WHERE garden_id = @garden_id AND id = @plant_id AND archived_at IS NULL
RETURNING *;

-- Sets the plant's profile picture, or clears it when photo_id is null. The
-- update matches no row when the photo belongs to another plant or has no
-- square variant, because every list shows the picture as its square.
-- name: SetProfilePhoto :one
UPDATE plant SET profile_photo_id = sqlc.narg('photo_id')
WHERE plant.garden_id = @garden_id AND plant.id = @plant_id AND plant.archived_at IS NULL
  AND (sqlc.narg('photo_id')::uuid IS NULL
       OR EXISTS (SELECT 1 FROM photo
                  WHERE photo.id = sqlc.narg('photo_id')
                    AND photo.garden_id = @garden_id AND photo.plant_id = @plant_id
                    AND photo.square_bytes IS NOT NULL))
RETURNING *;

-- Archiving an already archived plant matches nothing, the same result as for
-- a plant the garden does not have.
-- name: ArchivePlant :one
UPDATE plant SET archived_at = now()
WHERE garden_id = @garden_id AND id = @plant_id AND archived_at IS NULL
RETURNING *;

-- Every room the garden's plants are in, listed once, for the Room field on
-- the plant form. A room only archived plants are in is left out, because the
-- Plants list stops showing it too. The order ignores case, the same as the
-- Plants list's grouping. GROUP BY rather than SELECT DISTINCT, because
-- DISTINCT can only order by an expression that is in the select list, and
-- lower(location) is not. The ::text cast is what makes sqlc return []string
-- rather than []*string for a nullable column.
-- name: ListRooms :many
SELECT location::text AS room FROM plant
WHERE garden_id = @garden_id AND archived_at IS NULL AND location IS NOT NULL
GROUP BY location
ORDER BY lower(location), location;

-- name: ListArchivedPlants :many
SELECT * FROM plant
WHERE garden_id = @garden_id AND archived_at IS NOT NULL
ORDER BY archived_at DESC, id;

-- The Plants page links to the archive only when this is not zero.
-- name: CountArchivedPlants :one
SELECT count(*) FROM plant
WHERE garden_id = @garden_id AND archived_at IS NOT NULL;

-- Returns no rows for a plant that is not archived, the same as for a plant in
-- another garden. The handler returns 404 for both.
-- name: RestorePlant :one
UPDATE plant SET archived_at = NULL
WHERE garden_id = @garden_id AND id = @plant_id AND archived_at IS NOT NULL
RETURNING *;
