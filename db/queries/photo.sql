-- The id is supplied rather than defaulted, because the file is written under
-- that id before the row exists.
-- name: CreatePhoto :one
INSERT INTO photo (id, garden_id, plant_id, uploaded_by, taken_at, kind, path, width, height, bytes, square_bytes)
VALUES (@id, @garden_id, @plant_id, @uploaded_by, @taken_at, @kind, @path, @width, @height, @bytes, @square_bytes)
RETURNING *;

-- name: GetPhoto :one
SELECT * FROM photo
WHERE garden_id = @garden_id AND id = @photo_id;

-- Sets which part of the photo the plant's page shows.
-- name: SetPhotoFocus :exec
UPDATE photo SET focus_x = @focus_x, focus_y = @focus_y
WHERE garden_id = @garden_id AND plant_id = @plant_id AND id = @photo_id;

-- The bytes of every photo in the garden, the square variants included.
-- name: SumPhotoBytes :one
SELECT coalesce(sum(bytes + coalesce(square_bytes, 0)), 0)::bigint
FROM photo
WHERE garden_id = @garden_id;

-- Every photo row, for the sweep that compares them with the files on disk.
-- It takes no garden id, because the sweep covers every garden at once.
-- name: ListPhotoFiles :many
SELECT id, plant_id, path, square_bytes FROM photo
ORDER BY path;

-- A plant's photos, newest first, with the name of whoever uploaded each.
-- before_at and before_id are null for the first page and otherwise hold the
-- uploaded_at and id of the last photo on the previous page. Both columns are
-- compared because two photos can share an uploaded_at. It defaults to now(),
-- the same value for every row inserted in one transaction.
-- name: ListPlantPhotos :many
SELECT sqlc.embed(photo), app_user.display_name AS uploaded_by_name
FROM photo
JOIN app_user ON app_user.id = photo.uploaded_by
WHERE photo.garden_id = @garden_id AND photo.plant_id = @plant_id
  AND (sqlc.narg('before_at')::timestamptz IS NULL
       OR (photo.uploaded_at, photo.id) < (sqlc.narg('before_at'), sqlc.narg('before_id')::uuid))
ORDER BY photo.uploaded_at DESC, photo.id DESC
LIMIT @count;

-- name: CountPlantPhotos :one
SELECT count(*) FROM photo
WHERE garden_id = @garden_id AND plant_id = @plant_id;

-- One photo with the name of whoever uploaded it, for the photo's own page.
-- name: GetPlantPhoto :one
SELECT sqlc.embed(photo), app_user.display_name AS uploaded_by_name
FROM photo
JOIN app_user ON app_user.id = photo.uploaded_by
WHERE photo.garden_id = @garden_id AND photo.plant_id = @plant_id AND photo.id = @photo_id;

-- Matches on uploaded_by unless may_delete_any is set, so the ownership check
-- is in the query and cannot be forgotten by a caller. A plant whose profile
-- picture this was loses the picture through the foreign key.
-- name: DeletePhoto :one
DELETE FROM photo
WHERE garden_id = @garden_id AND plant_id = @plant_id AND id = @photo_id
  AND (@may_delete_any::boolean OR uploaded_by = @uploaded_by)
RETURNING *;
