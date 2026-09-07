-- The id is supplied rather than defaulted, because the file is written under
-- that id before the row exists.
-- name: CreatePhoto :one
INSERT INTO photo (id, garden_id, plant_id, uploaded_by, taken_at, kind, path, width, height, bytes, square_bytes)
VALUES (@id, @garden_id, @plant_id, @uploaded_by, @taken_at, @kind, @path, @width, @height, @bytes, @square_bytes)
RETURNING *;

-- name: GetPhoto :one
SELECT * FROM photo
WHERE garden_id = @garden_id AND id = @photo_id;

-- The bytes of every photo in the garden, the square variants included.
-- name: SumPhotoBytes :one
SELECT coalesce(sum(bytes + coalesce(square_bytes, 0)), 0)::bigint
FROM photo
WHERE garden_id = @garden_id;
